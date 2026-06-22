// Package provider defines the model-backend abstraction and a registry mapping
// a provider "kind" to a factory. Concrete implementations live in subpackages
// (e.g. provider/openai) and self-register via init(). The core resolves
// providers by kind from config and never hardcodes a specific model.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zzycxz/momapeer/internal/nilutil"
)

// Role is the role of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single conversation message.
type Message struct {
	Role             Role   `json:"role"`
	Content          any    `json:"content,omitempty"`           // string or []ContentPart (multimodal)
	ReasoningContent string `json:"reasoning_content,omitempty"` // assistant: thinking-mode chain-of-thought, round-tripped on multi-turn
	// ReasoningSignature is an opaque, provider-issued proof that ReasoningContent
	// is genuine model output. Anthropic requires the signed thinking block be
	// replayed on the next turn when a tool call followed thinking; providers
	// without signed reasoning (e.g. the openai-compatible ones) leave it empty.
	// Round-tripped alongside ReasoningContent.
	ReasoningSignature string     `json:"reasoning_signature,omitempty"`
	ToolCalls          []ToolCall `json:"tool_calls,omitempty"`   // set by assistant
	ToolCallID         string     `json:"tool_call_id,omitempty"` // links a tool result to its call
	Name               string     `json:"name,omitempty"`         // tool message: tool name
}

// UnmarshalJSON restores Content as its concrete type. The field is `any`
// (string for plain text, []ContentPart for multimodal). encoding/json has no
// way to recover that from a generic []interface{} on reload, so without this
// a saved multimodal message comes back as []interface{} — and every downstream
// switch on `content.(type)` (ContentString, ContentLen, buildRequest) misses
// its []ContentPart case, dumping the image data URL as plain text. We decode
// content separately: a JSON string stays a string; a JSON array becomes
// []ContentPart, fixing the type for the whole pipeline.
func (m *Message) UnmarshalJSON(data []byte) error {
	type alias Message
	var raw struct {
		alias
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = Message(raw.alias)
	trimmed := bytes.TrimSpace(raw.Content)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		m.Content = nil
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		m.Content = s
		return nil
	}
	var parts []ContentPart
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return err
	}
	m.Content = parts
	return nil
}

// ContentPart is one block in a multimodal message (OpenAI content parts format).
type ContentPart struct {
	Type     string    `json:"type"`                // "text" or "image_url"
	Text     string    `json:"text,omitempty"`      // for Type == "text"
	ImageURL *ImageURL `json:"image_url,omitempty"` // for Type == "image_url"
}

// ImageURL holds a base64 data URL for inline image content.
type ImageURL struct {
	URL    string `json:"url"`              // "data:image/png;base64,..."
	Detail string `json:"detail,omitempty"` // "low", "high", "auto"
}

// ContentString extracts the text portion of a Message.Content field.
// When Content is nil or a plain string, returns it directly.
// When Content is a structured multimodal block, extracts and concatenates text parts.
func ContentString(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []ContentPart:
		var b strings.Builder
		for _, p := range v {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	default:
		return fmt.Sprintf("%v", content)
	}
}

// ContentLen estimates the byte size of Content for token budgeting.
// For multimodal content, text bytes + base64 image bytes (approximate).
func ContentLen(content any) int {
	if content == nil {
		return 0
	}
	switch v := content.(type) {
	case string:
		return len(v)
	case []ContentPart:
		n := 0
		for _, p := range v {
			switch p.Type {
			case "text":
				n += len(p.Text)
			case "image_url":
				if p.ImageURL != nil {
					n += len(p.ImageURL.URL)
				}
			}
		}
		return n
	default:
		return len(fmt.Sprintf("%v", content))
	}
}

// IsTextOnly reports whether Content contains no image parts.
func IsTextOnly(content any) bool {
	_, ok := content.(string)
	return ok
}

// TextContent creates a simple text-only Content value.
func TextContent(text string) any {
	return text
}

// ImageContent creates a multimodal Content value with text and image parts.
func ImageContent(text string, imageURLs ...string) any {
	parts := []ContentPart{{Type: "text", Text: text}}
	for _, url := range imageURLs {
		parts = append(parts, ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: url}})
	}
	return parts
}

