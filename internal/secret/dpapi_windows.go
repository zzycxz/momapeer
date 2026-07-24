//go:build windows

package secret

import (
	"errors"
	"syscall"
	"unsafe"
)

// This file implements Protect/Unprotect via the Windows Data Protection API
// (DPAPI). DPAPI encrypts a blob with a key derived from the current Windows
// user's logon credentials, so the ciphertext can only be decrypted by the same
// user on the same machine. We call crypt32.dll!CryptProtectData/CryptUnprotectData
// directly via syscall — the same zero-CGO approach the rest of the desktop
// build uses (see desktop/screenshot_hotkey_windows.go).
//
// Implementation note: the input DATA_BLOB is heap-allocated (newBlob) and the
// optional entropy is passed as a pointer to an empty blob rather than NULL.
// Passing NULL entropy or a stack-allocated blob triggers ERROR_INVALID_PARAMETER
// (87) on stock Windows installs; mirroring the well-tested alexmullins/dpapi
// shape avoids that.

var (
	crypt32            = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtect   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect = crypt32.NewProc("CryptUnprotectData")

	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLocalFree = kernel32.NewProc("LocalFree")
)

// dataBlob mirrors the Windows DATA_BLOB structure: a length + pointer to the
// raw bytes. Heap-allocate instances passed into DPAPI (via newBlob).
type dataBlob struct {
	cbData uint32
	pbData *byte
}

// newBlob returns a heap-allocated DATA_BLOB wrapping d. An empty d yields a
// zero-length blob (size=0, data=nil) whose address is still a valid (non-NULL)
// argument for the optional-entropy parameter.
func newBlob(d []byte) *dataBlob {
	if len(d) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(d)), pbData: &d[0]}
}

// toBytes copies the blob's contents into a fresh slice. Used to read DPAPI's
// output buffer before we LocalFree it.
func (b *dataBlob) toBytes() []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	out := make([]byte, b.cbData)
	copy(out, (*[1 << 28]byte)(unsafe.Pointer(b.pbData))[:b.cbData:b.cbData])
	return out
}

// Protect encrypts plaintext using DPAPI at the current-user scope. The returned
// ciphertext is only decryptable by the same Windows user. An empty input gives
// an empty output (no encryption call).
func Protect(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	var out dataBlob
	in := newBlob(plaintext) // heap-allocated; pinned for the call
	entropy := &dataBlob{}   // non-NULL empty blob (NULL triggers err 87)
	r, _, lastErr := procCryptProtect.Call(
		uintptr(unsafe.Pointer(in)),
		0, // description (NULL is allowed)
		uintptr(unsafe.Pointer(entropy)),
		0, // reserved
		0, // prompt struct (none)
		0, // flags: 0 = current-user scope
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		if lastErr == nil {
			lastErr = errors.New("dpapi: CryptProtectData returned 0")
		}
		return nil, lastErr
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.toBytes(), nil
}

// Unprotect decrypts a DPAPI blob produced by Protect. Fails (returns a non-nil
// error) if the ciphertext was protected by a different user or on a different
// machine — callers should treat that as "secret must be re-entered".
func Unprotect(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, nil
	}
	var out dataBlob
	in := newBlob(ciphertext)
	entropy := &dataBlob{}
	r, _, lastErr := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(in)),
		0,
		uintptr(unsafe.Pointer(entropy)),
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		if lastErr == nil {
			lastErr = errors.New("dpapi: CryptUnprotectData returned 0")
		}
		return nil, lastErr
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.toBytes(), nil
}
