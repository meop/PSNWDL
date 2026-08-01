package pkg

import (
	"crypto/sha1" // #nosec G505 — SHA-1 is mandated by the PS3 PKG format spec
	"fmt"
	"io"
	"os"
)

// packageDigestSize is the size of the trailing digest region.
// PyKG uses 32 bytes for the overall "trailer" but the PackageDigest
// itself is the first 20 of those bytes (SHA-1 output).
// See verify_pkg_hash() in PyKG: body_size = file_size - 32, then reads 20 bytes.
const packageDigestSize = 32

// Verify confirms the PKG's body SHA-1 matches its trailing 20-byte digest.
// Returns nil for homebrew PKGs (zero digest) and on successful match.
// Faithfully ported from PyKG's verify_pkg_hash().
func Verify(pkgPath string) error {
	f, err := os.Open(pkgPath)
	if err != nil {
		return fmt.Errorf("open for verify: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if info.Size() < packageDigestSize {
		return fmt.Errorf("file too small (%d bytes) to contain a PackageDigest", info.Size())
	}

	bodySize := info.Size() - packageDigestSize

	// Hash the body (everything except the trailing 32 bytes).
	h := sha1.New() // #nosec G401
	if _, err := io.CopyN(h, f, bodySize); err != nil {
		return fmt.Errorf("hash body: %w", err)
	}

	// Read the 20-byte stored digest (the first 20 of the 32 trailing bytes).
	var stored [20]byte
	if _, err := io.ReadFull(f, stored[:]); err != nil {
		return fmt.Errorf("read stored digest: %w", err)
	}

	// Homebrew PKGs have a zero digest — skip the check.
	if isZero(stored[:]) {
		return nil
	}

	computed := h.Sum(nil)
	for i, b := range computed {
		if b != stored[i] {
			return fmt.Errorf("SHA-1 mismatch: stored %x, computed %x", stored[:], computed)
		}
	}
	return nil
}

// isZero returns true when all bytes in b are 0x00.
func isZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
