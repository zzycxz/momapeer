package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zzycxz/momapeer/internal/config"
	"github.com/zzycxz/momapeer/internal/tool"
)

// JiutianTools returns the three Jiutian multimodal tools based on config.
func JiutianTools(cfg *config.JiutianConfig) []tool.Tool {
	if cfg == nil {
		return nil
	}
	var tools []tool.Tool
	if cfg.ImageUnderstand {
		tools = append(tools, &imageUnderstand{})
	}
	if cfg.ImageGenerate {
		tools = append(tools, &imageGenerate{})
	}
	if cfg.VideoUnderstand {
		tools = append(tools, &videoUnderstand{})
	}
	return tools
}

// ── Image Understand ─────────────────────────────────────────────────────

type imageUnderstand struct{}

func (*imageUnderstand) Name() string { return "image_understand" }
func (*imageUnderstand) Description() string {
	return "Analyze an image via Jiutian's LLMImage2Text vision model. Accepts base64 data URLs or local file paths. Returns concise text description. Best for: error screenshots, UI mockups, code screenshots, diagrams. Ask specific questions in the prompt."
}
func (*imageUnderstand) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"image":{"type":"string","description":"Image: base64 data URL or local file path"},"prompt":{"type":"string","description":"What to analyze"}},"required":["image","prompt"]}`)
}
func (*imageUnderstand) ReadOnly() bool { return true }

func (*imageUnderstand) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Image  string `json:"image"`
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Image == "" || p.Prompt == "" {
		return "", fmt.Errorf("image and prompt are required")
	}

	var result struct {
		Code   int `json:"code"`
		Result struct {
			Text  string `json:"text"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		} `json:"result"`
	}
	if err := jiutianAPICall(ctx, "POST", "/image/text", map[string]any{
		"model":  "LLMImage2Text",
		"image":  p.Image,
		"prompt": p.Prompt,
		"stream": false,
	}, &result); err != nil {
		return "", err
	}
	if result.Code != 200 {
		return "", fmt.Errorf("jiutian image/text code=%d", result.Code)
	}
	return fmt.Sprintf("%s\n(tokens: %d in, %d out)", result.Result.Text, result.Result.Usage.PromptTokens, result.Result.Usage.CompletionTokens), nil
}

// ── Image Generate ───────────────────────────────────────────────────────

type imageGenerate struct{}

func (*imageGenerate) Name() string { return "image_generate" }
func (*imageGenerate) Description() string {
	return "Generate images via Jiutian's cntxt2image model. Text-to-image or image-to-image. Returns download URLs. Include style, colors, layout in prompt."
}
func (*imageGenerate) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string","description":"Image description"},"reference_image":{"type":"string","description":"Optional: base64 data URL or file path for image-to-image"},"n":{"type":"integer","description":"Number of images (1-4, default 1)"}},"required":["prompt"]}`)
}

// ReadOnly is false: image_generate produces a visible artifact (a saved
// picture rendered under the tool card), so it must not be folded into the
// transcript's quiet read-only batch like grep/ls. Treating it as a
// foreground call keeps its card — and the generated image — visible.
func (*imageGenerate) ReadOnly() bool { return false }

