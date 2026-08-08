package pkg

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// e2eItem is one file entry in a synthetic encrypted PKG built for the
// end-to-end Extract() test below.
type e2eItem struct {
	name string
	data []byte
}

// buildEncryptedPKG lays out a minimal, fully valid retail PS3 NPDRM PKG:
// a real 132-byte header followed by an item table, item names, and item
// data, all encrypted as one continuous AES-CTR stream starting at counter
// = iv (matching how decryptRegion recomputes the counter for any
// requested sub-range: AES-CTR is randomly addressable, so encrypting the
// whole logical stream in one pass here is equivalent to what a real PKG
// writer/decrypter does region by region). This is what lets the test
// exercise Extract()'s full item-table parsing, name decryption, and
// writeItem streaming — not just the pure helpers around it.
func buildEncryptedPKG(t *testing.T, items []e2eItem) string {
	t.Helper()

	const dataOffset = hdrSize

	type laidOutItem struct {
		nameOff, itemDataOff   uint32
		nameSize, itemDataSize uint32
	}

	// First pass: lay out the item table, then each name, then each data
	// blob, sequentially within the logical encrypted stream (streamPos 0
	// is the start of the item table).
	tableSize := uint32(len(items)) * itemRecordSize
	cursor := tableSize
	laid := make([]laidOutItem, len(items))
	for i, it := range items {
		laid[i].nameOff = cursor
		laid[i].nameSize = uint32(len(it.name))
		cursor += laid[i].nameSize
	}
	for i, it := range items {
		laid[i].itemDataOff = cursor
		laid[i].itemDataSize = uint32(len(it.data))
		cursor += laid[i].itemDataSize
	}

	// decryptRegion always reads whole 16-byte blocks, even for a request
	// that ends mid-block, so pad the logical stream out to a block boundary
	// the same way a real PKG's encrypted stream naturally continues past
	// any single item's exact byte count.
	paddedLen := (cursor + 15) &^ 15
	plain := make([]byte, paddedLen)
	for i, it := range laid {
		rec := plain[i*itemRecordSize : (i+1)*itemRecordSize]
		binary.BigEndian.PutUint32(rec[0:4], it.nameOff)
		binary.BigEndian.PutUint32(rec[4:8], it.nameSize)
		binary.BigEndian.PutUint64(rec[8:16], uint64(it.itemDataOff))
		binary.BigEndian.PutUint64(rec[16:24], uint64(it.itemDataSize))
		// flags left 0 (not a directory); trailing 4 bytes unused padding.
	}
	for i, it := range items {
		copy(plain[laid[i].nameOff:], it.name)
		copy(plain[laid[i].itemDataOff:], it.data)
	}

	var iv [16]byte
	binary.BigEndian.PutUint64(iv[8:16], 0x1234)

	block, err := aes.NewCipher(ps3NpdrmKey)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCTR(block, iv[:]).XORKeyStream(encrypted, plain)

	header := make([]byte, hdrSize)
	copy(header[0:4], PKG_MAGIC)
	binary.BigEndian.PutUint16(header[4:6], pkgReleaseTypeRetail)
	binary.BigEndian.PutUint16(header[6:8], pkgTypePS3)
	binary.BigEndian.PutUint32(header[20:24], uint32(len(items)))
	binary.BigEndian.PutUint64(header[32:40], uint64(dataOffset))
	copy(header[112:128], iv[:])

	path := filepath.Join(t.TempDir(), "test.pkg")
	if err := os.WriteFile(path, append(header, encrypted...), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestExtract_RetailPKG_RoundTrip builds a small but fully encrypted PS3 PKG
// (PARAM.SFO + one USRDIR file), runs it through the real Extract() pipeline,
// and checks the decrypted files land on disk with the right content. Unlike
// the rest of extract_test.go (which only tests pure helpers in isolation),
// this exercises header parsing, item-table decryption, name decryption, and
// writeItem's chunked decryption together — the path a real PKG install
// actually takes.
func TestExtract_RetailPKG_RoundTrip(t *testing.T) {
	sfoData := buildSFO(t, map[string]string{
		"TITLE_ID": "BCUS98114",
		"APP_VER":  "01.00",
		"TITLE":    "Test Game",
	})
	ebootData := []byte("this is a fake but nontrivial EBOOT.BIN payload for the round trip test")

	pkgPath := buildEncryptedPKG(t, []e2eItem{
		{name: "PARAM.SFO", data: sfoData},
		{name: "USRDIR/EBOOT.BIN", data: ebootData},
	})

	destBase := t.TempDir()
	info, err := Extract(pkgPath, destBase)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if info.TitleID != "BCUS98114" {
		t.Errorf("TitleID = %q, want BCUS98114", info.TitleID)
	}
	if info.AppVer != "01.00" {
		t.Errorf("AppVer = %q, want 01.00", info.AppVer)
	}
	if info.Title != "Test Game" {
		t.Errorf("Title = %q, want Test Game", info.Title)
	}

	gotEboot, err := os.ReadFile(filepath.Join(destBase, "BCUS98114", "USRDIR", "EBOOT.BIN"))
	if err != nil {
		t.Fatalf("read extracted EBOOT.BIN: %v", err)
	}
	if string(gotEboot) != string(ebootData) {
		t.Errorf("EBOOT.BIN content = %q, want %q", gotEboot, ebootData)
	}

	gotSFO, err := os.ReadFile(filepath.Join(destBase, "BCUS98114", "PARAM.SFO"))
	if err != nil {
		t.Fatalf("read extracted PARAM.SFO: %v", err)
	}
	if string(gotSFO) != string(sfoData) {
		t.Error("extracted PARAM.SFO content does not match source")
	}
}

// TestDiscoverPKGs_RetailPKG confirms DiscoverPKGs (used to build the batch
// install queue) can probe a real encrypted PKG's header + PARAM.SFO without
// extracting it.
func TestDiscoverPKGs_RetailPKG(t *testing.T) {
	sfoData := buildSFO(t, map[string]string{
		"TITLE_ID": "BCUS98114",
		"APP_VER":  "02.01",
		"TITLE":    "Discoverable Game",
	})
	pkgPath := buildEncryptedPKG(t, []e2eItem{{name: "PARAM.SFO", data: sfoData}})

	found, err := DiscoverPKGs(filepath.Dir(pkgPath))
	if err != nil {
		t.Fatalf("DiscoverPKGs: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d pkg(s), want 1", len(found))
	}
	if found[0].TitleID != "BCUS98114" || found[0].AppVer != "02.01" || found[0].Title != "Discoverable Game" {
		t.Errorf("found[0] = %+v, want BCUS98114/02.01/Discoverable Game", found[0])
	}
}
