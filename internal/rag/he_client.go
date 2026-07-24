package rag

// he_client.go is a Go HTTP client for the Hyper-Extract Python server.
// It communicates with hyper_extract_server.py over localhost HTTP.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HETemplate represents a Hyper-Extract template.
type HETemplate struct {
	Name           string        `json:"name"`
	Category       string        `json:"category"`
	File           string        `json:"file"`
	Available      bool          `json:"available"`
	Description    string        `json:"description"`
	TemplateType   string        `json:"templateType"`
	EntityFields   []HEFieldMeta `json:"entityFields"`
	RelationFields []HEFieldMeta `json:"relationFields"`
}

// HEFieldMeta is a field description from a template YAML.
type HEFieldMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// HEResult is the extraction result from Hyper-Extract.
type HEResult struct {
	Entities  []HEEntity   `json:"entities"`
	Relations []HERelation `json:"relations"`
}

// HEEntity is an extracted entity from Hyper-Extract.
type HEEntity struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// HERelation is an extracted relation from Hyper-Extract.
type HERelation struct {
	Source      string  `json:"source"`
	Target      string  `json:"target"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Strength    float64 `json:"strength,omitempty"`
}

// HEClient is the HTTP client for the Hyper-Extract server.
type HEClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHEClient creates a new Hyper-Extract client.
func NewHEClient(port int) *HEClient {
	return &HEClient{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // extraction can be slow
		},
	}
}

// Health checks if the server is running and Hyper-Extract is available.
func (c *HEClient) Health(ctx context.Context) (bool, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return false, false, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()
	var result struct {
		Status      string `json:"status"`
		HEAvailable bool   `json:"he_available"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, false, err
	}
	return result.Status == "ok", result.HEAvailable, nil
}

// ListTemplates returns available extraction templates.
func (c *HEClient) ListTemplates(ctx context.Context) ([]HETemplate, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/templates", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Templates []HETemplate `json:"templates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Templates, nil
}

// Extract extracts entities and relations from text.
func (c *HEClient) Extract(ctx context.Context, text string, template string, lang string) (*HEResult, error) {
	body, _ := json.Marshal(map[string]string{
		"text":     text,
		"template": template,
		"lang":     lang,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/extract", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("extract failed (%d): %s", resp.StatusCode, string(errBody))
	}
	var result HEResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ExportObsidian exports a KA to an Obsidian vault.
func (c *HEClient) ExportObsidian(ctx context.Context, kaPath string, outputDir string) error {
	body, _ := json.Marshal(map[string]string{
		"ka_path":    kaPath,
		"output_dir": outputDir,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/export_obsidian", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("export failed (%d): %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// Embed generates embedding vectors for a list of texts via the HE server.
func (c *HEClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"texts": texts})
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed failed (%d): %s", resp.StatusCode, string(errBody))
	}
	var result struct {
		Vectors [][]float32 `json:"vectors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Vectors, nil
}

// HESummary is the response from the /summarize endpoint.
type HESummary struct {
	Summary string   `json:"summary"`
	Themes  []string `json:"themes"`
	Error   string   `json:"error,omitempty"`
}

// Summarize generates a knowledge summary from entities and relations.
func (c *HEClient) Summarize(ctx context.Context, entities []HEEntity, relations []HERelation, lang string) (*HESummary, error) {
	entList := make([]map[string]string, 0, len(entities))
	for _, e := range entities {
		entList = append(entList, map[string]string{"name": e.Name, "type": e.Type, "description": e.Description})
	}
	relList := make([]map[string]string, 0, len(relations))
	for _, r := range relations {
		relList = append(relList, map[string]string{"source": r.Source, "target": r.Target, "type": r.Type, "description": r.Description})
	}
	body, _ := json.Marshal(map[string]any{"entities": entList, "relations": relList, "lang": lang})
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/summarize", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("summarize failed (%d): %s", resp.StatusCode, string(errBody))
	}
	var result HESummary
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, fmt.Errorf("summarize: %s", result.Error)
	}
	return &result, nil
}