func (*imageGenerate) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Prompt         string `json:"prompt"`
		ReferenceImage string `json:"reference_image"`
		N              int    `json:"n"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if p.N <= 0 {
		p.N = 1
	}
	if p.N > 4 {
		p.N = 4
	}

	payload := map[string]any{"model": "cntxt2image", "prompt": p.Prompt, "n": p.N}
	if p.ReferenceImage != "" {
		payload["image"] = map[string]any{"filePath": p.ReferenceImage}
	}

	var result struct {
		Choices []struct {
			Data []struct {
				URL string `json:"url"`
			} `json:"data"`
			Text         string `json:"text"`
			WidthResult  int    `json:"width_result"`
			HeightResult int    `json:"height_result"`
			RatioResult  string `json:"ratio_result"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			ImageCount       int `json:"imageOrVideoCount"`
		} `json:"usage"`
	}
	if err := jiutianAPICall(ctx, "POST", "/images/generations", payload, &result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 || len(result.Choices[0].Data) == 0 {
		return "", fmt.Errorf("no images generated")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Generated %d image(s):\n", len(result.Choices[0].Data))
	for i, img := range result.Choices[0].Data {
		// The /fs/getFile link requires the API-key Bearer header, so a bare URL
		// answers 401. Download each image with auth and save it under
		// .momapeer/attachments so the user gets an accessible local file; fall
		// back to the raw link (with a note) if the download fails.
		downloadURL := fmt.Sprintf("https://jiutian.10086.cn/largemodel/moma/api/v1/fs/getFile?key=%s", img.URL)
		raw, mime, err := jiutianDownloadFile(ctx, downloadURL)
		if err != nil {
			fmt.Fprintf(&sb, "  %d: %s  (本地保存失败：%v；该链接需带 API Key 访问)\n", i+1, downloadURL, err)
			continue
		}
		rel, err := saveImageAttachment(mime, raw)
		if err != nil {
			fmt.Fprintf(&sb, "  %d: %s  (本地保存失败：%v；该链接需带 API Key 访问)\n", i+1, downloadURL, err)
			continue
		}
		// Output as markdown image syntax so the chat renders the picture inline
		// (Markdown.tsx resolves .momapeer/attachments paths to data URLs). The
		// file is already saved locally by SaveImageBytes, satisfying "save to
		// project + show in chat" without relying on the model echoing anything.
		fmt.Fprintf(&sb, "  %d: ![image](%s)\n", i+1, rel)
		fmt.Fprintf(&sb, "     (saved to %s)\n", rel)
	}
	if result.Choices[0].Text != "" {
		fmt.Fprintf(&sb, "Description: %s\n", result.Choices[0].Text)
	}
	c := result.Choices[0]
	fmt.Fprintf(&sb, "(%dx%d, %s, %d tokens)", c.WidthResult, c.HeightResult, c.RatioResult, result.Usage.CompletionTokens)
	return sb.String(), nil
}

// ── Video Understand ─────────────────────────────────────────────────────

type videoUnderstand struct{}

func (*videoUnderstand) Name() string { return "video_understand" }
func (*videoUnderstand) Description() string {
	return "Analyze a video via Jiutian's video_to_text model. Accepts local file paths (auto-uploaded) or Jiutian server paths. Non-streaming, may take 30-60s."
}
func (*videoUnderstand) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"video":{"type":"string","description":"Local file path or Jiutian server path"},"prompt":{"type":"string","description":"What to analyze"}},"required":["video","prompt"]}`)
}
func (*videoUnderstand) ReadOnly() bool { return true }

func (*videoUnderstand) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Video  string `json:"video"`
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Video == "" || p.Prompt == "" {
		return "", fmt.Errorf("video and prompt are required")
	}

	videoPath := p.Video
	// Auto-upload local files.
	if !strings.Contains(videoPath, "/") || strings.Contains(videoPath, ":") || strings.HasPrefix(videoPath, ".") {
		serverPath, err := jiutianUploadFile(ctx, videoPath)
		if err != nil {
			return "", fmt.Errorf("upload video: %w", err)
		}
		videoPath = serverPath
	}

	var result struct {
		Code   int `json:"code"`
		Result struct {
			Text  string `json:"text"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		} `json:"result"`
	}
	if err := jiutianAPICall(ctx, "POST", "/video/text", map[string]any{
		"model":  "video_to_text",
		"video":  videoPath,
		"prompt": p.Prompt,
	}, &result); err != nil {
		return "", err
	}
	if result.Code != 200 {
		return "", fmt.Errorf("jiutian video/text code=%d", result.Code)
	}
	return fmt.Sprintf("%s\n(tokens: %d in, %d out)", result.Result.Text, result.Result.Usage.PromptTokens, result.Result.Usage.CompletionTokens), nil
}

// ── helpers ──────────────────────────────────────────────────────────────

// ImageToDataURL reads a local file and converts it to a base64 data URL.
func ImageToDataURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	mime := "application/octet-stream"
	switch {
	case len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8:
		mime = "image/jpeg"
	case len(data) > 4 && string(data[:4]) == "\x89PNG":
		mime = "image/png"
	case len(data) > 4 && string(data[:4]) == "GIF8":
		mime = "image/gif"
	case len(data) > 4 && string(data[:4]) == "RIFF":
		mime = "image/webp"
	default:
		ext := strings.TrimPrefix(filepath.Ext(path), ".")
		if ext != "" {
			mime = "image/" + ext
		}
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// attachmentSeq makes saved-image filenames unique within a process.
var attachmentSeq atomic.Uint64

// saveImageAttachment writes image bytes to .momapeer/attachments/ and returns
// the repo-relative path. It mirrors control.SaveImageBytes but lives in this
// package to avoid a builtin→control→agent import cycle (agent's test bundle
// blank-imports builtin). Used by image_generate to persist generated pictures
// so the frontend can render them under the tool card.
func saveImageAttachment(declaredMime string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 10*1024*1024 {
		return "", fmt.Errorf("image must be between 1 byte and 10 MB")
	}
	mime := http.DetectContentType(raw[:min(len(raw), 512)])
	ext := ""
	switch strings.TrimSpace(mime) {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	default:
		return "", fmt.Errorf("generated data is not a supported image (mime=%s)", mime)
	}
	root := filepath.Join(".momapeer", "attachments")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create attachments dir: %w", err)
	}
	for range 1000 {
		seq := attachmentSeq.Add(1)
		name := fmt.Sprintf("generated-%s-%06d%s", time.Now().Format("20060102-150405.000000"), seq, ext)
		rel := filepath.ToSlash(filepath.Join(root, name))
		if err := os.WriteFile(rel, raw, 0o644); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", fmt.Errorf("write attachment: %w", err)
		}
		return rel, nil
	}
	return "", fmt.Errorf("could not allocate a unique attachment path")
}
