// Package openai implements the OpenAI-compatible /chat/completions provider.
// It self-registers under the "openai" kind, so MoMA (九天), MiniMax-M3, and
// any other OpenAI-compatible endpoint are just config instances rather than
// code. Each instance picks the wire shape from its base URL:
//   - api.jiutian.10086.cn → emits thinking.type=enabled (MoMA-flavor CoT) plus
//     thinking_effort as a depth hint.
//   - api.minimaxi.com → emits thinking.type=adaptive|disabled (M3's binary
//     knob) instead of reasoning_effort, since M3 has no level scale.
//   - everything else (MoMA and other OpenAI-compatible gateways) uses the
//     vanilla reasoning_effort scale (low/medium/high).
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zzycxz/momapeer/internal/jiutian"
	"github.com/zzycxz/momapeer/internal/netclient"
	"github.com/zzycxz/momapeer/internal/provider"
)

// defaultStreamIdleTimeout caps how long a started SSE stream may go without any
// bytes before it's treated as a dropped connection. A half-open TCP connection
// (e.g. a proxy switched mid-stream) sends no RST, so scanner.Scan() would block
// forever; this turns that hang into a recoverable error. Generous on purpose —
// live streams emit tokens/keepalives far more often. Stored per-client
// (client.idleTimeout) so a test can shorten it without a shared global that
// would race other streams' watchdogs.
// NOTE: Increased from 120s to 180s to accommodate slower models (e.g. glm-5.1)
// that may take longer to generate responses, especially for complex tasks like
// PPT generation.
const defaultStreamIdleTimeout = 180 * time.Second

func init() {
	provider.Register("openai", New)
}

// New builds an OpenAI-compatible provider from a resolved config.
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("openai: base_url is required for provider %q", cfg.Name)
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("openai: model is required for provider %q", cfg.Name)
	}
	name := cfg.Name
	if name == "" {
		name = "openai"
	}
	keyEnv, _ := cfg.Extra["api_key_env"].(string) // for actionable auth errors
	effort, _ := cfg.Extra["effort"].(string)
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "auto" {
		effort = ""
	}
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	protocol = normalizeReasoningProtocol(protocol)
	// moma is true when the provider uses MoMA-compatible thinking mode.
	// For MoMA, only thinking-capable models (registered in MoMAThinkingModels)
	// get thinking enabled — non-reasoning models (qwen, glm, etc.) would 400.
	//
	// SECURITY/RELIABILITY: a model in MoMAHarmfulThinkingModels is excluded
	// from thinking EVEN when the user explicitly sets reasoning_protocol=moma.
	// Without this guard, glm-5.2 + explicit moma short-circuits past the
	// MoMAThinkingModels allow-list (line 71's first term) and sends thinking
	// params to a model the MoMA platform hangs on — a 180s stall with a
	// misleading "stream stalled" error. See audit finding B4.
	modelKey := strings.ToLower(strings.TrimSpace(cfg.Model))
	moma := (protocol == "moma" || (protocol == "" && IsMoMA(cfg.BaseURL) && MoMAThinkingModels[modelKey])) && !MoMAHarmfulThinkingModels[modelKey]
	minimax := protocol == "" && IsMiniMax(cfg.BaseURL)
	switch {
	case protocol == "none":
		effort = ""
	case moma:
		switch effort {
		case "", "off": // "off" is a retired level (disabled thinking); fall back to the default depth
			effort = "high"
		case "medium", "high":
			// pass through — universally accepted by all MoMA models (18/18 tested)
		case "low":
			effort = "medium" // low rejected by kimi-k2.6, jiutian-lan-236b; clamp to medium
		case "xhigh", "max":
			effort = "high" // rejected by 16/18 MoMA models; clamp to high
		default:
			return nil, fmt.Errorf("openai: provider %q uses MoMA thinking; effort must be low, medium, or high", name)
		}
	case minimax:
		// M3's knob is binary. The config effort layer normalises user input
		// to "adaptive", "disabled", or "" (== auto). We keep "high"/"max"
		// (legacy MoMA) and "low"/"medium" (Anthropic) out — config-level
		// NormalizeEffort remaps them to "adaptive" already, so anything
		// reaching here is expected to be one of: "", "adaptive", "disabled".
		effort = strings.ToLower(strings.TrimSpace(effort))
		switch effort {
		case "": // auto — leave empty so the wire emits thinking.type=adaptive
		case "adaptive", "disabled":
		default:
			return nil, fmt.Errorf("openai: provider %q uses MiniMax thinking; effort must be adaptive or disabled", name)
		}
	case effort != "":
		// Non-MoMA backends use OpenAI's reasoning_effort scale (low/medium/
		// high); "max" is a MoMA-ism other gateways reject with 400, so clamp it
		// to the OpenAI ceiling and reject other values at boot, not at request time.
		switch effort {
		case "max":
			effort = "high"
		case "low", "medium", "high":
		default:
			return nil, fmt.Errorf("openai: provider %q: effort must be low, medium, or high", name)
		}
	}
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("openai: network: %w", err)
	}
	vision, _ := cfg.Extra["vision"].(bool)
	visionDetail, _ := cfg.Extra["vision_detail"].(string)
	if visionDetail == "" {
		visionDetail = "auto"
	}
	imageUnderstand, _ := cfg.Extra["jiutian_image_understand"].(bool)
	return &client{
		name:            name,
		apiKey:          cfg.APIKey,
		keyEnv:          keyEnv,
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		model:           cfg.Model,
		moma:            moma,
		minimax:         minimax,
		effort:          effort,
		vision:          vision,
		visionDetail:    visionDetail,
		imageUnderstand: imageUnderstand,
		http:            httpClient,
		idleTimeout:     defaultStreamIdleTimeout,
	}, nil
}

