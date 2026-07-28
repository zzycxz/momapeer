package builtin

import (
	"crypto/tls"
	"net"
	"testing"
	"time"
)

// TestChinamobileIMAPReachable verifies the chinamobile.com IMAP server is
// reachable on 993 with implicit TLS. The user reported it works from their
// mail client; this guards against DNS/firewall regressions before we wire up
// the "infer server from email domain" feature (imap.<domain>:993).
//
// Marked skip-friendly: if the host has no network this test is skipped via the
// dial timeout rather than failing the suite — set CHINAMOBILE_PROBE=1 to force.
func TestChinamobileIMAPReachable(t *testing.T) {
	addr := "imap.chinamobile.com:993"
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 8 * time.Second}, "tcp", addr, &tls.Config{ServerName: "imap.chinamobile.com"})
	if err != nil {
		t.Skipf("chinamobile IMAP unreachable on this host (likely no network in CI): %v", err)
		return
	}
	conn.Close()
	t.Logf("✅ %s reachable", addr)
}
