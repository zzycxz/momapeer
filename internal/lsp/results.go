package lsp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseLocations decodes the three shapes textDocument/definition and
// /references may return: Location, Location[], or LocationLink[].
func parseLocations(raw json.RawMessage) []Location {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil
	}
	if s[0] == '[' {
		var arr []Location
		if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 && arr[0].URI != "" {
			return arr
		}
		var links []struct {
			TargetURI   string `json:"targetUri"`
			TargetRange Range  `json:"targetRange"`
		}
		if json.Unmarshal(raw, &links) == nil {
			out := make([]Location, 0, len(links))
			for _, l := range links {
				if l.TargetURI != "" {
					out = append(out, Location{URI: l.TargetURI, Range: l.TargetRange})
				}
			}
			return out
		}
		return nil
	}
	var one Location
	if json.Unmarshal(raw, &one) == nil && one.URI != "" {
		return []Location{one}
	}
	return nil
}

// parseHover decodes a Hover.contents, which may be a MarkupContent, a single
// MarkedString, or an array of them.
func parseHover(raw json.RawMessage) string {
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	if json.Unmarshal(raw, &h) != nil || len(h.Contents) == 0 {
		return ""
	}
	return strings.TrimSpace(markedToText(h.Contents))
}

func markedToText(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	switch s[0] {
	case '"':
		var str string
		_ = json.Unmarshal(raw, &str)
		return str
	case '{':
		var mc struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}
		if json.Unmarshal(raw, &mc) == nil && mc.Value != "" {
			return mc.Value
		}
		var ms struct {
			Language string `json:"language"`
			Value    string `json:"value"`
		}
		_ = json.Unmarshal(raw, &ms)
		return ms.Value
	case '[':
		var parts []json.RawMessage
		_ = json.Unmarshal(raw, &parts)
		var out []string
		for _, p := range parts {
			if t := markedToText(p); t != "" {
				out = append(out, t)
			}
		}
		return strings.Join(out, "\n")
	}
	return ""
}

// symbolKindName maps LSP SymbolKind integers to short human labels. Mirrors the
// LSP 3.17 enum; unknown kinds fall back to a generic "symbol".
var symbolKindName = map[int]string{
	1: "file", 2: "module", 3: "namespace", 4: "package", 5: "class",
	6: "method", 7: "property", 8: "field", 9: "constructor", 10: "enum",
	11: "interface", 12: "function", 13: "variable", 14: "constant",
	15: "string", 16: "number", 17: "boolean", 18: "array", 19: "object",
	20: "key", 21: "null", 22: "enum-member", 23: "struct", 24: "event",
	25: "operator", 26: "type-parameter",
}

// formatSymbolInformation decodes the workspace/symbol result and renders each
// match as "name (kind) — container  file:line" (container and file:line
// omitted when absent). Returns "" when the response carries no symbols so the
// caller can skip the server's result. m is accepted to match the call site and
// reserve a hook for workspace-root-relative paths; it is not currently used.
func formatSymbolInformation(raw json.RawMessage, m *Manager) string {
	// workspace/symbol returns SymbolInformation[] (or DocumentSymbol[], but
	// workspace/symbol is always the flat form).
	var syms []struct {
		Name          string   `json:"name"`
		Kind          int      `json:"kind"`
		ContainerName string   `json:"containerName"`
		Location      Location `json:"location"`
	}
	if err := json.Unmarshal(raw, &syms); err != nil || len(syms) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range syms {
		kind := symbolKindName[s.Kind]
		if kind == "" {
			kind = "symbol"
		}
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		fmt.Fprintf(&b, "%s (%s)", name, kind)
		if c := strings.TrimSpace(s.ContainerName); c != "" {
			fmt.Fprintf(&b, " — %s", c)
		}
		if s.Location.URI != "" {
			path := uriToPath(s.Location.URI)
			fmt.Fprintf(&b, "  %s:%d", path, s.Location.Range.Start.Line+1)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

var severityName = map[int]string{1: "error", 2: "warning", 3: "info", 4: "hint"}

func formatDiagnostics(rel string, diags []Diagnostic) string {
	if len(diags) == 0 {
		return "no diagnostics for " + rel
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d diagnostic(s) in %s:\n", len(diags), rel)
	for _, d := range diags {
		sev := severityName[d.Severity]
		if sev == "" {
			sev = "error"
		}
		src := ""
		if d.Source != "" {
			src = " [" + d.Source + "]"
		}
		fmt.Fprintf(&b, "%d:%d %s%s %s\n",
			d.Range.Start.Line+1, d.Range.Start.Character+1, sev, src,
			strings.TrimSpace(d.Message))
	}
	return strings.TrimRight(b.String(), "\n")
}