func newHTTPClient(cfg provider.Config) (*http.Client, error) {
	spec, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	return netclient.NewHTTPClient(spec, netclient.TransportOptions{
		DialTimeout:           30 * time.Second,
		KeepAlive:             30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 180 * time.Second, // models can think for a while before the first token; increased for slow models
	})
}

type client struct {
	name            string
	apiKey          string
	keyEnv          string // api_key_env name, surfaced in auth errors
	baseURL         string
	model           string
	http            *http.Client
	moma            bool
	minimax         bool                            // true for api.minimaxi.com — emits MiniMax-M3's thinking knob instead of reasoning_effort
	effort          string                          // reasoning_effort for OpenAI; thinking.type for MiniMax; "" = auto/provider default
	vision          bool                            // true when the provider supports image_url content parts
	visionDetail    string                          // "auto", "low", "high" — forwarded as image detail level
	imageUnderstand bool                            // true when Jiutian image_understand tool is enabled (auto-degradation)
	idleTimeout     time.Duration                   // SSE stall watchdog window; defaultStreamIdleTimeout unless a test overrides
	authed          atomic.Bool                     // true after first successful auth; enables transient 401 retry
	onReplay        func(ctx context.Context) error // optional: charged by the RPM limiter on each mid-stream replay
}

func (c *client) Name() string { return c.name }

// SetOnReplay installs a hook fired immediately before each mid-stream
// reconnect replay (the streamWithReconnect path). The RPM rate limiter sets
// it so a replay — which sends a fresh HTTP request to the gateway — draws a
// second slot from the budget, matching the real request count the provider
// meters. Without this, one user-facing Stream() call could issue up to
// maxStreamReconnects+1 gateway requests while the budget counted only one.
func (c *client) SetOnReplay(fn func(ctx context.Context) error) { c.onReplay = fn }

