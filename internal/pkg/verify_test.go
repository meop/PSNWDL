package pkg

import (
	"crypto/sha1" // #nosec G505
	"encoding/binary"
	"os"
	"testing"
)

// buildVerifyPKG creates a temp file representing a PKG with a known body
// and writes the SHA-1 of the body as the trailing 20-byte digest (followed
// by 12 zero bytes to make the full 32-byte trailer).
// If zeroDigest is true, the 20-byte stored digest is all zeros (homebrew).
func buildVerifyPKG(t *testing.T, body []byte, zeroDigest bool) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "verify-*.pkg")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := f.Write(body); err != nil {
		t.Fatal(err)
	}

	// The trailer is 32 bytes: 20-byte SHA-1 + 12 padding bytes.
	trailer := make([]byte, packageDigestSize)
	if !zeroDigest {
		sum := sha1.Sum(body) // #nosec G401
		copy(trailer[:20], sum[:])
	}
	if _, err := f.Write(trailer); err != nil {
		t.Fatal(err)
	}

	return f.Name()
}

func TestVerify_ValidDigest(t *testing.T) {
	body := []byte("hello world PKG body data that is long enough to matter")
	path := buildVerifyPKG(t, body, false)

	if err := Verify(path); err != nil {
		t.Errorf("Verify returned unexpected error: %v", err)
	}
}

func TestVerify_HomebrewZeroDigest(t *testing.T) {
	body := make([]byte, 256)
	binary.BigEndian.PutUint32(body, 0xDEADBEEF) // some non-zero content
	path := buildVerifyPKG(t, body, true)

	if err := Verify(path); err != nil {
		t.Errorf("Verify returned error for homebrew (zero digest): %v", err)
	}
}

func TestVerify_CorruptedBody(t *testing.T) {
	body := []byte("original PKG body content")
	path := buildVerifyPKG(t, body, false)

	// Corrupt the first byte of the body.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0xFF}); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	err = Verify(path)
	if err == nil {
		t.Fatal("expected mismatch error for corrupted body, got nil")
	}
}

func TestVerify_TooSmall(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tiny-*.pkg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("short")); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	err = Verify(f.Name())
	if err == nil {
		t.Fatal("expected error for too-small file, got nil")
	}
}