// ParseImageDataURL splits a "data:image/png;base64,AAAA..." data URL into its
// MIME type and raw base64 payload. Returns ("", "", false) when the prefix is
// not a valid data URL.
func ParseImageDataURL(dataURL string) (mediaType, base64Data string, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(dataURL, prefix) {
		return "", "", false
	}
	rest := dataURL[len(prefix):]
	idx := strings.Index(rest, ";base64,")
	if idx < 0 {
		return "", "", false
	}
	mediaType = rest[:idx]
	base64Data = rest[idx+len(";base64,"):]
	if mediaType == "" || base64Data == "" {
		return "", "", false
	}
	return mediaType, base64Data, true
}

// ImageParts extracts image ContentParts from a multimodal Content value.
// Returns nil when Content is a plain string or contains no images.
func ImageParts(content any) []ContentPart {
	parts, ok := content.([]ContentPart)
	if !ok {
		return nil
	}
	var imgs []ContentPart
	for _, p := range parts {
		if p.Type == "image_url" && p.ImageURL != nil && p.ImageURL.URL != "" {
			imgs = append(imgs, p)
		}
	}
	return imgs
}

// ToolCall is a tool invocation requested by the model. Arguments is raw JSON.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSchema is a tool definition exposed to the model. Parameters is JSON Schema.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Request is a single completion request.
type Request struct {
	Messages    []Message
	Tools       []ToolSchema
	Temperature float64
	MaxTokens   int
}

// interruptedToolResult stands in for a tool result that never landed — an
// assistant tool_calls turn whose execution was cut short (interrupt, crash) and
// later resumed. Sending such a turn unanswered trips the OpenAI/MoMA 400
// "An assistant message with 'tool_calls' must be followed by tool messages
// responding to each 'tool_call_id'".
const interruptedToolResult = "[no result: the previous turn was interrupted before this tool call completed]"

// SanitizeToolPairing repairs a history for a provider request (every assistant
// tool_calls answered by a following tool message, orphan tool messages dropped,
// empty tool-call names backfilled from results, truncated args closed) right
// before sending it to the wire — without touching the stored session. Kept as a
// distinct name so call sites read as "defensive wire prep" rather than "session
// mutation". Now a thin alias over NormalizeMessages so the wire path and the
// session-load path share one repair implementation.
func SanitizeToolPairing(msgs []Message) []Message { return NormalizeMessages(msgs) }

// repairToolCallArgs returns m with any undecodable tool-call Arguments closed
// into valid JSON (copy-on-write; the caller's history is never mutated). Empty
// arguments pass through — some gateways send "" for no-arg tools.
func repairToolCallArgs(m Message) Message {
	broken := false
	for _, tc := range m.ToolCalls {
		if tc.Arguments != "" && !json.Valid([]byte(tc.Arguments)) {
			broken = true
			break
		}
	}
	if !broken {
		return m
	}
	calls := make([]ToolCall, len(m.ToolCalls))
	copy(calls, m.ToolCalls)
	for i := range calls {
		if calls[i].Arguments == "" || json.Valid([]byte(calls[i].Arguments)) {
			continue
		}
		calls[i].Arguments = closeTruncatedJSON(calls[i].Arguments)
	}
	m.ToolCalls = calls
	return m
}

// closeTruncatedJSON best-effort completes a JSON document cut off mid-stream
// (unterminated string, open braces, dangling comma/colon); anything still
// invalid after closing degrades to "{}".
func closeTruncatedJSON(s string) string {
	var stack []byte
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	out := s
	if esc {
		out = out[:len(out)-1]
	}
	if inStr {
		out += `"`
	}
	trimmed := strings.TrimRight(out, " \t\r\n")
	switch {
	case strings.HasSuffix(trimmed, ","):
		out = trimmed[:len(trimmed)-1]
	case strings.HasSuffix(trimmed, ":"):
		out = trimmed + "null"
	}
	for i := len(stack) - 1; i >= 0; i-- {
		out += string(stack[i])
	}
	if !json.Valid([]byte(out)) {
		return "{}"
	}
	return out
}

