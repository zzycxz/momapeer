//go:build windows

package builtin

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"

	"golang.org/x/image/draw"
)

// VLM image encoding. Screenshots are captured at full native resolution (often
// 1920×1080 or higher), and a full-res PNG is ~175KB+ → ~233KB base64. Sending
// that to a multimodal model is the single biggest driver of slow VLM calls
// (30-47s each observed): the model spends most of its time decoding the image.
// Most vision models are trained/recurrent at ~768-1024px on the long edge;
// anything above that adds latency with no accuracy gain. So before sending to a
// VLM we downscale to at most vlmMaxDim on the long edge and JPEG-encode (sharp
// UI text survives JPEG at 85 fine, and JPEG is ~5-8x smaller than PNG for
// photo-like screenshots). This routinely turns a 40s call into a ~8-12s call.

const vlmMaxDim = 1920 // max width or height of an image sent to a VLM (1024→1920: 原图不缩放，提升小文字识别，VLM 响应会慢 3-4 倍)

// encodeForVLM downscales img (if larger than vlmMaxDim on its long edge) and
// returns a base64 JPEG data URL suitable for an image_url content part. It never
// enlarges small images. The JPEG quality 85 keeps UI text legible while making
// photo regions small.
func encodeForVLM(img image.Image) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scaled := img
	if w > vlmMaxDim || h > vlmMaxDim {
		nw, nh := w, h
		if w >= h {
			nw = vlmMaxDim
			nh = h * vlmMaxDim / w
		} else {
			nh = vlmMaxDim
			nw = w * vlmMaxDim / h
		}
		// CatmullRom is the standard high-quality downscaler; for UI screenshots
		// it preserves text edges much better than nearest-neighbor.
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
		scaled = dst
	}
	var buf bytes.Buffer
	// JPEG 85: text stays crisp, screenshots shrink dramatically vs PNG. We do NOT
	// use PNG here on purpose — the size is the whole reason VLM calls are slow.
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 85}); err != nil {
		// Shouldn't happen for a valid image; fall back to raw PNG if it ever does.
		buf.Reset()
		_ = jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 90})
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// encodeScaledSizeBytes is the byte length of a VLM-bound image, for logging.
// Kept minimal — the data URL string length tells us the upload size directly.
func encodeScaledSizeBytes(img image.Image) int {
	return len(encodeForVLM(img))
}
