// Package pkgtest builds small but fully valid PS3 PKG and PARAM.SFO files
// for tests outside package pkg. The layout constants mirror internal/pkg;
// pkg's own round-trip tests keep that parser and this builder in step.
package pkgtest

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

const (
	hdrSize              = 132
	itemRecordSize       = 0x20
	pkgMagic             = "\x7fPKG"
	pkgTypePS3           = 0x0001
	pkgReleaseTypeRetail = 0x8000
)

// ps3NpdrmKey is the public PS3 NPDRM retail PKG key.
var ps3NpdrmKey = []byte{
	0x2e, 0x7b, 0x71, 0xd7, 0xc9, 0xc9, 0xa1, 0x4e,
	0xa3, 0x22, 0x1f, 0x18, 0x88, 0x28, 0xb8, 0xf8,
}

// Item is one file entry in a PKG.
type Item struct {
	Name string
	Data []byte
}

// SFO returns a PARAM.SFO holding TITLE_ID, APP_VER and TITLE as UTF-8
// strings.
func SFO(titleID, appVer, title string) []byte {
	entries := []struct{ k, v string }{
		{"APP_VER", appVer},
		{"TITLE", title},
		{"TITLE_ID", titleID},
	}
	const headerSize = 0x14
	keyTableStart := headerSize + len(entries)*0x10
	keyTableSize := 0
	dataTableSize := 0
	for _, e := range entries {
		keyTableSize += len(e.k) + 1
		dataTableSize += len(e.v) + 1
	}
	dataTableStart := keyTableStart + keyTableSize

	buf := make([]byte, dataTableStart+dataTableSize)
	copy(buf[0:4], []byte{0x00, 'P', 'S', 'F'})
	binary.LittleEndian.PutUint32(buf[4:8], 0x01010000)
	binary.LittleEndian.PutUint32(buf[0x08:0x0C], uint32(keyTableStart))
	binary.LittleEndian.PutUint32(buf[0x0C:0x10], uint32(dataTableStart))
	binary.LittleEndian.PutUint32(buf[0x10:0x14], uint32(len(entries)))

	keyPos, dataPos := 0, 0
	for i, e := range entries {
		entryOff := headerSize + i*0x10
		size := uint32(len(e.v) + 1)
		binary.LittleEndian.PutUint16(buf[entryOff:], uint16(keyPos))
		binary.LittleEndian.PutUint16(buf[entryOff+2:], 0x0204)
		binary.LittleEndian.PutUint32(buf[entryOff+4:], size)
		binary.LittleEndian.PutUint32(buf[entryOff+8:], size)
		binary.LittleEndian.PutUint32(buf[entryOff+12:], uint32(dataPos))
		copy(buf[keyTableStart+keyPos:], e.k)
		copy(buf[dataTableStart+dataPos:], e.v)
		keyPos += len(e.k) + 1
		dataPos += len(e.v) + 1
	}
	return buf
}

// PKG returns the bytes of a retail PS3 NPDRM PKG holding items, encrypted
// as one AES-CTR stream the way real PKGs are.
func PKG(items []Item) []byte {
	type laidOut struct{ nameOff, nameSize, dataOff, dataSize uint32 }

	cursor := uint32(len(items)) * itemRecordSize
	laid := make([]laidOut, len(items))
	for i, it := range items {
		laid[i].nameOff, laid[i].nameSize = cursor, uint32(len(it.Name))
		cursor += laid[i].nameSize
	}
	for i, it := range items {
		laid[i].dataOff, laid[i].dataSize = cursor, uint32(len(it.Data))
		cursor += laid[i].dataSize
	}

	plain := make([]byte, (cursor+15)&^15)
	for i, it := range laid {
		rec := plain[i*itemRecordSize : (i+1)*itemRecordSize]
		binary.BigEndian.PutUint32(rec[0:4], it.nameOff)
		binary.BigEndian.PutUint32(rec[4:8], it.nameSize)
		binary.BigEndian.PutUint64(rec[8:16], uint64(it.dataOff))
		binary.BigEndian.PutUint64(rec[16:24], uint64(it.dataSize))
	}
	for i, it := range items {
		copy(plain[laid[i].nameOff:], it.Name)
		copy(plain[laid[i].dataOff:], it.Data)
	}

	var iv [16]byte
	binary.BigEndian.PutUint64(iv[8:16], 0x1234)
	block, err := aes.NewCipher(ps3NpdrmKey)
	if err != nil {
		panic(err)
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCTR(block, iv[:]).XORKeyStream(encrypted, plain)

	header := make([]byte, hdrSize)
	copy(header[0:4], pkgMagic)
	binary.BigEndian.PutUint16(header[4:6], pkgReleaseTypeRetail)
	binary.BigEndian.PutUint16(header[6:8], pkgTypePS3)
	binary.BigEndian.PutUint32(header[20:24], uint32(len(items)))
	binary.BigEndian.PutUint64(header[32:40], hdrSize)
	copy(header[112:128], iv[:])
	return append(header, encrypted...)
}

// TitlePKG returns a PKG for one title version: its PARAM.SFO plus a
// USRDIR/EBOOT.BIN whose content names the version.
func TitlePKG(titleID, appVer, title string) []byte {
	return PKG([]Item{
		{Name: "PARAM.SFO", Data: SFO(titleID, appVer, title)},
		{Name: "USRDIR/EBOOT.BIN", Data: []byte("EBOOT " + titleID + " " + appVer)},
	})
}

// WriteTitlePKG writes TitlePKG to path, creating parent folders.
func WriteTitlePKG(t testing.TB, path, titleID, appVer, title string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, TitlePKG(titleID, appVer, title), 0o644); err != nil {
		t.Fatal(err)
	}
}