// pairToolResults answers each tool_call with its result, backfilling a
// placeholder for any unanswered one. Distinct non-empty ids pair by id (so
// reordered results re-sort to call order); empty or duplicate ids pair by
// position instead — some gateways stream tool calls by index with no id, and a
// map keyed on id would collapse those results into one (call order is preserved
// because the loop appends results in call order).
func pairToolResults(calls []ToolCall, avail []Message) []Message {
	out := make([]Message, 0, len(calls))
	if idDistinct(calls) {
		byID := make(map[string]Message, len(avail))
		for _, r := range avail {
			byID[r.ToolCallID] = r
		}
		for _, tc := range calls {
			if r, ok := byID[tc.ID]; ok {
				out = append(out, r)
			} else {
				out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
			}
		}
		return out
	}
	for k, tc := range calls {
		if k < len(avail) {
			r := avail[k]
			r.ToolCallID = tc.ID
			out = append(out, r)
		} else {
			out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
		}
	}
	return out
}

// idDistinct reports whether every call carries a non-empty id unique within the
// batch — the condition under which id-keyed pairing is safe.
func idDistinct(calls []ToolCall) bool {
	seen := make(map[string]struct{}, len(calls))
	for _, tc := range calls {
		if tc.ID == "" {
			return false
		}
		if _, dup := seen[tc.ID]; dup {
			return false
		}
		seen[tc.ID] = struct{}{}
	}
	return true
}

// ChunkType identifies the kind of a streamed increment.
type ChunkType int

const (
	ChunkText          ChunkType = iota // text delta
	ChunkReasoning                      // thinking-mode reasoning delta (before the visible answer)
	ChunkToolCallStart                  // a tool call has begun (ToolCall: ID+Name; args still streaming)
	ChunkToolCall                       // one complete tool call
	ChunkUsage                          // token usage for the completion
	ChunkDone                           // completion finished normally
	ChunkError                          // an error occurred
)

// Usage reports token accounting for a completion. Cache hit/miss come from
// either MoMA's top-level prompt_cache_{hit,miss}_tokens or the OpenAI/MoMA
// standard prompt_tokens_details.cached_tokens — the openai provider normalises
// both shapes into these fields. Note: MoMA currently does not report cache
// tokens (both fields stay 0); the normalisation is kept for future support.
// ReasoningTokens is the thinking-mode subset of
// CompletionTokens reported by thinking-capable models. FinishReason carries
// the model's last reported choices[0].finish_reason so the agent can surface
// abnormal terminations ("length", "content_filter", "repetition_truncation").
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheHitTokens   int    // prompt tokens served from cache
	CacheMissTokens  int    // prompt tokens not cached
	ReasoningTokens  int    // subset of CompletionTokens spent on chain-of-thought
	FinishReason     string // "stop", "tool_calls", "length", "content_filter", "repetition_truncation", …
}

// Pricing is a provider's per-1M-token rates, used to estimate spend. Currency
// is just a display symbol (default "¥"). toml tags let config decode it.
type Pricing struct {
	CacheHit float64 `toml:"cache_hit"` // per 1M cached prompt tokens
	Input    float64 `toml:"input"`     // per 1M uncached prompt tokens
	Output   float64 `toml:"output"`    // per 1M completion tokens
	Currency string  `toml:"currency"`
}

// Cost estimates the spend for a usage record.
func (p *Pricing) Cost(u *Usage) float64 {
	if p == nil || u == nil {
		return 0
	}
	promptCost := float64(u.CacheHitTokens)*p.CacheHit + float64(u.CacheMissTokens)*p.Input
	// When the provider reports no cache split (both zero), treat all prompt
	// tokens as full-price input — the cost is non-zero even when caching is
	// unavailable or unreported (e.g. MoMA currently omits cache fields).
	if promptCost == 0 && u.PromptTokens > 0 {
		promptCost = float64(u.PromptTokens) * p.Input
	}
	return (promptCost + float64(u.CompletionTokens)*p.Output) / 1e6
}

