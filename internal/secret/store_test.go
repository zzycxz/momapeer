package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "secrets.enc.json"))
	if err := s.Set("MAIL_PASSWORD", "abc123授权码"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := s.Get("MAIL_PASSWORD")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected secret present")
	}
	if got != "abc123授权码" {
		t.Fatalf("got %q, want %q", got, "abc123授权码")
	}
}

func TestStoreMissing(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "secrets.enc.json"))
	_, ok, err := s.Get("nope")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected missing")
	}
}

func TestStoreDelete(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "secrets.enc.json"))
	if err := s.Set("k", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Delete("k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := s.Get("k"); ok {
		t.Fatal("expected deleted")
	}
	// Deleting a missing key is a no-op.
	if err := s.Delete("absent"); err != nil {
		t.Fatalf("Delete absent: %v", err)
	}
}

func TestStoreCiphertextOnDisk(t *testing.T) {
	// The whole point: the plaintext must never appear in the file.
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc.json")
	s := New(path)
	if err := s.Set("MAIL_PASSWORD", "supersecret-value-123"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(b), "supersecret-value-123") {
		t.Fatal("plaintext leaked to disk")
	}
}

func TestStoreLoadIntoEnv(t *testing.T) {
	const key = "MOMAPEER_TEST_SECRET_XYZ_001"
	os.Unsetenv(key)
	s := New(filepath.Join(t.TempDir(), "secrets.enc.json"))
	if err := s.Set(key, "hello-env"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	n, err := s.LoadIntoEnv()
	if err != nil {
		t.Fatalf("LoadIntoEnv: %v", err)
	}
	if n < 1 {
		t.Fatal("expected at least 1 secret loaded")
	}
	if os.Getenv(key) != "hello-env" {
		t.Fatalf("env not set, got %q", os.Getenv(key))
	}
}

func TestStoreEmptyValue(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "secrets.enc.json"))
	if err := s.Set("k", ""); err != nil {
		t.Fatalf("Set empty: %v", err)
	}
	got, ok, err := s.Get("k")
	if err != nil || !ok {
		t.Fatalf("Get empty: ok=%v err=%v", ok, err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