func normalizeReasoningProtocol(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "moma", "openai", "none":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

// MoMAThinkingModels lists MoMA-hosted models that support MoMA-compatible
// thinking mode. Only these models receive the thinking: {type: "enabled"} parameter.
// This is the single source of truth; config/effort.go derives its map from this.
// Note: this controls the REQUEST-side thinking field, not the RESPONSE-side
// reasoning_content/reasoning fields (which are driven by whatever the model returns).
// Models verified to return reasoning_content via MoMA platform test (2026-06-13)
var MoMAThinkingModels = map[string]bool{
	"jiutian/jiutian-lan-thinking": true,
	"jiutian/jiutian-da-35b":       true,
	"qwen/qwen3.6-35b":             true,
	"qwen/qwen3.6-27b":             true,
	"qwen/qwen3.5-397b-a17b":       true,
	"z.ai/glm-5.1":                 true,
	// "z.ai/glm-5.2": removed — MoMA platform hangs (no response) when
	// thinking parameters are sent to glm-5.2; glm-5.1 works fine.
	"minimax/minimax-m2.7":          true,
	"minimax/minimax-m2.5":          true,
	"moonshotai/kimi-k2.6":          true,
	"moonshotai/kimi-k2.5-thinking": true,
	"openai/gpt-oss-120b":           true,
}

// MoMAHarmfulThinkingModels lists models the MoMA platform HANGS on (no
// response, eventually times out) when thinking parameters are sent. These are
// excluded from moma thinking even when the user explicitly sets
// reasoning_protocol = "moma" — that explicit setting would otherwise bypass
// the MoMAThinkingModels allow-list above. Each entry must reference a real
// platform behavior with a documented failure mode.
var MoMAHarmfulThinkingModels = map[string]bool{
	// glm-5.2: verified platform hang (2026-06-13). glm-5.1 works fine, so
	// this is model-specific. The user experiences a ~180s idle-timeout stall
	// reported as "stream stalled" rather than a clear "thinking not supported".
	"z.ai/glm-5.2": true,
}

// MoMAVisionModels lists MoMA-hosted models that support image_url content
// parts (multimodal / vision). Models NOT in this list will reject image input
// with a 400 error ("不支持的消息部件类型 image_url"). Users can override
// per-provider with vision = true in config.
// Verified via MoMA platform API test with 100x100 PNG (2026-06-17).
var MoMAVisionModels = map[string]bool{
	"qwen/qwen3.5-397b-a17b": true, // Qwen-VL series
	"qwen/qwen3.6-35b":       true, // Qwen3.6 (vision confirmed)
	"qwen/qwen3.6-27b":       true, // Qwen3.6 (vision confirmed)
	"moonshotai/kimi-k2.6":   true, // Kimi (vision confirmed)
}

// ModelSupportsVision reports whether the given model ID supports image input.
// Checks the provider-level override first, then falls back to the model registry.
func ModelSupportsVision(modelID string, providerOverride bool) bool {
	if providerOverride {
		return true
	}
	return MoMAVisionModels[strings.ToLower(strings.TrimSpace(modelID))]
}

// bufPool reuses byte buffers for JSON-marshalled request bodies. Each turn
// allocates a buffer, marshals the request, and sends it — pooling avoids the
// GC churn from repeated alloc/free of ~10-100KB buffers. The pool is
// provider-level (not global) so OpenAI and Anthropic don't compete.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func (c *client) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	// When the model doesn't support vision, automatically analyze each image
	// via the VLM degradation chain (qwen → 九天, injected via SetVLMBridge) and
	// replace image parts with the text descriptions. Only when the feature is
	// enabled in config ([jiutian] image_understand) — that toggle is the only
	// remaining use of the field after image_understand was globally unified.
	if !ModelSupportsVision(c.model, c.vision) && c.imageUnderstand {
		analyzed := false
		imageCount := 0
		// Count images first.
		for _, m := range req.Messages {
			if m.Role != provider.RoleUser {
				continue
			}
			if parts, ok := m.Content.([]provider.ContentPart); ok {
				for _, p := range parts {
					if p.Type == "image_url" && p.ImageURL != nil {
						imageCount++
					}
				}
			}
		}
		// Inject a status message so the user sees progress while waiting.
		if imageCount > 0 {
			noun := "image"
			if imageCount > 1 {
				noun = "images"
			}
			// Deep-copy the slice to avoid aliasing the caller's backing array.
			// req is a value type but req.Messages is a slice header sharing the
			// same underlying array.  If the original had spare capacity (cap > len),
			// an in-place append would corrupt the caller's data.
			msgs := make([]provider.Message, len(req.Messages))
			copy(msgs, req.Messages)
			req.Messages = msgs

			statusMsg := fmt.Sprintf("[Analyzing %d %s via vision model...]", imageCount, noun)
			req.Messages = append(req.Messages[:len(req.Messages)-1],
				provider.Message{Role: provider.RoleAssistant, Content: statusMsg},
				req.Messages[len(req.Messages)-1],
			)
		}
		for i := range req.Messages {
			m := &req.Messages[i]
			if m.Role != provider.RoleUser {
				continue
			}
			parts, ok := m.Content.([]provider.ContentPart)
			if !ok || !hasImageParts(parts) {
				continue
			}
			var replaced []provider.ContentPart
			for _, p := range parts {
				if p.Type == "text" {
					replaced = append(replaced, p)
					continue
				}
				if p.Type == "image_url" && p.ImageURL != nil {
					desc, err := jiutianImageUnderstand(ctx, p.ImageURL.URL)
					if err != nil {
						replaced = append(replaced, provider.ContentPart{
							Type: "text",
							Text: fmt.Sprintf("[Image analysis failed: %v]", err),
						})
					} else {
						replaced = append(replaced, provider.ContentPart{
							Type: "text",
							Text: fmt.Sprintf("[Image content: %s]", desc),
						})
					}
					analyzed = true
				}
			}
			if len(replaced) == 0 {
				replaced = []provider.ContentPart{{Type: "text", Text: "(image attached)"}}
			}
			m.Content = replaced
		}
		if analyzed {
			req.Messages = append(req.Messages[:len(req.Messages)-1],
				provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf(
					"[Image analysis complete. Your model %q does not support native image input, so images were pre-analyzed by a vision model. The descriptions above are the vision model's output — use them to answer the user's question.]",
					c.model,
				)},
				req.Messages[len(req.Messages)-1],
			)
		}
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(c.buildRequest(req)); err != nil {
		bufPool.Put(buf)
		return nil, fmt.Errorf("%s: marshal request: %w", c.name, err)
	}
	body := make([]byte, buf.Len())
	copy(body, buf.Bytes())
	bufPool.Put(buf)

	newReq := func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("Accept", "text/event-stream")
		return httpReq, nil
	}
	resp, err := provider.SendWithRetry(ctx, c.http, provider.SendOptions{
		ProvName:   c.name,
		KeyEnv:     c.keyEnv,
		KeyPresent: c.apiKey != "",
		RetryAuth:  c.authed.Load(),
	}, newReq)
	if err != nil {
		return nil, err
	}
	c.authed.Store(true)

	out := make(chan provider.Chunk)
	go c.streamWithReconnect(ctx, resp, newReq, out)
	return out, nil
}

