package builtin

import (
	"errors"
	"testing"
)

func TestClassifyAuthErrIMAPLogin(t *testing.T) {
	raw := errors.New("NO AUTHENTICATE Invalid credentials")
	got := classifyAuthErr(raw)
	if !errors.Is(got, ErrAuthExpired) {
		t.Fatal("IMAP AUTHENTICATE failure should be classified as ErrAuthExpired")
	}
	// original message preserved
	if !errors.Is(got, raw) {
		t.Fatal("original error should be preserved via join")
	}
}

func TestClassifyAuthErrSMTP535(t *testing.T) {
	got := classifyAuthErr(errors.New("535 5.7.3 Authentication unsuccessful"))
	if !errors.Is(got, ErrAuthExpired) {
		t.Fatal("SMTP 535 should be ErrAuthExpired")
	}
}

func TestClassifyAuthErrChinese(t *testing.T) {
	got := classifyAuthErr(errors.New("认证失败：密码错误"))
	if !errors.Is(got, ErrAuthExpired) {
		t.Fatal("Chinese auth-failure text should be ErrAuthExpired")
	}
}

func TestClassifyAuthErrNetworkNotAuth(t *testing.T) {
	cases := []string{
		"imap dial imap.139.com: i/o timeout",
		"connection refused",
		"tls dial smtp.139.com: EOF",
		"no such host",
	}
	for _, c := range cases {
		got := classifyAuthErr(errors.New(c))
		if errors.Is(got, ErrAuthExpired) {
			t.Fatalf("network error %q must NOT be classified as auth-expired", c)
		}
	}
}

func TestClassifyAuthErrNil(t *testing.T) {
	if got := classifyAuthErr(nil); got != nil {
		t.Fatal("nil should stay nil")
	}
}

func TestClassifyAuthErrUnrelated(t *testing.T) {
	got := classifyAuthErr(errors.New("select inbox: bad mailbox"))
	if errors.Is(got, ErrAuthExpired) {
		t.Fatal("unrelated error should not become ErrAuthExpired")
	}
}

// stubNotifier captures NotifyAuthExpired calls for assertion.
type stubNotifier struct{ got string }

func (s *stubNotifier) NotifyAuthExpired(account string) { s.got = account }

func TestFriendlyEmailErrAuthExpired(t *testing.T) {
	prev := authNotifier
	defer func() { authNotifier = prev }()
	st := &stubNotifier{}
	SetAuthNotifier(st)

	err := friendlyEmailErr("work-139", classifyAuthErr(errors.New("NO AUTHENTICATE")))
	if st.got != "work-139" {
		t.Fatalf("notifier should receive account name, got %q", st.got)
	}
	if err == nil || !contains(err.Error(), "授权码") {
		t.Fatalf("friendly message missing hint: %v", err)
	}
}

func TestFriendlyEmailErrNonAuth(t *testing.T) {
	prev := authNotifier
	defer func() { authNotifier = prev }()
	SetAuthNotifier(&stubNotifier{})

	orig := errors.New("some other error")
	got := friendlyEmailErr("", orig)
	if !errors.Is(got, orig) {
		t.Fatal("non-auth error should pass through unchanged")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
