//go:build windows

package builtin

import (
	"image"
	"image/color"
	"sort"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Set-of-Mark (SoM) labeling: classify UIA elements (A/N/K/U), assign short IDs
// (A, B, ... Z, AA, AB, ...), suppress redundant containers, and draw labeled
// boxes on the screenshot. This is the Go port of Rooster's ElitePainter
// (src/utils/vision/painter.py) — the algorithm is identical, only the language
// changed. The labeled image + element list together form the VLM's input: the
// VLM sees numbered boxes on the screen and a text list mapping each number to
// a control type + name, so it can select by number instead of guessing pixels.

// --- A/N/K/U classification (Rooster _ACTION_TYPES etc.) ---

var (
	somActionTypes = []string{"button", "menuitem", "checkbox", "hyperlink", "tab", "tabitem",
		"splitbutton", "link", "treeitem", "listitem", "image", "radiobutton"}
	somKeyinTypes = []string{"edit", "combobox", "spinner", "slider"}
	somNavTypes   = []string{"list", "tree", "menu", "toolbar"}
)

// classifyElement returns "A" (action: buttons/links), "K" (key-in: inputs),
// "N" (navigation: lists/menus), or "U" (unknown). Uses strings.Contains
// substring matching (same as Rooster — "hyperlink" matches "customhyperlink").
func classifyElement(typeName string) string {
	t := strings.ToLower(typeName)
	for _, k := range somActionTypes {
		if strings.Contains(t, k) {
			return "A"
		}
	}
	for _, k := range somKeyinTypes {
		if strings.Contains(t, k) {
			return "K"
		}
	}
	for _, k := range somNavTypes {
		if strings.Contains(t, k) {
			return "N"
		}
	}
	return "U"
}

// --- ID assignment (Rooster Base32 encoding) ---

// Base32 alphabet without easily-confused chars (no I/O/0/1), same as Rooster.
const somBase32 = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
const somAlpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// LabeledElement is a UIAElement augmented with SoM classification + ID.
type LabeledElement struct {
	UIAElement
	Category string `json:"category"` // A/K/N/U
	ID       string `json:"id"`       // "A", "B", "AA", ...
}

// prepareLabels classifies elements, suppresses redundant containers, assigns IDs.
// Returns the labeled (surviving) elements in display order (top-to-bottom, L-to-R).
func prepareLabels(els []UIAElement) []LabeledElement {
	// Classify + mark containers.
	type tmp struct {
		el         UIAElement
		cat        string
		container  bool
		suppressed bool
	}
	tmps := make([]tmp, len(els))
	for i, el := range els {
		cat := classifyElement(el.Type)
		tmps[i] = tmp{
			el:        el,
			cat:       cat,
			container: containerTypes[el.Type],
		}
	}

	// Selective Focus suppression: when a container fully contains a smaller
	// element, suppress the container (keep the detail). Same as Rooster
	// painter.py:114-116 — the LARGE box is eaten, the small element stays.
	// Sort by area descending so we process biggest first.
	sort.SliceStable(tmps, func(i, j int) bool {
		areaI := (tmps[i].el.Box[2] - tmps[i].el.Box[0]) * (tmps[i].el.Box[3] - tmps[i].el.Box[1])
		areaJ := (tmps[j].el.Box[2] - tmps[j].el.Box[0]) * (tmps[j].el.Box[3] - tmps[j].el.Box[1])
		return areaI > areaJ
	})
	for i := range tmps {
		if tmps[i].suppressed || !tmps[i].container {
			continue
		}
		for j := range tmps {
			if i == j || tmps[j].suppressed {
				continue
			}
			// 5px tolerance containment check (same as Rooster).
			if tmps[j].el.Box[0] >= tmps[i].el.Box[0]-5 &&
				tmps[j].el.Box[1] >= tmps[i].el.Box[1]-5 &&
				tmps[j].el.Box[2] <= tmps[i].el.Box[2]+5 &&
				tmps[j].el.Box[3] <= tmps[i].el.Box[3]+5 {
				tmps[i].suppressed = true // suppress the container
				break
			}
		}
	}

	// Collect surviving elements.
	var survivors []tmp
	for _, t := range tmps {
		if !t.suppressed {
			survivors = append(survivors, t)
		}
	}

	// Sort by position (Y//20 buckets, then X) for ID assignment.
	sort.SliceStable(survivors, func(i, j int) bool {
		bi := survivors[i].el.Center[1] / 20
		bj := survivors[j].el.Center[1] / 20
		if bi != bj {
			return bi < bj
		}
		return survivors[i].el.Center[0] < survivors[j].el.Center[0]
	})

	// Assign IDs: first 26 get single letters A-Z, rest get double letters.
	result := make([]LabeledElement, 0, len(survivors))
	for i, s := range survivors {
		var id string
		if i < 26 {
			id = string(somAlpha[i])
		} else {
			idx := i - 26
			first := (idx / 32) % 26
			second := idx % 32
			id = string(somAlpha[first]) + string(somBase32[second])
		}
		result = append(result, LabeledElement{
			UIAElement: s.el,
			Category:   s.cat,
			ID:         id,
		})
	}
	return result
}

// --- Screenshot drawing (draw boxes + labels) ---

var somYellow = color.RGBA{R: 255, G: 255, B: 0, A: 255}
var somBlack = color.RGBA{R: 0, G: 0, B: 0, A: 255}

// drawLabels draws yellow boxes + letter labels on a COPY of the screenshot.
// Each element gets a 1px yellow outline + a small yellow tag with its ID in
// the top-left corner. Same visual style as Rooster's ElitePainter.draw_labels.
func drawLabels(img *image.RGBA, els []LabeledElement) *image.RGBA {
	// Work on a copy to preserve the original for re-use.
	out := cloneRGBA(img)

	for _, el := range els {
		x1, y1 := el.Box[0], el.Box[1]
		x2, y2 := el.Box[2], el.Box[3]
		if x1 < 0 || y1 < 0 || x2 <= x1 || y2 <= y1 {
			continue
		}
		// 1px yellow outline (4 edges).
		drawHLine(out, x1, y1, x2, somYellow)
		drawHLine(out, x1, y2-1, x2, somYellow)
		drawVLine(out, x1, y1, y2, somYellow)
		drawVLine(out, x2-1, y1, y2, somYellow)

		// Label tag: yellow rectangle with black text, above the box (or below
		// if the box is at the top edge).
		labelW := 14
		if len(el.ID) > 1 {
			labelW = 24
		}
		labelH := 16
		ly := y1 - labelH
		if ly < 0 {
			ly = y1 // place below if at top edge
		}
		fillRect(out, x1, ly, x1+labelW, ly+labelH, somYellow)
		drawText(out, x1+2, ly+1, el.ID, somBlack)
	}
	return out
}

// cloneRGBA returns a deep copy of an RGBA image.
func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func drawHLine(img *image.RGBA, x1, y, x2 int, c color.Color) {
	b := img.Bounds()
	if y < b.Min.Y || y >= b.Max.Y {
		return
	}
	for x := maxInt(x1, b.Min.X); x < minInt(x2, b.Max.X); x++ {
		img.Set(x, y, c)
	}
}

func drawVLine(img *image.RGBA, x, y1, y2 int, c color.Color) {
	b := img.Bounds()
	if x < b.Min.X || x >= b.Max.X {
		return
	}
	for y := maxInt(y1, b.Min.Y); y < minInt(y2, b.Max.Y); y++ {
		img.Set(x, y, c)
	}
}

func fillRect(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	b := img.Bounds()
	for y := maxInt(y1, b.Min.Y); y < minInt(y2, b.Max.Y); y++ {
		for x := maxInt(x1, b.Min.X); x < minInt(x2, b.Max.X); x++ {
			img.Set(x, y, c)
		}
	}
}

func drawText(img *image.RGBA, x, y int, text string, c color.Color) {
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y + 12)},
	}
	d.DrawString(text)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