// maxStreamReconnects bounds how many times a mid-stream connection drop is
// replayed from scratch before the error is surfaced — each replay re-runs the
// whole request (cheap under prompt caching, but not free).
const maxStreamReconnects = 3

// streamWithReconnect drives readStream and, when the connection is cut before
// any model output has been forwarded, replays the request rather than failing
// the turn. Once a token (reasoning/text/tool-call) has been emitted, a replay
// would duplicate output, so the error is surfaced instead.
func (c *client) streamWithReconnect(ctx context.Context, resp *http.Response, newReq func(context.Context) (*http.Request, error), out chan<- provider.Chunk) {
	defer close(out)
	for attempt := 0; ; attempt++ {
		emitted, err := c.readStream(ctx, resp, out)
		if err == nil {
			return
		}
		if !provider.IsConnReset(err) {
			out <- provider.Chunk{Type: provider.ChunkError, Err: err}
			return
		}
		if emitted {
			out <- provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: err}}
			return
		}
		if attempt >= maxStreamReconnects {
			out <- provider.Chunk{Type: provider.ChunkError, Err: err}
			return
		}
		// A replay is a fresh gateway request, so charge the RPM budget for it
		// (the outer Stream() already charged the first attempt). If the budget
		// is exhausted or the context is cancelled, surface that rather than
		// silently bypassing the limit. onReplay is nil when no limiter wraps
		// this client (e.g. tests, or RPM disabled) — then replays are free.
		if c.onReplay != nil {
			if rerr := c.onReplay(ctx); rerr != nil {
				out <- provider.Chunk{Type: provider.ChunkError, Err: rerr}
				return
			}
		}
		next, rerr := provider.SendWithRetry(ctx, c.http, provider.SendOptions{
			ProvName:   c.name,
			KeyEnv:     c.keyEnv,
			KeyPresent: c.apiKey != "",
			RetryAuth:  c.authed.Load(),
		}, newReq)
		if rerr != nil {
			out <- provider.Chunk{Type: provider.ChunkError, Err: rerr}
			return
		}
		resp = next
	}
}

