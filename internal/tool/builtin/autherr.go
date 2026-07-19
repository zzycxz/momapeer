package builtin

import (
	"errors"
	"fmt"
	"strings"
)

// ErrAuthExpired signals that an email operation failed because the mailbox
// credentials were rejected — most commonly a 139 authorization code past its
// 90-day life. Detect with errors.Is so callers can prompt "re-enter your auth
// code" instead of showing an opaque network error.
var ErrAuthExpired = errors.New("email credentials rejected or expired")

// classifyAuthErr returns err joined with ErrAuthExpired when it looks like an
// auth failure, or err unchanged for network/dial/timeout failures. The match
// is deliberately broad on auth keywords because Chinese providers may return
// localized text ("认证失败" / "密码错误"), and a flaky connection must NOT be
// misreported as "code expired".
func classifyAuthErr(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	// Network / dial / timeout failures are not auth failures — leave as-is.
	for _, kw := range []string{"timeout", "deadline", "connection refused", "no such host", "reset", "unreachable", "eof", "dial"} {
		if strings.Contains(msg, kw) {
			return err
		}
	}
	// Auth-failure signatures: IMAP "NO ...AUTHENTICATE/LOGIN", SMTP "535", plus
	// generic auth/credential keywords (English + Chinese).
	for _, kw := range []string{"authenticate", "login", "535", "auth", "credential", "认证", "密码", "授权", "invalid", "rejected", "denied"} {
		if strings.Contains(msg, kw) {
			return errors.Join(err, ErrAuthExpired)
		}
	}
	return err
}

// AuthNotifier is an optional sink for "email credentials expired" notices. The
// desktop app injects one that fires an in-app toast, so the user learns their
// 139 authorization code needs renewing even mid-task. Nil = silent (the error
// still surfaces in the tool result and conversation).
type AuthNotifier interface {
	NotifyAuthExpired(account string)
}

var authNotifier AuthNotifier

// SetAuthNotifier injects the credentials-expired notifier (desktop toast).
func SetAuthNotifier(n AuthNotifier) { authNotifier = n }

// friendlyEmailErr fires an auth-expired toast (when err is a credentials
// rejection) and returns a user-facing error with a concrete remediation hint.
// Call from each email tool's IMAP/SMTP operation-error path.
//
// The returned message is deliberately SHORT and human — it surfaces directly
// in the chat and toast. We intentionally do NOT wrap the raw IMAP/SMTP error
// (%w) here because that exposes opaque provider internals ("LOGIN errno:1424,
// account not exist (md return:2100001)") which look alarming and mean nothing
// to the user. The full error is still logged server-side for debugging.
func friendlyEmailErr(account string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrAuthExpired) {
		if authNotifier != nil {
			authNotifier.NotifyAuthExpired(account)
		}
		name := strings.TrimSpace(account)
		if name == "" {
			name = "默认账号"
		}
		return fmt.Errorf("邮箱未登录或授权已失效（账号 %q），请在「办公设置 → 邮件」里重新填写授权码", name)
	}
	return err
}
