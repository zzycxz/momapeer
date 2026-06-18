package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zzycxz/momapeer/internal/jiutian"
)

// jiutianAPICall delegates to the shared Jiutian API helper.
func jiutianAPICall(ctx context.Context, method, path string, payload any, out any) error {
	return jiutian.APICall(ctx, method, path, payload, out)
}

// jiutianUploadFile uploads a local file to Jiutian's file storage and returns
// the server-side file path. Used internally by video_understand.
func jiutianUploadFile(ctx context.Context, filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("file is empty")
	}
	if len(data) > 100*1024*1024 {
		return "", fmt.Errorf("file too large (%d MB, max 100 MB)", len(data)/(1024*1024))
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("write file data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close writer: %w", err)
	}

	apiKey := os.Getenv("JIUTIAN_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("JIUTIAN_API_KEY not set")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://jiutian.10086.cn/largemodel/moma/api/v1/fs/uploadFile", &body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := jiutian.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("jiutian upload: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("jiutian upload HTTP %d: %s", resp.StatusCode, jiutian.Truncate(string(respBody), 300))
	}

	var result struct {
		Code     int    `json:"code"`
		Message  string `json:"message"`
		FilePath string `json:"filePath"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Code != 200 {
		return "", fmt.Errorf("jiutian upload code=%d: %s", result.Code, result.Message)
	}
	if result.FilePath == "" {
		return "", fmt.Errorf("upload succeeded but no file path returned")
	}
	return result.FilePath, nil
}

// jiutianDownloadFile fetches a Jiutian file URL (e.g. the /fs/getFile link
// returned by image generation) using the API-key auth header the endpoint
// requires — a bare link answers 401 because the key can't travel in the URL.
// Returns the raw bytes and a sniffed MIME. Capped at 10 MB like image attachments.
func jiutianDownloadFile(ctx context.Context, url string) ([]byte, string, error) {
	apiKey := os.Getenv("JIUTIAN_API_KEY")
	if apiKey == "" {
		return nil, "", fmt.Errorf("JIUTIAN_API_KEY not set")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := jiutian.Client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("jiutian download: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024+1))
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("jiutian download HTTP %d: %s", resp.StatusCode, jiutian.Truncate(string(raw), 300))
	}
	if len(raw) == 0 || len(raw) > 10*1024*1024 {
		return nil, "", fmt.Errorf("downloaded image must be between 1 byte and 10 MB")
	}
	mime := http.DetectContentType(raw)
	if !strings.HasPrefix(mime, "image/") {
		return nil, "", fmt.Errorf("downloaded data is not an image (mime=%s)", mime)
	}
	return raw, mime, nil
}
