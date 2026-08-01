package pkg

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// ps3NpdrmKey is the fixed AES key for retail PS3 NPDRM PKGs.
// Identical to PyKG's PS3_NPDRM_KEY.
var ps3NpdrmKey = []byte{
	0x2e, 0x7b, 0x71, 0xd7, 0xc9, 0xc9, 0xa1, 0x4e,
	0xa3, 0x22, 0x1f, 0x18, 0x88, 0x28, 0xb8, 0xf8,
}

// debugKeystreamBlock returns 16 bytes of keystream for a single 16-byte
// block of a debug PKG's encrypted stream.
// Faithfully ported from PyKG's get_debug_keystream_block().
//
// Layout of the 64-byte SHA-1 input buffer:
//
//	[0:8]  = qa_digest[0:8]   (qa_0)
//	[8:16] = qa_digest[0:8]   (qa_0 repeated)
//	[16:24]= qa_digest[8:16]  (qa_1)
//	[24:32]= qa_digest[8:16]  (qa_1 repeated)
//	[32:56]= 0x00 bytes
//	[56:64]= block_index as big-endian uint64
func debugKeystreamBlock(qaDigest [16]byte, blockIndex uint64) [16]byte {
	var buf [64]byte
	qa0 := qaDigest[0:8]
	qa1 := qaDigest[8:16]
	copy(buf[0:8], qa0)
	copy(buf[8:16], qa0)
	copy(buf[16:24], qa1)
	copy(buf[24:32], qa1)
	// bytes [32:56] stay zero
	binary.BigEndian.PutUint64(buf[56:64], blockIndex)

	sum := sha1.Sum(buf[:])
	var out [16]byte
	copy(out[:], sum[:16])
	return out
}

// decryptRegion decrypts `size` bytes of PKG data starting at `streamPos`
// (relative to the start of the encrypted stream, i.e. relative to
// hdr.dataOffset).
//
// PyKG calls this decrypt_region(f, hdr, stream_pos, size).
// The AES-CTR counter is derived from hdr.iv (retail) or qa_digest (debug).
func decryptRegion(f *os.File, hdr *header, streamPos uint64, size uint32) ([]byte, error) {
	if size == 0 {
		return []byte{}, nil
	}

	// Align back to the nearest 16-byte block boundary.
	blockStart := streamPos &^ 0xF
	prefixLen := streamPos - blockStart
	numBytes := uint64(prefixLen) + uint64(size)
	numBlocks := (numBytes + 15) / 16

	readOff := hdr.dataOffset + blockStart
	readLen := numBlocks * 16

	if _, err := f.Seek(int64(readOff), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to encrypted region: %w", err)
	}
	enc := make([]byte, readLen)
	if _, err := io.ReadFull(f, enc); err != nil {
		return nil, fmt.Errorf("read encrypted region: %w", err)
	}

	plain := make([]byte, len(enc))

	if hdr.releaseType == pkgReleaseTypeDebug {
		// Debug PKG: keystream = SHA-1 of 64-byte buffer per block.
		blockIndex := blockStart / 16
		for i := uint64(0); i < numBlocks; i++ {
			ks := debugKeystreamBlock(hdr.qaDigest, blockIndex+i)
			off := i * 16
			for j := 0; j < 16; j++ {
				plain[off+uint64(j)] = enc[off+uint64(j)] ^ ks[j]
			}
		}
	} else {
		// Retail PKG: AES-128-CTR with key=PS3_NPDRM_KEY, counter starts at
		// (iv_as_big_int + block_start/16) mod 2^128.
		block, err := aes.NewCipher(ps3NpdrmKey)
		if err != nil {
			return nil, fmt.Errorf("create AES cipher: %w", err)
		}

		// Build the initial counter value = (iv + blockStart/16) mod 2^128.
		// iv is stored big-endian; we do 128-bit addition manually.
		ctrBytes := ivPlusOffset(hdr.iv, blockStart/16)

		stream := cipher.NewCTR(block, ctrBytes[:])
		stream.XORKeyStream(plain, enc)
	}

	return plain[prefixLen : prefixLen+uint64(size)], nil
}

// ivPlusOffset adds a uint64 to a 16-byte big-endian integer (128-bit),
// matching PyKG's:
//
//	ctr_val = (iv_int + block_start // 16) & ((1 << 128) - 1)
func ivPlusOffset(iv [16]byte, offset uint64) [16]byte {
	// Treat iv as two 64-bit halves: high [0:8], low [8:16].
	hi := binary.BigEndian.Uint64(iv[0:8])
	lo := binary.BigEndian.Uint64(iv[8:16])

	newLo := lo + offset
	carry := uint64(0)
	if newLo < lo {
		carry = 1
	}
	newHi := hi + carry

	var result [16]byte
	binary.BigEndian.PutUint64(result[0:8], newHi)
	binary.BigEndian.PutUint64(result[8:16], newLo)
	return result
}