// Symbol returns the currency display symbol, defaulting to "¥".
// Normalizes common ISO codes to their symbol equivalents.
func (p *Pricing) Symbol() string {
	if p == nil || p.Currency == "" {
		return "¥"
	}
	return currencySymbol(p.Currency)
}

// currencySymbol normalizes common ISO 4217 currency codes to display symbols.
func currencySymbol(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "USD", "US":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "CNY", "RMB":
		return "¥"
	case "JPY":
		return "¥"
	case "KRW":
		return "₩"
	case "₹":
		return "₹"
	default:
		// If the code is already a single Unicode rune (e.g. "€"), pass through.
		if len([]rune(code)) == 1 {
			return code
		}
		return strings.ToUpper(code)
	}
}

// Chunk is a single streamed event. Read the field matching Type.
type Chunk struct {
	Type      ChunkType
	Text      string    // ChunkText, ChunkReasoning
	Signature string    // ChunkReasoning: opaque proof for the reasoning (Anthropic thinking signature), when issued
	ToolCall  *ToolCall // ChunkToolCallStart (ID+Name only), ChunkToolCall (complete)
	Usage     *Usage    // ChunkUsage
	Err       error     // ChunkError
}

// StreamInterruptedError marks a recoverable transport cut that happened after
// the caller had already received model output. Providers must not replay these
// requests themselves because doing so could duplicate visible text or tool
// calls; the agent can append a tail recovery prompt instead.
type StreamInterruptedError struct {
	Err error
}

func (e *StreamInterruptedError) Error() string {
	if e == nil || e.Err == nil {
		return "stream interrupted"
	}
	return e.Err.Error()
}

func (e *StreamInterruptedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func IsStreamInterrupted(err error) bool {
	var interrupted *StreamInterruptedError
	return errors.As(err, &interrupted)
}

// Provider is a chat-capable model backend.
type Provider interface {
	// Name returns the provider instance name, e.g. "MoMA" / "MoMA".
	Name() string
	// Stream starts a streaming completion, pushing increments on the channel.
	// Cancelling ctx must abort the underlying request; a closed channel marks
	// the end of the completion.
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Config is a resolved provider instance configuration.
type Config struct {
	Name    string         // instance name, e.g. "MoMA"
	BaseURL string         // OpenAI-compatible endpoint
	Model   string         // model id
	APIKey  string         // resolved from api_key_env
	Extra   map[string]any // kind-specific options
}

// AuthError reports that a provider rejected the API key (HTTP 401/403). Its
// message is already user-facing and actionable — it names the provider and,
// when known, the environment variable the key comes from — so the CLI can
// surface it verbatim instead of dumping a raw status body. Providers should
// return this (rather than a generic status error) for auth failures.
type AuthError struct {
	Provider string // the provider instance name, e.g. "MoMA"
	KeyEnv   string // the api_key_env the key is read from, when known
	Status   int    // the HTTP status (401 or 403)
	HasKey   bool   // a non-empty key was sent vs. no key configured
}

func (e *AuthError) Error() string {
	key := "the API key"
	if e.KeyEnv != "" {
		key = e.KeyEnv
	}
	return fmt.Sprintf("authentication failed for provider %q (HTTP %d): %s is invalid or expired — update it (in .env or your environment) and retry, or run `momapeer setup`",
		e.Provider, e.Status, key)
}

// Factory builds a Provider from a resolved Config.
type Factory func(cfg Config) (Provider, error)

var registry = map[string]Factory{}

// Register adds a factory under a kind (e.g. "openai"). Intended for init().
// It panics on a duplicate kind, since that is a compile-time wiring mistake.
func Register(kind string, f Factory) {
	if _, dup := registry[kind]; dup {
		panic("provider: duplicate kind " + kind)
	}
	registry[kind] = f
}

// New instantiates the provider of the given kind.
func New(kind string, cfg Config) (Provider, error) {
	f, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("provider: unknown kind %q (registered: %v)", kind, Kinds())
	}
	p, err := f(cfg)
	if err != nil {
		return nil, err
	}
	if nilutil.IsNil(p) {
		return nil, fmt.Errorf("provider: factory %q returned nil provider", kind)
	}
	return p, nil
}

// Kinds returns the registered kinds, sorted.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
