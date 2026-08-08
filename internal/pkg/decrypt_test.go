package pkg

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/binary"
	"os"
	"testing"
)

// TestIvPlusOffset_NoCarry checks plain 128-bit addition when the low 64
// bits don't overflow.
func TestIvPlusOffset_NoCarry(t *testing.T) {
	var iv [16]byte
	binary.BigEndian.PutUint64(iv[0:8], 0x0102030405060708)
	binary.BigEndian.PutUint64(iv[8:16], 100)

	got := ivPlusOffset(iv, 5)

	wantHi := uint64(0x0102030405060708)
	wantLo := uint64(105)
	if gotHi := binary.BigEndian.Uint64(got[0:8]); gotHi != wantHi {
		t.Errorf("hi = %#x, want %#x", gotHi, wantHi)
	}
	if gotLo := binary.BigEndian.Uint64(got[8:16]); gotLo != wantLo {
		t.Errorf("lo = %#x, want %#x", gotLo, wantLo)
	}
}

// TestIvPlusOffset_Carry checks that an overflow of the low 64 bits carries
// into the high 64 bits, matching PyKG's `(iv_int + offset) & ((1<<128)-1)`
// 128-bit semantics. This is the exact bit of arithmetic a retail PS3 PKG's
// AES-CTR counter relies on once decryption walks far enough into a large
// (multi-GB) package for the low half of the IV to wrap.
func TestIvPlusOffset_Carry(t *testing.T) {
	var iv [16]byte
	binary.BigEndian.PutUint64(iv[0:8], 7)
	binary.BigEndian.PutUint64(iv[8:16], 0xFFFFFFFFFFFFFFF0)

	got := ivPlusOffset(iv, 16) // 0xFFFFFFFFFFFFFFF0 + 16 wraps to 0, carry 1

	wantHi := uint64(8)
	wantLo := uint64(0)
	if gotHi := binary.BigEndian.Uint64(got[0:8]); gotHi != wantHi {
		t.Errorf("hi = %#x, want %#x (carry not applied)", gotHi, wantHi)
	}
	if gotLo := binary.BigEndian.Uint64(got[8:16]); gotLo != wantLo {
		t.Errorf("lo = %#x, want %#x", gotLo, wantLo)
	}
}

// TestDebugKeystreamBlock_KnownVector independently reconstructs the 64-byte
// SHA-1 input buffer PyKG's get_debug_keystream_block() hashes, rather than
// calling debugKeystreamBlock's own buffer-building code, so this actually
// catches a layout mistake instead of just re-asserting it.
func TestDebugKeystreamBlock_KnownVector(t *testing.T) {
	var qaDigest [16]byte
	for i := range qaDigest {
		qaDigest[i] = byte(i + 1) // 0x01..0x10
	}
	const blockIndex = 42

	var buf [64]byte
	copy(buf[0:8], qaDigest[0:8])
	copy(buf[8:16], qaDigest[0:8])
	copy(buf[16:24], qaDigest[8:16])
	copy(buf[24:32], qaDigest[8:16])
	// [32:56] left zero
	binary.BigEndian.PutUint64(buf[56:64], blockIndex)
	wantSum := sha1.Sum(buf[:])

	got := debugKeystreamBlock(qaDigest, blockIndex)
	if got != [16]byte(wantSum[:16]) {
		t.Errorf("debugKeystreamBlock = %x, want %x", got, wantSum[:16])
	}
}

