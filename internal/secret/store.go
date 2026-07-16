package secret

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store is an encrypted key→value secret store. Values are Protect'd (DPAPI on
// Windows, AES-GCM fallback elsewhere) before being written to a JSON file at
// rest, so the on-disk file is ciphertext bound to the current OS user. Use
// Get/Set/Delete at the call site; Protect/Unprotect are exported for callers
// that want raw bytes.
//
// The intended integration is: secrets live here encrypted at rest, and at
// startup LoadIntoEnv() decrypts them into os.Setenv so existing tools keep
// reading via os.Getenv(passwordEnv) unchanged. Env becomes an in-memory
// decrypted view; the file is the encrypted source of truth.
type Store struct {
	path string
	mu   sync.Mutex

	// injectedKeys records the env-var names LoadIntoEnv actually set (i.e. those
	// that were NOT already present as explicit user/system env). UnloadFromEnv
	// uses this to clear only what we injected, never clobbering a user's own
	// env. This bounds the secret's lifetime in the process env: a caller that
	// Unloads on teardown avoids leaving plaintext secrets in os.Environ() for
	// every later child process (bash/MCP/LSP) to inherit. See audit finding A9.
	// NOTE: a full per-run isolation (injecting secrets only into specific child
	// cmd.Env rather than the global process env) is a larger architecture change
	// tracked separately; this Unload capability is the first defensive step.
	injectedKeys []string
}

const userDirname = "momapeer"

// defaultStoreOnce ensures Default() returns a single shared Store instance so
// that LoadIntoEnv (which records injected keys) and UnloadFromEnv (which
// clears them) operate on the same injectedKeys slice across calls.
var (
	defaultStore     *Store
	defaultStoreOnce sync.Once
)

// New returns a Store backed by path. The parent dir is created lazily on first
// Set. The file format is {"secrets": {key: base64(Protect(value))}}.
func New(path string) *Store { return &Store{path: path} }

// DefaultPath returns the canonical store location, beside config.toml and the
// credentials file: os.UserConfigDir()/momapeer/secrets.enc.json. This matches
// config.userDir()/desktopConfigDir() so a single migration sweep finds the
// legacy cowork.env and credentials files.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = home
	}
	return filepath.Join(dir, userDirname, "secrets.enc.json")
}

// Default returns a Store at DefaultPath().
// Default returns the singleton Store at DefaultPath(). The singleton matters
// because LoadIntoEnv records which env vars it injected into injectedKeys, and
// UnloadFromEnv (which may run much later at teardown) must read the same slice
// — two separate New() instances would each have their own (empty) list.
func Default() *Store {
	defaultStoreOnce.Do(func() {
		defaultStore = New(DefaultPath())
	})
	return defaultStore
}

type onDisk struct {
	Secrets map[string]string `json:"secrets"` // key -> base64(Protect(value))
}

func (s *Store) load() (onDisk, error) {
	var doc onDisk
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil // first run — no secrets yet
		}
		return doc, err
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return doc, fmt.Errorf("secret: parse %s: %w", s.path, err)
	}
	if doc.Secrets == nil {
		doc.Secrets = map[string]string{}
	}
	return doc, nil
}

// saveLocked writes doc atomically (sibling tmp + rename). Caller holds s.mu.
func (s *Store) saveLocked(doc onDisk) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Set stores value under key, encrypting it at rest. An empty value is allowed
// (it encrypts to a non-empty blob, so presence is still detectable via Get).
func (s *Store) Set(key, value string) error {
	if key == "" {
		return errors.New("secret: empty key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return err
	}
	protected, err := Protect([]byte(value))
	if err != nil {
		return err
	}
	if doc.Secrets == nil {
		doc.Secrets = map[string]string{}
	}
	doc.Secrets[key] = base64.StdEncoding.EncodeToString(protected)
	return s.saveLocked(doc)
}

// Get returns the decrypted value for key. ok is false when the key is absent
// (not an error). A decrypt failure (e.g. ciphertext from another user) returns
// ok=false and the error so the caller can prompt to re-enter the secret.
func (s *Store) Get(key string) (value string, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return "", false, err
	}
	enc, exists := doc.Secrets[key]
	if !exists {
		return "", false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", false, err
	}
	plain, err := Unprotect(raw)
	if err != nil {
		return "", false, err
	}
	return string(plain), true, nil
}

// Delete removes key. No-op when the key is absent.
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := doc.Secrets[key]; !ok {
		return nil
	}
	delete(doc.Secrets, key)
	return s.saveLocked(doc)
}

// Keys returns the names of all stored secrets (order unspecified).
func (s *Store) Keys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(doc.Secrets))
	for k := range doc.Secrets {
		keys = append(keys, k)
	}
	return keys, nil
}

// LoadIntoEnv decrypts every secret and exports it into the process environment
// via os.Setenv, skipping any env var that is already set (explicit user/system
// env always wins over the file). Returns the count of secrets loaded. Secrets
// that fail to decrypt — e.g. the file was copied from another Windows user —
// are skipped silently; the caller treats them as unset and the tool reports a
// config error, prompting re-entry.
func (s *Store) LoadIntoEnv() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return 0, err
	}
	n := 0
	for key, enc := range doc.Secrets {
		if os.Getenv(key) != "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			continue
		}
		plain, err := Unprotect(raw)
		if err != nil {
			continue
		}
		os.Setenv(key, string(plain))
		// Record that WE injected this key (it was empty before), so UnloadFromEnv
		// can later clear it without touching env vars the user set themselves.
		s.injectedKeys = append(s.injectedKeys, key)
		n++
	}
	return n, nil
}

// UnloadFromEnv clears the env vars that LoadIntoEnv injected, bounding the
// plaintext secret's lifetime in the process environment. Vars the user/system
// set explicitly are never touched (LoadIntoEnv skips them, so they're absent
// from injectedKeys). Safe to call multiple times; the recorded list is cleared
// after unloading. Returns the count of vars unset.
//
// Note: os.Unsetenv only affects the current process and children spawned
// afterwards — already-running child processes retain the env they inherited at
// spawn. For full isolation, call UnloadFromEnv before spawning untrusted
// children rather than after.
func (s *Store) UnloadFromEnv() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, key := range s.injectedKeys {
		os.Unsetenv(key)
		n++
	}
	s.injectedKeys = nil
	return n
}
