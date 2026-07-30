package builtin

import (
	"encoding/base64"
	"testing"

	"github.com/emersion/go-imap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// TestDecodeRFC2047_GBK verifies that RFC 2047 encoded-words using the GBK
// family of charsets (gbk, gb2312, gb18030) — common in Chinese mail like
// 139.com — decode to readable UTF-8 rather than showing the raw
// "=?gb2312?B?...?=" blob. The standard mime.WordDecoder omits these charsets,
// which was the root cause of garbled subjects in the cowork mail dock.
func TestDecodeRFC2047_GBK(t *testing.T) {
	cases := []string{
		"关于项目验收的通知",
		"周报：本周工作总结",
		"【系统通知】您的账号有新动态",
	}
	for _, want := range cases {
		// Encode the Chinese text as GBK bytes, then base64, then wrap as an
		// RFC 2047 encoded-word — mirroring how a real Chinese mailer emits it.
		gbkStr, _, err := transform.String(simplifiedchinese.GBK.NewEncoder(), want)
		if err != nil {
			t.Fatalf("encode %q as GBK: %v", want, err)
		}
		b64 := base64.StdEncoding.EncodeToString([]byte(gbkStr))
		for _, charset := range []string{"gbk", "gb2312", "gb18030"} {
			header := "=?" + charset + "?B?" + b64 + "?="
			got := decodeRFC2047(header)
			if got != want {
				t.Errorf("decodeRFC2047(%q) [charset=%s] = %q, want %q", header, charset, got, want)
			}
		}
	}
}

// TestDecodeRFC2047_UTF8 ensures the UTF-8 fast path (handled by the stdlib
// mime.WordDecoder) still works after adding the GBK fallback.
func TestDecodeRFC2047_UTF8(t *testing.T) {
	// "Hello" in UTF-8 base64.
	b64 := base64.StdEncoding.EncodeToString([]byte("Hello"))
	header := "=?utf-8?B?" + b64 + "?="
	if got := decodeRFC2047(header); got != "Hello" {
		t.Errorf("decodeRFC2047(%q) = %q, want Hello", header, got)
	}
}

// TestDecodeRFC2047_PlainText verifies already-decoded text passes through
// unchanged (no encoded-words to process).
func TestDecodeRFC2047_PlainText(t *testing.T) {
	for _, s := range []string{"plain subject", "纯文本主题", ""} {
		if got := decodeRFC2047(s); got != s {
			t.Errorf("decodeRFC2047(%q) = %q, want unchanged", s, got)
		}
	}
}

// TestFormatAddresses_GBK verifies that imap.Address items with RFC 2047 GBK
// encoded PersonalName fields (e.g. from China Mobile 139 mailbox) are correctly
// decoded into readable Chinese names instead of raw =?gbk?b?...?= blobs.
func TestFormatAddresses_GBK(t *testing.T) {
	wantName := "中国移动"
	gbkStr, _, err := transform.String(simplifiedchinese.GBK.NewEncoder(), wantName)
	if err != nil {
		t.Fatalf("encode %q as GBK: %v", wantName, err)
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(gbkStr))
	encodedName := "=?gbk?b?" + b64 + "?="

	addrs := []*imap.Address{
		{
			PersonalName: encodedName,
			MailboxName:  "10086",
			HostName:     "139.com",
		},
	}

	got := formatAddresses(addrs)
	want := "中国移动 <10086@139.com>"
	if got != want {
		t.Fatalf("formatAddresses() = %q, want %q", got, want)
	}
}
