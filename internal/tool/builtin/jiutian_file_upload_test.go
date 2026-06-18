package builtin

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadToJiutian_NoAPIKey(t *testing.T) {
	old := os.Getenv("JIUTIAN_API_KEY")
	os.Unsetenv("JIUTIAN_API_KEY")
	defer os.Setenv("JIUTIAN_API_KEY", old)

	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0o644)
	_, err := jiutianUploadFile(context.Background(), path)
	if err == nil || err.Error() != "JIUTIAN_API_KEY not set" {
		t.Fatalf("expected JIUTIAN_API_KEY error, got: %v", err)
	}
}

func TestUploadToJiutian_Integration(t *testing.T) {
	apiKey := os.Getenv("JIUTIAN_API_KEY")
	if apiKey == "" {
		t.Skip("JIUTIAN_API_KEY not set")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(f, img)
	f.Close()

	serverPath, err := jiutianUploadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("jiutianUploadFile: %v", err)
	}
	t.Logf("Server path: %s", serverPath)
	if serverPath == "" {
		t.Fatal("empty server path")
	}
}