func (c *client) buildRequest(req provider.Request) chatRequest {
	// Repair tool-call pairing before sending: an interrupted/resumed history can
	// carry an assistant tool_calls turn whose results never landed, which MoMA
	// rejects with a 400 ("must be followed by tool messages …").
	src := provider.SanitizeToolPairing(req.Messages)
	msgs := make([]chatMessage, len(src))
	for i, m := range src {
		cm := chatMessage{
			Role:       string(m.Role),
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		// MoMA thinking mode 400s a tool_calls turn whose reasoning_content was
		// dropped on a cache-miss replay ("reasoning_content … must be passed back"),
		// so round it back — but only on the turn that carries the tool calls.
		if c.moma && m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			cm.ReasoningContent = m.ReasoningContent
		}
		for _, tc := range m.ToolCalls {
			wire := chatToolCall{ID: tc.ID, Type: "function"}
			wire.Function.Name = tc.Name
			wire.Function.Arguments = tc.Arguments
			cm.ToolCalls = append(cm.ToolCalls, wire)
		}
		if m.Role != provider.RoleAssistant || len(cm.ToolCalls) == 0 || provider.ContentString(m.Content) != "" {
			cm.Content = m.Content
			// For vision-capable models, convert image content parts to the
			// wire format with detail level. The Stream() early-exit already
			// rejects images for non-vision models, so this only runs when safe.
			if ModelSupportsVision(c.model, c.vision) && m.Role == provider.RoleUser {
				if parts, ok := m.Content.([]provider.ContentPart); ok && hasImageParts(parts) {
					cm.Content = imageContentParts(parts, c.visionDetail)
				}
			}
		}
		msgs[i] = cm
	}

	var tools []chatTool
	for _, t := range req.Tools {
		tools = append(tools, chatTool{
			Type:     "function",
			Function: chatFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}

	out := chatRequest{
		Model:         c.model,
		Messages:      msgs,
		Tools:         tools,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
	}
	switch {
	case c.moma:
		// MoMA uses `thinking_effort` (not OpenAI's `reasoning_effort`) for depth.
		// Thinking is always enabled for MoMA thinking models.
		out.Thinking = &thinkingMode{Type: "enabled"}
		out.ThinkingEffort = c.effort
	case c.minimax:
		// M3 uses a single `thinking.type` field with two valid values:
		// "adaptive" (default, thinking on) and "disabled" (off). Reasoning
		// depth is not a knob on M3, so reasoning_effort is omitted entirely.
		t := c.effort
		if t == "" {
			t = "adaptive" // /effort auto == the M3 model default
		}
		out.Thinking = &thinkingMode{Type: t}
	default:
		// OpenAI-compatible: use standard reasoning_effort field.
		out.ReasoningEffort = c.effort
	}
	return out
}

// readStream parses one SSE response into chunks: text deltas stream live,
// tool-call fragments accumulate by index and emit complete on [DONE], and a
// ChunkToolCallStart fires the moment a call's name is known. It returns whether
// any model output was forwarded (so the caller can decide a replay is safe) and
// the first fatal error — a nil error means the stream reached [DONE].
func (c *client) readStream(ctx context.Context, resp *http.Response, out chan<- provider.Chunk) (emitted bool, _ error) {
	defer resp.Body.Close()

	// Close the response body when the context is canceled (user interrupt) or the
	// stream stalls past c.idleTimeout, so scanner.Scan() unblocks instead of
	// hanging on a half-open connection. done lets the watchdog exit on a normal
	// return — otherwise it outlives the call and blocks forever on a non-cancellable
	// context whose Done() is nil. The watchdog owns the timer; the read loop only
	// pings the buffered activity channel, so there's no Timer.Reset race.
	idleTimeout := c.idleTimeout
	if idleTimeout <= 0 { // zero-value client (constructed without New)
		idleTimeout = defaultStreamIdleTimeout
	}
	done := make(chan struct{})
	defer close(done)
	activity := make(chan struct{}, 1)
	var stalled atomic.Bool
	go func() {
		idle := time.NewTimer(idleTimeout)
		defer idle.Stop()
		for {
			select {
			case <-ctx.Done():
				resp.Body.Close()
				return
			case <-idle.C:
				stalled.Store(true)
				resp.Body.Close()
				return
			case <-activity:
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(idleTimeout)
			case <-done:
				return
			}
		}
	}()

	acc := map[int]*provider.ToolCall{}
	started := map[int]bool{}
	var order []int
	var lastFinishReason string
	var sawDone bool
	var think thinkSplitter

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select { // ping the idle watchdog; non-blocking so a full buffer is fine
		case activity <- struct{}{}:
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}

		var sr streamResponse
		if err := json.Unmarshal([]byte(data), &sr); err != nil {
			return emitted, fmt.Errorf("%s: decode stream: %w", c.name, err)
		}
		if sr.Error != nil {
			return emitted, fmt.Errorf("%s: %s", c.name, sr.Error.Message)
		}
		if len(sr.Choices) > 0 && sr.Choices[0].FinishReason != nil && *sr.Choices[0].FinishReason != "" {
			lastFinishReason = *sr.Choices[0].FinishReason
		}
		if sr.Usage != nil {
			u := normaliseUsage(sr.Usage)
			u.FinishReason = lastFinishReason
			emitted = true
			out <- provider.Chunk{Type: provider.ChunkUsage, Usage: u}
		}
		if len(sr.Choices) == 0 {
			continue
		}

		delta := sr.Choices[0].Delta
		rc := delta.ReasoningContent
		if rc == "" {
			rc = delta.Reasoning
		}
		if rc != "" {
			emitted = true
			out <- provider.Chunk{Type: provider.ChunkReasoning, Text: rc}
		}
		if delta.Content != "" {
			r, txt := think.push(delta.Content)
			if r != "" {
				emitted = true
				out <- provider.Chunk{Type: provider.ChunkReasoning, Text: r}
			}
			if txt != "" {
				emitted = true
				out <- provider.Chunk{Type: provider.ChunkText, Text: txt}
			}
		}
		for _, tc := range delta.ToolCalls {
			cur, ok := acc[tc.Index]
			if !ok {
				cur = &provider.ToolCall{}
				acc[tc.Index] = cur
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Function.Name != "" {
				cur.Name = tc.Function.Name
			}
			cur.Arguments += tc.Function.Arguments
			// Signal the call's start the moment its name is known, so a frontend
			// can show the tool card immediately rather than only after its
			// (possibly large) arguments finish streaming.
			if !started[tc.Index] && cur.Name != "" {
				started[tc.Index] = true
				emitted = true
				out <- provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: cur.ID, Name: cur.Name}}
			}
		}
	}

	if stalled.Load() {
		return emitted, fmt.Errorf("%s: stream stalled — no data for %s, connection likely dropped", c.name, idleTimeout)
	}
	if err := scanner.Err(); err != nil {
		return emitted, fmt.Errorf("%s: read stream: %w", c.name, err)
	}
	// A proxy that idle-closes with a clean FIN ends the scan with no error. Without
	// this check the turn would be committed as complete — including half-streamed
	// tool-call arguments, which then 400 on every replay (#3953).
	if !sawDone && lastFinishReason == "" {
		return emitted, fmt.Errorf("%s: stream ended before completion: %w", c.name, io.ErrUnexpectedEOF)
	}

	if r, txt := think.flush(); r != "" || txt != "" {
		if r != "" {
			out <- provider.Chunk{Type: provider.ChunkReasoning, Text: r}
		}
		if txt != "" {
			out <- provider.Chunk{Type: provider.ChunkText, Text: txt}
		}
	}

	sort.Ints(order)
	for _, idx := range order {
		tc := acc[idx]
		if tc.ID == "" {
			// Some OpenAI-compatible gateways stream tool calls by index with no id.
			// Synthesize a stable one so the result can be paired back to its call —
			// an empty tool_call_id collapses multi-tool turns downstream.
			tc.ID = fmt.Sprintf("call_%d", idx)
		}
		out <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: tc}
	}
	out <- provider.Chunk{Type: provider.ChunkDone}
	return emitted, nil
}

