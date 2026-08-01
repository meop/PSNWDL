package pkg

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// PKG_MAGIC is the four-byte magic that begins every valid PS3 PKG.
const PKG_MAGIC = "\x7fPKG"

// pkg type and release type constants from PyKG.
const (
	pkgTypePS3           = 0x0001
	pkgReleaseTypeDebug  = 0x0000
	pkgReleaseTypeRetail = 0x8000
)

// hdrSize matches struct.calcsize(">4sHHIIIIQQQ48s16s16s") = 4+2+2+4+4+4+4+8+8+8+48+16+16 = 132.
const hdrSize = 132

// header holds the parsed fields we need from a PS3 PKG header.
// Field names and offsets are taken directly from PyKG's read_header / HDR_FMT.
type header struct {
	itemCount   uint32
	dataOffset  uint64
	contentID   string
	iv          [16]byte // riv field — AES-CTR initial value for retail PKGs
	qaDigest    [16]byte // digest field — used to derive keystream for debug PKGs
	releaseType uint16   // == revision field; 0x0000 = debug, 0x8000 = retail
}

// ReadHeader parses the 132-byte PKG header from f (seeking to byte 0).
// Returns an error for non-PS3 PKGs or magic mismatches.
func ReadHeader(f *os.File) (*header, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to header: %w", err)
	}

	raw := make([]byte, hdrSize)
	if _, err := io.ReadFull(f, raw); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Check magic (first 4 bytes).
	if string(raw[0:4]) != PKG_MAGIC {
		return nil, fmt.Errorf("not a valid PKG file (magic=%q)", raw[0:4])
	}

	// All fields are big-endian (">").
	// Layout: magic[4] revision[2] pkg_type[2] meta_offset[4] meta_count[4]
	//         header_size[4] item_count[4] total_size[8] data_offset[8]
	//         data_size[8] content_id[48] digest[16] riv[16]
	revision := binary.BigEndian.Uint16(raw[4:6])
	pkgType := binary.BigEndian.Uint16(raw[6:8])
	// meta_offset [8:12], meta_count [12:16], header_size [16:20] — not needed
	itemCount := binary.BigEndian.Uint32(raw[20:24])
	// total_size [24:32]
	dataOffset := binary.BigEndian.Uint64(raw[32:40])
	// data_size [40:48]
	contentIDRaw := raw[48:96] // 48 bytes

	var digest [16]byte
	copy(digest[:], raw[96:112])
	var riv [16]byte
	copy(riv[:], raw[112:128])

	if pkgType != pkgTypePS3 {
		return nil, fmt.Errorf("not a PS3 PKG (type=%#06x): only PS3 NPDRM PKGs are supported", pkgType)
	}

	// Trim null bytes from content_id, same as PyKG's .rstrip(b"\x00").decode().
	end := len(contentIDRaw)
	for end > 0 && contentIDRaw[end-1] == 0 {
		end--
	}
	contentID := string(contentIDRaw[:end])

	return &header{
		itemCount:   itemCount,
		dataOffset:  dataOffset,
		contentID:   contentID,
		iv:          riv,
		qaDigest:    digest,
		releaseType: revision,
	}, nil
}
