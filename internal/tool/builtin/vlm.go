package builtin

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/zzycxz/momapeer/internal/jiutian"
	"github.com/zzycxz/momapeer/internal/provider"
)

// VLM 可切换调用层。screen_perceive 的视觉理解走一条降级链：优先 provider
// 多模态模型（qwen3.6-27b 等），失败时回落到九天 LLMImage2Text。boot.go 根据
// [cowork] vlm_backend/vlm_model 构造链后通过 SetVLMChain 注入。
//
// 链中每个 backend 独立尝试：成功即返回，失败（5xx/超时/空）则尝试下一个，
// 全部失败时返回最后一个错误。九天始终作为链尾兜底（只要有 JIUTIAN_API_KEY）。

// VLMBackendKind identifies one backend in the VLM degradation chain.
type VLMBackendKind int

const (
	// VLMBackendProvider is a provider-layer multimodal chat model (qwen/kimi/etc).
	// The model must be vision-capable; the runner is injected from boot.
	VLMBackendProvider VLMBackendKind = iota
	// VLMBackendJiutian is the 九天 LLMImage2Text endpoint, the default terminal
	// fallback that only needs JIUTIAN_API_KEY.
	VLMBackendJiutian
)

// VLMBackend is one link in the VLM degradation chain. Kind selects the backend;
// Model is the provider model ref (Kind=Provider only); Label is the
// human-readable name surfaced in errors and logs.
type VLMBackend struct {
	Kind  VLMBackendKind
	Model string // Kind=Provider: model ref (e.g. "qwen/qwen3.6-27b")
	Label string // display name for logs/errors
}

var (
	vlmChainMu     sync.RWMutex
	globalVLMChain []VLMBackend
)

// SetVLMChain replaces the VLM degradation chain. boot.go calls this after
// resolving config; the chain always closes with the 九天 terminal fallback so a
// configured primary backend failing doesn't leave screen_perceive blind. An
// empty chain falls back to jiutian-only so CallVLM never has nothing to try.
func SetVLMChain(chain []VLMBackend) {
	vlmChainMu.Lock()
	defer vlmChainMu.Unlock()
	if len(chain) == 0 {
		globalVLMChain = []VLMBackend{{Kind: VLMBackendJiutian, Label: "jiutian-LLMImage2Text"}}
		return
	}
	globalVLMChain = append([]VLMBackend(nil), chain...)
}

// vlmChain returns a snapshot of the current chain under the read lock. Falls
// back to jiutian-only when SetVLMChain was never called (zero value).
func vlmChain() []VLMBackend {
	vlmChainMu.RLock()
	defer vlmChainMu.RUnlock()
	if len(globalVLMChain) == 0 {
		return []VLMBackend{{Kind: VLMBackendJiutian, Label: "jiutian-LLMImage2Text"}}
	}
	return globalVLMChain
}

// CallVLM sends an image (base64 data URL) + prompt to the VLM degradation chain
// and returns the first backend's text response. Each backend is tried in order;
// on failure (error or empty result) the next is attempted, and the last error
// is surfaced only when every backend failed. This is the unified entry for the
// screen_perceive loop.
func CallVLM(ctx context.Context, imgDataURL string, prompt string) (string, error) {
	chain := vlmChain()
	var lastErr error
	for _, b := range chain {
		text, err := callVLMBackend(ctx, b, imgDataURL, prompt)
		if err == nil && strings.TrimSpace(text) != "" {
			return text, nil
		}
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", b.Label, err)
		} else {
			lastErr = fmt.Errorf("%s: empty response", b.Label)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no VLM backend configured")
	}
	return "", lastErr
}

// callVLMBackend dispatches one backend. Kept separate so CallVLM's retry loop
// stays uniform regardless of Kind.
func callVLMBackend(ctx context.Context, b VLMBackend, imgDataURL, prompt string) (string, error) {
	switch b.Kind {
	case VLMBackendProvider:
		return callProviderVLM(ctx, b.Model, imgDataURL, prompt)
	case VLMBackendJiutian:
		return callJiutianVLM(ctx, imgDataURL, prompt)
	default:
		return "", fmt.Errorf("unknown VLM backend kind %d", b.Kind)
	}
}

// callJiutianVLM uses the 九天 /image/text endpoint (LLMImage2Text). Terminal
// fallback — only needs JIUTIAN_API_KEY, no provider config.
func callJiutianVLM(ctx context.Context, imgDataURL string, prompt string) (string, error) {
	req := struct {
		Model  string `json:"model"`
		Image  string `json:"image"`
		Prompt string `json:"prompt"`
		Stream bool   `json:"stream"`
	}{
		Model:  "LLMImage2Text",
		Image:  imgDataURL,
		Prompt: prompt,
		Stream: false,
	}
	var resp struct {
		Result struct {
			Text string `json:"text"`
		} `json:"result"`
	}
	if err := jiutian.APICall(ctx, "POST", "/image/text", req, &resp); err != nil {
		return "", fmt.Errorf("jiutian VLM: %w", err)
	}
	return resp.Result.Text, nil
}

// callProviderVLM uses the provider layer's multimodal chat (qwen/kimi/etc). The
// provider already supports image_url content parts (provider.ImageContent); we
// construct a multimodal user message and run it through the injected runner.
// The model must be vision-capable (provider.Vision=true), enforced by boot.
func callProviderVLM(ctx context.Context, model, imgDataURL, prompt string) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("vlm_model is empty — set [cowork] vlm_model to a vision-capable model (e.g. qwen/qwen3.6-27b)")
	}
	content := provider.ImageContent(prompt, imgDataURL)
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: content},
	}
	resp, err := runProviderChat(ctx, model, msgs)
	if err != nil {
		return "", fmt.Errorf("provider VLM (%s): %w", model, err)
	}
	if len(resp) > 0 {
		return provider.ContentString(resp[0].Content), nil
	}
	return "", nil
}

// runProviderChat is a thin bridge to the provider layer. boot.go injects the
// real runner via SetProviderChatRunner; the zero value returns an error so a
// misconfigured boot surfaces clearly instead of a nil panic.
var runProviderChat = func(ctx context.Context, model string, msgs []provider.Message) ([]provider.Message, error) {
	return nil, fmt.Errorf("provider VLM bridge not initialized")
}

// SetProviderChatRunner injects the provider chat runner from boot.go (which has
// access to the resolved provider config). Avoids a circular import between
// tool/builtin and provider/boot.
func SetProviderChatRunner(fn func(ctx context.Context, model string, msgs []provider.Message) ([]provider.Message, error)) {
	runProviderChat = fn
}
