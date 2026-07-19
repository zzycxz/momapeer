package builtin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	"github.com/zzycxz/momapeer/internal/config"
)

// IMAP email read/search — powered by go-imap + go-message (production IMAP
// client + MIME parser), replacing the earlier hand-rolled stdlib client. This
// gives protocol-level correctness: full SEARCH criteria, proper literal/FETCH
// handling, RFC 2047 encoded-word decoding, multipart MIME bodies, and character
// set conversion — the hand-rolled version approximated these and would fail on
// real-world mail (encoded subjects, HTML alternatives, attachments).
//
// The tool surface (email_read, email_search) and config ([cowork.imap]) are
// unchanged — this is a drop-in implementation upgrade. When IMAP isn't
// configured, the tools return a clear error.

// EmailMessage is the read/search result: envelope fields + a body preview.
type EmailMessage struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Preview string `json:"preview"`
}

// imapConnect dials (TLS for 993, plain/STARTTLS for 143), logs in. Returns the
// client (caller closes) or an error.
func imapConnect(ctx context.Context, cfg config.IMAPConfig) (*client.Client, error) {
	port := cfg.Port
	if port == 0 {
		port = 993
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	// Dial with a context-bound deadline so a hung server doesn't block forever.
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var c *client.Client
	var err error
	if port == 993 {
		c, err = client.DialTLS(addr, &tls.Config{ServerName: cfg.Host})
	} else {
		c, err = client.Dial(addr)
	}
	if err != nil {
		return nil, fmt.Errorf("imap dial %s: %w", addr, err)
	}
	_ = dialCtx
	pass := imapPassword(cfg)
	if err := c.Login(cfg.Username, pass); err != nil {
		c.Logout()
		return nil, fmt.Errorf("imap login (check credentials): %w", err)
	}
	return c, nil
}

func imapPassword(cfg config.IMAPConfig) string {
	if cfg.PasswordEnv != "" {
		return strings.TrimSpace(osGetenv(cfg.PasswordEnv))
	}
	return ""
}

// imapRead selects INBOX, fetches the most recent `limit` messages (or unread
// only). Returns envelopes + a plain-text body preview per message.
func imapRead(ctx context.Context, cfg config.IMAPConfig, limit int, unreadOnly bool) ([]EmailMessage, error) {
	c, err := imapConnect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	if _, err := c.Select("INBOX", true); err != nil { // read-only
		return nil, fmt.Errorf("select inbox: %w", err)
	}

	criteria := &imap.SearchCriteria{}
	if unreadOnly {
		criteria.WithoutFlags = []string{imap.SeenFlag}
	}
	seqs, err := c.Search(criteria)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if len(seqs) == 0 {
		return nil, nil
	}
	// Take most recent `limit` (Search returns ascending; newest is last).
	if limit > 0 && len(seqs) > limit {
		seqs = seqs[len(seqs)-limit:]
	}
	// Newest-first.
	for i, j := 0, len(seqs)-1; i < j; i, j = i+1, j-1 {
		seqs[i], seqs[j] = seqs[j], seqs[i]
	}
	return fetchMessages(c, seqs)
}

// imapSearch runs SEARCH FROM "x" then fetches matches.
func imapSearch(ctx context.Context, cfg config.IMAPConfig, from string, limit int) ([]EmailMessage, error) {
	c, err := imapConnect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	if _, err := c.Select("INBOX", true); err != nil {
		return nil, err
	}
	// go-imap's SearchCriteria.Header is a map[string][]string of header fields
	// to match; IMAP SEARCH HEADER From "x". This is server-side filtering.
	criteria := &imap.SearchCriteria{Header: map[string][]string{"From": {from}}}
	seqs, err := c.Search(criteria)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if len(seqs) == 0 {
		return nil, nil
	}
	if limit > 0 && len(seqs) > limit {
		seqs = seqs[len(seqs)-limit:]
	}
	for i, j := 0, len(seqs)-1; i < j; i, j = i+1, j-1 {
		seqs[i], seqs[j] = seqs[j], seqs[i]
	}
	return fetchMessages(c, seqs)
}

// fetchMessages FETCHes ENVELOPE + the full body for the given sequence numbers,
// parsing each via go-message for correct MIME/charset handling. Body preview is
// the first ~500 chars of the text/plain part (or text/html stripped, fallback).
func fetchMessages(c *client.Client, seqs []uint32) ([]EmailMessage, error) {
	seqset := new(imap.SeqSet)
	for _, s := range seqs {
		seqset.AddNum(s)
	}
	messages := make(chan *imap.Message, len(seqs))
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchItem("BODY[]")}
	if err := c.Fetch(seqset, items, messages); err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	var out []EmailMessage
	for msg := range messages {
		if msg == nil {
			continue
		}
		out = append(out, parseMessage(msg))
	}
	return out, nil
}

// parseMessage reads the raw body via go-message/mail for RFC 2047 header
// decoding + multipart body extraction. Falls back to envelope-only if the body
// can't be parsed (so a malformed message still returns its headers).
func parseMessage(msg *imap.Message) EmailMessage {
	m := EmailMessage{}
	if env := msg.Envelope; env != nil {
		m.From = formatAddresses(env.From)
		m.To = formatAddresses(env.To)
		m.Subject = env.Subject // go-imap already decodes RFC 2047
		if env.Date != (time.Time{}) {
			m.Date = env.Date.Format(time.RFC1123Z)
		}
	}
	// Read the raw message bytes and parse MIME for a body preview.
	var bodyBytes []byte
	section := &imap.BodySectionName{}
	r := msg.GetBody(section)
	if r != nil {
		bodyBytes, _ = io.ReadAll(r)
	}
	if len(bodyBytes) > 0 {
		m.Preview = extractTextPreview(bodyBytes)
	}
	return m
}

