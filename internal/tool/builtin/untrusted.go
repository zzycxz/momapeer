package builtin

import (
	"fmt"
	"strings"
)

// untrustedCloseTag is the fence we wrap content in. A malicious page/document
// that embeds this exact string can prematurely close the fence and inject
// instructions after it — the classic prompt-injection escape. We neutralize
// any literal occurrence inside content before wrapping.
const untrustedCloseTag = "</untrusted_content>" //nolint:unused

// WrapUntrusted wraps externally-sourced content (web pages, browser DOM, RAG
// snippets) in an <untrusted_content> tag. The cowork system prompt instructs
// the model to treat anything inside this tag as DATA, never as instructions —
// the core defense against prompt injection from malicious web pages or
// documents that try to hijack the agent ("ignore previous instructions…").
//
// source identifies where the content came from ("browser", "web", "rag") so
// the model can weigh trust and the user can audit the provenance in tool
// output. An empty content still gets wrapped so the boundary is always
// explicit — never let untrusted text bleed into the model's context without
// a clear fence around it.
//
// SECURITY: content is sanitized so a literal </untrusted_content> inside it
// cannot close the fence early. The closing tag is split into HTML-entity-
// encoded pieces the model still reads as data but that no longer match the
// fence boundary. This is the ONLY defense against prompt injection from
// fetched content, so it must be airtight.
//
// Exported so non-builtin packages (e.g. desktop expert-team injection,
// boot-time RAG auto-search) can share the same airtight fence instead of
// hand-rolling <untrusted_content> tags that forget to sanitize.
func WrapUntrusted(source, content string) string {
	return fmt.Sprintf("<untrusted_content source=%q>\n%s\n</untrusted_content>", source, sanitizeUntrusted(content))
}

// sanitizeUntrusted neutralizes any literal occurrence of the fence tags within
// content so a malicious payload can neither:
//   - close the fence early (</untrusted_content>) and inject instructions after
//     it, nor
//   - forge a nested <untrusted_content source="system"> block inside the fence
//     to mislead the model about the trust level of subsequent content.
//
// We replace the leading "<" of both tags with "&lt;" so the sequences are no
// longer recognized as tag boundaries, while remaining readable to the model as
// data. Matching is case-insensitive so attackers can't slip through with
// </UNTRUSTED_CONTENT> or <UNTRUSTED_CONTENT ...>. See security review finding:
// the close-tag-only sanitize left open-tag forgery possible.
func sanitizeUntrusted(content string) string {
	lower := strings.ToLower(content)
	closeNeedle := "</untrusted_content"
	openNeedle := "<untrusted_content"
	if !strings.Contains(lower, closeNeedle) && !strings.Contains(lower, openNeedle) {
		return content // fast path: no fence-tag sequence present
	}
	// Scan left-to-right, replacing whichever needle appears next (open or close)
	// with its entity-encoded form. This preserves the original text around each
	// occurrence and handles interleaved open/close forgeries in one pass.
	var b strings.Builder
	for {
		ci := strings.Index(lower, closeNeedle)
		oi := strings.Index(lower, openNeedle)
		// Pick the earliest occurrence; -1 (not found) sorts after any real idx.
		var idx int
		var needle, replacement string
		switch {
		case ci < 0 && oi < 0:
			b.WriteString(content)
			return b.String()
		case ci < 0 || (oi >= 0 && oi < ci):
			idx, needle, replacement = oi, openNeedle, "&lt;untrusted_content"
		default:
			idx, needle, replacement = ci, closeNeedle, "&lt;/untrusted_content"
		}
		b.WriteString(content[:idx])
		b.WriteString(replacement)
		content = content[idx+len(needle):]
		lower = lower[idx+len(needle):]
	}
}
