//go:build !windows

package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
)

// Non-Windows fallback for Protect/Unprotect. MoMAPeer targets Windows for the
// email/desktop features (Win32 automation), so this path exists only so the
// package compiles and runs its tests on macOS/Linux/CI. It derives a static
// AES-256 key from machine identity (hostname + home dir) — far weaker than
// DPAPI's user-bound key, but acceptable as a stand-in off Windows.

func machineKey() []byte {
	host, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	h := sha256.Sum256([]byte("momapeer-secret-v1:" + host + ":" + home))
	return h[:]
}

// Protect encrypts plaintext with AES-GCM under a machine-derived key.
func Protect(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	block, err := aes.NewCipher(machineKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// nonce is prepended; ciphertext = nonce || gcm-tag'd data.
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Unprotect decrypts an AES-GCM blob produced by Protect.
func Unprotect(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, nil
	}
	block, err := aes.NewCipher(machineKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("secret: ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}