// normaliseUsage folds the two cache-hit shapes the OpenAI-compatible ecosystem
// uses into a single Usage: MoMA puts prompt_cache_{hit,miss}_tokens at the
// top of usage; OpenAI and MoMA put it nested under prompt_tokens_details.
// Whichever side reports non-zero wins; miss is derived when only hit is given.
// Reasoning tokens land in completion_tokens_details on thinking-mode models.
// Note: MoMA currently does not report cache tokens (both fields are 0); the
// normalisation logic is kept for future cache support and for other providers.
func normaliseUsage(u *wireUsage) *provider.Usage {
	hit := u.PromptCacheHitTokens
	miss := u.PromptCacheMissTokens
	if hit == 0 && u.PromptTokensDetails != nil {
		hit = u.PromptTokensDetails.CachedTokens
	}
	if miss == 0 && hit > 0 && u.PromptTokens > hit {
		miss = u.PromptTokens - hit
	}
	reasoning := 0
	if u.CompletionTokensDetails != nil {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	return &provider.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CacheHitTokens:   hit,
		CacheMissTokens:  miss,
		ReasoningTokens:  reasoning,
	}
}

// --- OpenAI-compatible wire protocol ---

type chatRequest struct {
	Model           string         `json:"model"`
	Messages        []chatMessage  `json:"messages"`
	Tools           []chatTool     `json:"tools,omitempty"`
	Stream          bool           `json:"stream"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	Temperature     float64        `json:"temperature,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"` // OpenAI standard
	ThinkingEffort  string         `json:"thinking_effort,omitempty"`  // MoMA platform
	Thinking        *thinkingMode  `json:"thinking,omitempty"`
}

type thinkingMode struct {
	Type string `json:"type"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role string `json:"role"`
	// content is always present (never omitted): MoMA's strict deserializer
	// rejects a message missing the field. A pure tool_calls assistant turn
	// serializes as null (OpenAI-spec, and what strict clones expect); every
	// other role/message serializes as a string, empty included — null is
	// rejected by some backends for a tool message.
	// For multimodal messages, content is an array of ContentPart objects.
	Content          any            `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	Name             string         `json:"name,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content          string         `json:"content"`
			ReasoningContent string         `json:"reasoning_content"`
			Reasoning        string         `json:"reasoning"`
			ToolCalls        []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// wireUsage covers both MoMA's top-level cache fields and the
// OpenAI/MoMA nested details — normaliseUsage chooses whichever side
// reports values.
type wireUsage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	PromptTokensDetails   *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// hasImageParts reports whether any part in the slice is an image_url.
func hasImageParts(parts []provider.ContentPart) bool {
	for _, p := range parts {
		if p.Type == "image_url" && p.ImageURL != nil {
			return true
		}
	}
	return false
}

// chatContentPart is the wire format for a content part in the OpenAI API.
type chatContentPart struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL *chatImageURLPart `json:"image_url,omitempty"`
}

