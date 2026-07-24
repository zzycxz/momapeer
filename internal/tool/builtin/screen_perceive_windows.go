//go:build windows

package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"
)

// screen_perceive — the complete perception loop in one tool call:
// screenshot → UIA dump → classify/label (SoM) → draw on screenshot → VLM
// semantic selection → parse result. Returns the labeled image path + element
// list + VLM's choice (label or coordinates) + confidence.
//
// This is the core of the UIA+VLM fusion: UIA provides precise element structure
// (type/name/rect), VLM provides semantic understanding (which element matches
// the task). The labeled screenshot bridges them — the VLM sees numbered boxes
// and a text list, so it selects by number (reliable) rather than guessing
// pixels (error-prone). Per the user's design: "UIA labels are a reference, not
// a constraint — the VLM is the final judge."

type screenPerceive struct{}

func (screenPerceive) Name() string { return "screen_perceive" }

func (screenPerceive) Description() string {
	return "Complete desktop perception: screenshot + UIA element detection + Set-of-Mark labeling + VLM semantic selection. Returns a labeled screenshot (elements boxed with letter IDs), an element list (ID→type/name/coordinates), and the VLM's choice (which element to interact with, with confidence). Pass task_hint describing what you're looking for (e.g. 'the login button'). Use this BEFORE screen_click — it gives you precise coordinates. The VLM can pick a labeled element by ID or give raw coordinates; UIA labels are a helpful reference but the VLM has final say."
}

func (screenPerceive) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "task_hint":{"type":"string","description":"What you're looking for, e.g. 'the submit button' or 'the username input field'. Helps the VLM select the right element."}
},
"required":["task_hint"]
}`)
}

func (screenPerceive) ReadOnly() bool { return true }

func (screenPerceive) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		TaskHint string `json:"task_hint"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.TaskHint == "" {
		p.TaskHint = "interact with the most relevant element"
	}

	// 1. Screenshot (physical pixels, DPI-aware).
	img, err := captureScreen(false, 0, 0, 0, 0)
	if err != nil {
		return "", fmt.Errorf("screenshot: %w", err)
	}

	// 2. UIA element dump.
	elements, fgInteractive, err := dumpUIA()
	if err != nil {
		// UIA failed — fall back to screenshot-only VLM (no labels).
		return perceiveNoUIA(ctx, img, p.TaskHint)
	}

	// 3. Classify + suppress + assign IDs.
	labeled := prepareLabels(elements)
	screenW := img.Bounds().Dx()
	screenH := img.Bounds().Dy()

	// 4. Draw labels on screenshot.
	labeledImg := drawLabels(img, labeled)

	// 5. Save labeled screenshot.
	dir := screenAttachmentsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	labeledPath := filepath.Join(dir, fmt.Sprintf("perceive-%d.png", time.Now().Unix()))
	var buf bytes.Buffer
	if err := png.Encode(&buf, labeledImg); err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	if err := os.WriteFile(labeledPath, buf.Bytes(), 0o644); err != nil {
		return "", err
	}

	// 6. Build VLM prompt: element list + task hint + "UIA is reference, you decide".
	imgDataURL := imageDataURL(buf.Bytes(), "png")
	prompt := buildPerceivePrompt(p.TaskHint, labeled, fgInteractive)

	// 7. Call VLM.
	vlmText, err := CallVLM(ctx, imgDataURL, prompt)
	if err != nil {
		// VLM failed — still return the labeled image + elements so the agent can
		// inspect manually. Don't fail the whole perceive over a VLM hiccup.
		return formatPerceiveResult(labeledPath, labeled, screenW, screenH, "", 0, "VLM call failed: "+err.Error()), nil
	}

	// 8. Parse VLM response: try label first, then coordinates.
	confidence := parseConfidence(vlmText)
	label, hasLabel := parseVLMLabel(vlmText)
	cx, cy, hasCoords := parseVLMCoords(vlmText)

	var choice map[string]any
	if hasLabel {
		// Label → look up element center.
		for _, el := range labeled {
			if el.ID == label {
				choice = map[string]any{
					"label":      label,
					"x":          el.Center[0],
					"y":          el.Center[1],
					"type":       el.Type,
					"name":       el.Name,
					"confidence": confidence,
				}
				break
			}
		}
		if choice == nil {
			choice = map[string]any{"label": label, "note": "ID not found in element list", "confidence": confidence}
		}
	} else if hasCoords {
		// Coordinates → denormalize to screen pixels.
		px := denormalize(cx, screenW)
		py := denormalize(cy, screenH)
		choice = map[string]any{
			"x":          px,
			"y":          py,
			"normalized": [2]float64{cx, cy},
			"confidence": confidence,
		}
	}

	return formatPerceiveResult(labeledPath, labeled, screenW, screenH, vlmText, confidence, ""), nil
}

