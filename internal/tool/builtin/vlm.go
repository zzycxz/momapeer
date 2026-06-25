package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zzycxz/momapeer/internal/jiutian"
	"github.com/zzycxz/momapeer/internal/provider"
)

// VLM 可切换调用层。默认走九天 LLMImage2Text，可切换到 provider 多模态模型
// （minimax/minimax-m2.7, moonshotai/kimi-k2.6 等），传 base64 图片。
//
// 配置（[cowork] vlm_backend / vlm_model）注入 boot.go。

var globalVLMConfig VLMConfig

// VLMConfig selects which backend the screen_perceive loop uses for visual
// understanding. Backend "jiutian" = 九天 LLMImage2Text (default, 已有);
// Backend "provider" = 走 provider 层多模态聊天（minimax/kimi 等）。
type VLMConfig struct {
	Backend string `json:"backend"` // "jiutian" | "provider"
	Model   string `json:"model"`   // provider model ref (backend=provider 时)
}

// SetVLMConfig injects the VLM backend selection. Called from boot.go.
func SetVLMConfig(c VLMConfig) { globalVLMConfig = c }

// CallVLM sends an image (base64 data URL) + prompt to the configured VLM and
// returns the text response. This is the unified entry for the screen_perceive
// loop — it abstracts whether 九天 or a provider model handles the call.
func CallVLM(ctx context.Context, imgDataURL string, prompt string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(globalVLMConfig.Backend)) {
	case "provider":
		return callProviderVLM(ctx, globalVLMConfig.Model, imgDataURL, prompt)
	default:
		return callJiutianVLM(ctx, imgDataURL, prompt)
	}
}

// callJiutianVLM uses the existing 九天 /image/text endpoint (LLMImage2Text).
// This is the default backend — no config needed, works with existing API key.
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

// callProviderVLM uses the provider layer's multimodal chat (minimax/kimi/etc).
// The provider already supports image_url content parts (provider.ImageContent);
// we construct a multimodal user message and run it through the standard chat
// completion path. The model must be vision-capable (provider.Vision=true).
func callProviderVLM(ctx context.Context, model, imgDataURL, prompt string) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("vlm_model is empty — set [cowork] vlm_model to a vision-capable model (e.g. minimax/minimax-m2.7)")
	}
	// Build the multimodal message: text prompt + image.
	content := provider.ImageContent(prompt, imgDataURL)
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: content},
	}
	// We need a provider client to run the chat. The global controller's
	// provider isn't directly accessible here; instead we resolve the model to
	// a provider entry and create a one-shot client. This mirrors how
	// jiutian.APICall works (standalone HTTP, not tied to a controller).
	resp, err := runProviderChat(ctx, model, msgs)
	if err != nil {
		return "", fmt.Errorf("provider VLM (%s): %w", model, err)
	}
	// Extract text from the response.
	if len(resp) > 0 {
		return provider.ContentString(resp[0].Content), nil
	}
	return "", nil
}

// runProviderChat is a thin bridge to the provider layer. It resolves the model
// ref to a provider entry and runs a single chat completion. Implemented in
// vlm_provider.go to keep the dispatch logic localized.
var runProviderChat = func(ctx context.Context, model string, msgs []provider.Message) ([]provider.Message, error) {
	return nil, fmt.Errorf("provider VLM bridge not initialized")
}

// SetProviderChatRunner injects the provider chat runner from boot.go (which has
// access to the resolved provider config). This avoids a circular import between
// tool/builtin and provider.
func SetProviderChatRunner(fn func(ctx context.Context, model string, msgs []provider.Message) ([]provider.Message, error)) {
	runProviderChat = fn
}

// suppress unused import if json isn't referenced in some build paths.
var _ = json.Marshal