// chatImageURLPart is the wire format for an image_url content part.
type chatImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// imageContentParts converts provider ContentParts to the OpenAI wire format,
// applying the vision detail level to all images.
func imageContentParts(parts []provider.ContentPart, detail string) []chatContentPart {
	out := make([]chatContentPart, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, chatContentPart{Type: "text", Text: p.Text})
		case "image_url":
			if p.ImageURL != nil {
				out = append(out, chatContentPart{
					Type: "image_url",
					ImageURL: &chatImageURLPart{
						URL:    p.ImageURL.URL,
						Detail: detail,
					},
				})
			}
		}
	}
	return out
}

// imageUnderstandPrompt tells the vision model that its output will be consumed
// by a text-only LLM, so it should produce stable, structured descriptions.
const imageUnderstandPrompt = "Your output will be read by a text-only AI model. Describe the image concisely in one paragraph — transcribe all visible text (errors, code, labels) exactly, skip decorative elements and filler phrases, match the dominant language of the text."

// vlmBridge 是注入的图片理解降级链入口。boot.go 启动时把 builtin.CallVLM
// 注入进来，让会话降级复用全局链条（qwen 397B → 27B → 九天）。
// 未注入时（nil）回退到旧的直调九天路径，保持向后兼容。
var vlmBridge func(ctx context.Context, image, prompt string) (string, error)