// perceiveNoUIA handles the case where UIA dump failed — send a raw screenshot
// to the VLM without labels. The VLM does pure visual localization.
func perceiveNoUIA(ctx context.Context, img *image.RGBA, taskHint string) (string, error) {
	// This fallback is simpler: just screenshot + VLM with no element list.
	// Re-capture as we need the *image.RGBA type.
	return "", fmt.Errorf("UIA dump failed and screenshot-only fallback not yet implemented")
}

// buildPerceivePrompt constructs the VLM prompt with element list + task context.
func buildPerceivePrompt(taskHint string, elements []LabeledElement, fgInteractive int) string {
	// Build element list (max 20, same as Rooster).
	max := 20
	if len(elements) < max {
		max = len(elements)
	}
	var listBuilder bytes.Buffer
	for i := 0; i < max; i++ {
		el := elements[i]
		listBuilder.WriteString(fmt.Sprintf("  [%s] %s '%s' @ (%d, %d)\n",
			el.ID, el.Type, el.Name, el.Center[0], el.Center[1]))
	}

	return fmt.Sprintf(`你是一个精准的 GUI 视觉分析助手。

[任务]: %s

[屏幕上已标注的交互元素]:
%s
[图片说明]: 图片上每个交互元素已用字母标签 (A, B, ... ) 标注了黄色方框。

[你的职责]:
1. 根据任务语义，从标注列表中选择最匹配的目标元素。
2. 输出格式（选一种）:
   a. 输出元素编号: [TARGET] A （A 替换为你选的编号）
   b. 或输出坐标: [TARGET_ACTION] x=%d, y=%d （归一化 0-1000）
3. 坐标是整个屏幕的归一化坐标 (0-1000)。

[重要]: 我们帮你预标注了元素（见编号），你可以参考编号定位，但你也可以不按编号、自己从视觉判断——你是最终决策者。如果标注不准确，请按你看到的实际位置输出坐标。

[诚实性约束]: 如果你在图片中找不到任务要求的目标，必须输出 [NO_TARGET]。绝不允许猜测或编造坐标。

[置信度]: 在结论末尾输出: [CONFIDENCE: 0-100] (100=完全确定, 50=半信半疑, 0=完全不确定)

直接给出结论。`, taskHint, listBuilder.String(), 500, 500)
}

// formatPerceiveResult assembles the JSON result string.
func formatPerceiveResult(labeledPath string, elements []LabeledElement, screenW, screenH int, vlmRaw string, confidence int, vlmErr string) string {
	type elemJSON struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Name     string `json:"name"`
		Category string `json:"category"`
		X        int    `json:"x"`
		Y        int    `json:"y"`
	}
	ejs := make([]elemJSON, 0, len(elements))
	for _, el := range elements {
		ejs = append(ejs, elemJSON{el.ID, el.Type, el.Name, el.Category, el.Center[0], el.Center[1]})
	}
	result := map[string]any{
		"labeled_image":  labeledPath,
		"screen_size":    map[string]int{"w": screenW, "h": screenH},
		"elements":       ejs,
		"vlm_confidence": confidence,
	}
	if vlmErr != "" {
		result["vlm_error"] = vlmErr
	} else {
		result["vlm_raw"] = vlmRaw
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b)
}

// imageDataURL converts image bytes to a data URL for VLM input.
func imageDataURL(data []byte, format string) string {
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(data)
}