// writeFakePKGFile creates a temp file whose first `dataOffset` bytes are
// irrelevant header padding, then returns it positioned for decryptRegion to
// read the encrypted region that follows.
func writeFakePKGFile(t *testing.T, dataOffset uint64, encryptedBody []byte) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "fake-*.pkg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })

	if _, err := f.Write(make([]byte, dataOffset)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(encryptedBody); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestDecryptRegion_RetailRoundTrip encrypts known plaintext with the exact
// retail AES-CTR construction (key + counter = iv) production code uses, then
// asks decryptRegion to recover arbitrary, block-unaligned sub-ranges of it.
// This is what actually exercises the block-alignment/prefix-trim math in
// decryptRegion, which nothing else in the test suite touches.
func TestDecryptRegion_RetailRoundTrip(t *testing.T) {
	plaintext := []byte("The quick brown fox jumps over the lazy dog!!!") // 47 bytes, spans 3 blocks

	var iv [16]byte
	binary.BigEndian.PutUint64(iv[8:16], 9000) // arbitrary nonzero low half

	block, err := aes.NewCipher(ps3NpdrmKey)
	if err != nil {
		t.Fatal(err)
	}
	// decryptRegion always reads whole 16-byte blocks, even for a request
	// that ends mid-block (a real PKG's encrypted stream simply continues
	// past the requested range) — pad the fixture the same way so the reader
	// doesn't hit EOF before the last block is complete.
	paddedLen := (len(plaintext) + 15) &^ 15
	padded := make([]byte, paddedLen)
	copy(padded, plaintext)
	encrypted := make([]byte, paddedLen)
	cipher.NewCTR(block, iv[:]).XORKeyStream(encrypted, padded)

	const dataOffset = 0x100
	hdr := &header{dataOffset: dataOffset, iv: iv, releaseType: pkgReleaseTypeRetail}
	f := writeFakePKGFile(t, dataOffset, encrypted)

	cases := []struct {
		name        string
		start, size int
	}{
		{"full range", 0, len(plaintext)},
		{"unaligned start mid first block", 5, 10},
		{"spans block boundary", 12, 20},
		{"single byte", 20, 1},
		{"final partial block", 32, len(plaintext) - 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decryptRegion(f, hdr, uint64(tc.start), uint32(tc.size))
			if err != nil {
				t.Fatalf("decryptRegion: %v", err)
			}
			want := plaintext[tc.start : tc.start+tc.size]
			if string(got) != string(want) {
				t.Errorf("decryptRegion(%d, %d) = %q, want %q", tc.start, tc.size, got, want)
			}
		})
	}
}

// TestDecryptRegion_DebugRoundTrip mirrors the retail test but for the
// SHA-1-keystream debug-PKG path.
func TestDecryptRegion_DebugRoundTrip(t *testing.T) {
	plaintext := []byte("Debug PKG payload spanning multiple 16-byte blocks.")

	var qaDigest [16]byte
	for i := range qaDigest {
		qaDigest[i] = byte(0xA0 + i)
	}

	numBlocks := (len(plaintext) + 15) / 16
	encrypted := make([]byte, numBlocks*16)
	padded := make([]byte, numBlocks*16)
	copy(padded, plaintext)
	for i := 0; i < numBlocks; i++ {
		ks := debugKeystreamBlock(qaDigest, uint64(i))
		for j := 0; j < 16; j++ {
			encrypted[i*16+j] = padded[i*16+j] ^ ks[j]
		}
	}

	const dataOffset = 0x80
	hdr := &header{dataOffset: dataOffset, qaDigest: qaDigest, releaseType: pkgReleaseTypeDebug}
	f := writeFakePKGFile(t, dataOffset, encrypted)

	cases := []struct {
		name        string
		start, size int
	}{
		{"full range", 0, len(plaintext)},
		{"unaligned start", 3, 12},
		{"spans block boundary", 10, 25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decryptRegion(f, hdr, uint64(tc.start), uint32(tc.size))
			if err != nil {
				t.Fatalf("decryptRegion: %v", err)
			}
			want := plaintext[tc.start : tc.start+tc.size]
			if string(got) != string(want) {
				t.Errorf("decryptRegion(%d, %d) = %q, want %q", tc.start, tc.size, got, want)
			}
		})
	}
}

func TestDecryptRegion_ZeroSize(t *testing.T) {
	hdr := &header{dataOffset: 0, releaseType: pkgReleaseTypeRetail}
	f := writeFakePKGFile(t, 0, nil)
	got, err := decryptRegion(f, hdr, 0, 0)
	if err != nil {
		t.Fatalf("decryptRegion: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("decryptRegion size=0 = %v, want empty", got)
	}
}