// SetVLMBridge 注入图片理解降级链。由 boot.go 调用，避免 provider→builtin 循环依赖。
func SetVLMBridge(fn func(ctx context.Context, image, prompt string) (string, error)) {
	vlmBridge = fn
}

// jiutianImageUnderstand calls the configured VLM chain (default qwen → 九天 fallback)
// to describe an image. imageParam can be a base64 data URL or a Jiutian uploaded
// file path. Returns the text description.
//
// 当 vlmBridge 已注入（boot.go 启动后常态），走降级链条；否则回退到直调九天
// 的旧行为，保证测试和未完成初始化的路径不会 NPE。
func jiutianImageUnderstand(ctx context.Context, imageParam string) (string, error) {
	if vlmBridge != nil {
		text, err := vlmBridge(ctx, imageParam, imageUnderstandPrompt)
		if err == nil && strings.TrimSpace(text) != "" {
			return text, nil
		}
		// 桥失败（整条链都失败）→ 落到下面的九天直调做最后兜底。
		// 不在这里 return err，因为旧路径可能仍然可用。
	}
	payload := map[string]any{
		"model":  "LLMImage2Text",
		"image":  imageParam,
		"prompt": imageUnderstandPrompt,
		"stream": false,
	}

	var result struct {
		Code   int `json:"code"`
		Result struct {
			Text string `json:"text"`
		} `json:"result"`
	}
	if err := jiutian.APICall(ctx, "POST", "/image/text", payload, &result); err != nil {
		return "", err
	}
	if result.Code != 200 || result.Result.Text == "" {
		return "", fmt.Errorf("jiutian image/text code=%d, empty text", result.Code)
	}
	return result.Result.Text, nil
}
