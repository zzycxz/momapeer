package builtin

import (
	"fmt"
	"strings"
)

// untrustedCloseTag is the fence we wrap content in. A malicious page/document
// that embeds this exact string can prematurely close the fence and inject
// instructions after it — the classic prompt-injection escape. We neutralize
// any literal occurrence inside content before wrapping.
const untrustedCloseTag = "</untrusted_content>"

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

// sanitizeUntrusted neutralizes any literal occurrence of the closing fence
// tag within content. We replace the "</" of a closing tag with "&lt;/" so the
// sequence can no longer terminate our fence, while remaining readable to the
// model as data. Both an exact-match and a case-insensitive match are covered
// so attackers can't slip through with </UNTRUSTED_CONTENT>.
func sanitizeUntrusted(content string) string {
	if !strings.Contains(strings.ToLower(content), "</untrusted_content") {
		return content // fast path: no fence-closing sequence present
	}
	// Replace every "</untrusted_content" (case-insensitive) with the entity-
	// encoded form. We split on the lowercased needle and re-emit the encoding,
	// preserving the original text around it.
	var b strings.Builder
	needle := "</untrusted_content"
	lower := strings.ToLower(content)
	for {
		idx := strings.Index(lower, needle)
		if idx < 0 {
			b.WriteString(content)
			break
		}
		b.WriteString(content[:idx])
		b.WriteString("&lt;/untrusted_content")
		content = content[idx+len(needle):]
		lower = lower[idx+len(needle):]
	}
	return b.String()
}