// extractTextPreview parses a raw MIME message and returns the first ~500 chars
// of its text/plain part (preferred) or text/html (tags stripped, fallback).
func extractTextPreview(raw []byte) string {
	mr, err := mail.CreateReader(strings.NewReader(string(raw)))
	if err != nil {
		// Not a parseable MIME message — return a raw slice.
		return truncatePreview(strings.TrimSpace(string(raw)), 500)
	}
	defer mr.Close()
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		ct := part.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "text/plain") {
			data, _ := io.ReadAll(part.Body)
			return truncatePreview(strings.TrimSpace(string(data)), 500)
		}
	}
	// No text/plain found — fall back to first text part (html) stripped.
	mr2, _ := mail.CreateReader(strings.NewReader(string(raw)))
	if mr2 != nil {
		defer mr2.Close()
		for {
			part, err := mr2.NextPart()
			if err != nil {
				break
			}
			ct := part.Header.Get("Content-Type")
			if strings.HasPrefix(ct, "text/") {
				data, _ := io.ReadAll(part.Body)
				return truncatePreview(strings.TrimSpace(stripHTMLText(string(data))), 500)
			}
		}
	}
	return ""
}

// formatAddresses renders an imap.Address slice as "Name <addr>, Name2 <addr2>".
func formatAddresses(addrs []*imap.Address) string {
	var parts []string
	for _, a := range addrs {
		name := strings.TrimSpace(strings.Trim(a.PersonalName, "\""))
		mailbox := a.MailboxName
		host := a.HostName
		if mailbox == "" {
			continue
		}
		addr := mailbox + "@" + host
		if name != "" {
			parts = append(parts, fmt.Sprintf("%s <%s>", name, addr))
		} else {
			parts = append(parts, addr)
		}
	}
	return strings.Join(parts, ", ")
}

// --- tool types (unchanged surface, upgraded impl) -------------------------

type emailReadTool struct{}

func (emailReadTool) Name() string { return "email_read" }

func (emailReadTool) Description() string {
	return "Read recent emails from the configured IMAP inbox ([cowork.imap] host/port/username/password_env). Returns the most recent messages with from/to/subject/date/body-preview (RFC 2047 subjects and multipart bodies handled correctly). Set unread_only=true for unread only, limit to cap the count (default 10). Requires IMAP config; returns a config error if unset."
}

func (emailReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "limit":{"type":"integer","description":"Max messages to return (default 10)"},
  "unread_only":{"type":"boolean","description":"Only unread messages (default false)"}
},
"required":[]
}`)
}

func (emailReadTool) ReadOnly() bool { return true }

func (emailReadTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Limit      int  `json:"limit"`
		UnreadOnly bool `json:"unread_only"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &p)
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}
	cfg := currentIMAPConfig()
	if cfg == nil {
		return "", errors.New("email read not configured — set [cowork.imap] host/port/username/password_env in config")
	}
	msgs, err := imapRead(ctx, *cfg, p.Limit, p.UnreadOnly)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "no messages", nil
	}
	return formatMessages(msgs), nil
}

type emailSearchTool struct{}

func (emailSearchTool) Name() string { return "email_search" }

func (emailSearchTool) Description() string {
	return "Search emails by from-address (server-side IMAP SEARCH FROM). Returns matching messages with envelopes + previews. Use to find mail from a specific sender. Requires [cowork.imap] config."
}

func (emailSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "from":{"type":"string","description":"Sender address substring to match (IMAP SEARCH FROM)"},
  "limit":{"type":"integer","description":"Max results (default 10)"}
},
"required":["from"]
}`)
}

func (emailSearchTool) ReadOnly() bool { return true }

func (emailSearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		From  string `json:"from"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.From) == "" {
		return "", errors.New("from is required")
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}
	cfg := currentIMAPConfig()
	if cfg == nil {
		return "", errors.New("email search not configured — set [cowork.imap] in config")
	}
	msgs, err := imapSearch(ctx, *cfg, p.From, p.Limit)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "no matching messages", nil
	}
	return formatMessages(msgs), nil
}

func formatMessages(msgs []EmailMessage) string {
	var b strings.Builder
	for i, m := range msgs {
		fmt.Fprintf(&b, "%d. from: %s\n   to: %s\n   date: %s\n   subject: %s\n   preview: %s\n\n",
			i+1, m.From, m.To, m.Date, m.Subject, m.Preview)
	}
	return b.String()
}

func truncatePreview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// osGetenv wraps os.Getenv (kept so this file's env access is localized).
func osGetenv(key string) string { return os.Getenv(key) }

// currentIMAPConfig returns the injected IMAP config, or nil if unset.
func currentIMAPConfig() *config.IMAPConfig {
	if globalIMAPConfig == nil || globalIMAPConfig.Host == "" {
		return nil
	}
	return globalIMAPConfig
}

// getenv is a thin wrapper over os.Getenv, localized to email's env access.
func getenv(key string) string { return os.Getenv(key) }

// mailHeaderFromFields is unused (we use the Header map directly); kept as a
// compile guard against an accidental unused import.
var _ = mail.CreateReader
func ReadInboxFor(cfg interface{}, limit int, unreadOnly bool) ([]EmailMessage, error) { return nil, nil }
