package pkg

import (
	"encoding/binary"
	"testing"
)

// buildSFO constructs a minimal PARAM.SFO binary containing the given
// key=value pairs (all treated as UTF-8 string type, fmt=0x0204).
// This is sufficient to test parseSFO without a real PKG.
func buildSFO(t *testing.T, kvs map[string]string) []byte {
	t.Helper()

	// Collect keys in a stable order.
	keys := []string{"APP_VER", "TITLE", "TITLE_ID"}
	entries := make([]struct{ k, v string }, 0, 3)
	for _, k := range keys {
		if v, ok := kvs[k]; ok {
			entries = append(entries, struct{ k, v string }{k, v})
		}
	}

	// --- Build key table ---
	keyTableOff := 0
	for _, e := range entries {
		keyTableOff += len(e.k) + 1 // null-terminated
	}

	// Layout:
	//   [0x00] magic[4]
	//   [0x04] version[4]  (ignored)
	//   [0x08] key_table_start[4]
	//   [0x0C] data_table_start[4]
	//   [0x10] entry_count[4]
	//   [0x14] entries[entry_count * 0x10]
	//   key table
	//   data table

	headerSize := 0x14
	entryAreaSize := len(entries) * 0x10
	keyTableStart := headerSize + entryAreaSize
	keyTableSize := 0
	for _, e := range entries {
		keyTableSize += len(e.k) + 1
	}
	dataTableStart := keyTableStart + keyTableSize

	// Value sizes: each value is padded to max_size bytes.
	// For simplicity we use the actual length + 1 (null terminator).
	type valInfo struct {
		off  int
		size int
	}
	valInfos := make([]valInfo, len(entries))
	off := 0
	for i, e := range entries {
		valInfos[i] = valInfo{off: off, size: len(e.v) + 1}
		off += len(e.v) + 1
	}
	dataTableSize := off

	totalSize := dataTableStart + dataTableSize
	buf := make([]byte, totalSize)

	// Magic
	copy(buf[0:4], sfoMagic)
	// version = 0x01010000 (irrelevant)
	binary.LittleEndian.PutUint32(buf[4:8], 0x01010000)
	// key_table_start
	binary.LittleEndian.PutUint32(buf[0x08:0x0C], uint32(keyTableStart))
	// data_table_start
	binary.LittleEndian.PutUint32(buf[0x0C:0x10], uint32(dataTableStart))
	// entry_count
	binary.LittleEndian.PutUint32(buf[0x10:0x14], uint32(len(entries)))

	// Write entries and key/data tables.
	keyPos := 0
	for i, e := range entries {
		entryOff := 0x14 + i*0x10
		// key_off
		binary.LittleEndian.PutUint16(buf[entryOff:], uint16(keyPos))
		// fmt = 0x0204 (UTF-8 string)
		binary.LittleEndian.PutUint16(buf[entryOff+2:], 0x0204)
		// size
		binary.LittleEndian.PutUint32(buf[entryOff+4:], uint32(valInfos[i].size))
		// max_size (same as size for our test)
		binary.LittleEndian.PutUint32(buf[entryOff+8:], uint32(valInfos[i].size))
		// data_off
		binary.LittleEndian.PutUint32(buf[entryOff+12:], uint32(valInfos[i].off))

		// Key table entry.
		copy(buf[keyTableStart+keyPos:], e.k)
		buf[keyTableStart+keyPos+len(e.k)] = 0
		keyPos += len(e.k) + 1

		// Data table entry.
		copy(buf[dataTableStart+valInfos[i].off:], e.v)
		// remaining bytes are already 0 (null terminator + padding)
	}

	return buf
}

func TestParseSFO_BasicFields(t *testing.T) {
	raw := buildSFO(t, map[string]string{
		"TITLE_ID": "BCUS98114",
		"APP_VER":  "01.05",
		"TITLE":    "Gran Turismo 5",
	})

	info, err := parseSFO(raw)
	if err != nil {
		t.Fatalf("parseSFO error: %v", err)
	}
	if info.TitleID != "BCUS98114" {
		t.Errorf("TitleID: got %q, want BCUS98114", info.TitleID)
	}
	if info.AppVer != "01.05" {
		t.Errorf("AppVer: got %q, want 01.05", info.AppVer)
	}
	if info.Title != "Gran Turismo 5" {
		t.Errorf("Title: got %q, want Gran Turismo 5", info.Title)
	}
}

func TestParseSFO_MissingTitleID(t *testing.T) {
	raw := buildSFO(t, map[string]string{
		"APP_VER": "01.00",
		"TITLE":   "No ID",
	})

	// Replace TITLE_ID with APP_VER in the map so it's absent.
	// buildSFO only writes keys that are in the map, so omitting TITLE_ID
	// means no TITLE_ID entry — but buildSFO above includes APP_VER and TITLE.
	// Reconstruct without TITLE_ID explicitly.
	_, err := parseSFO(raw)
	// This raw has APP_VER + TITLE but no TITLE_ID — should fail.
	if err == nil {
		t.Fatal("expected error for missing TITLE_ID, got nil")
	}
}

func TestParseSFO_BadMagic(t *testing.T) {
	raw := buildSFO(t, map[string]string{"TITLE_ID": "BCUS98114"})
	raw[0] = 0xFF // corrupt magic

	_, err := parseSFO(raw)
	if err == nil {
		t.Fatal("expected error for bad magic, got nil")
	}
}

func TestParseSFO_TooShort(t *testing.T) {
	_, err := parseSFO([]byte{0x00, 'P', 'S', 'F'})
	if err == nil {
		t.Fatal("expected error for too-short data, got nil")
	}
}

func TestParseSFO_TitleIDOnly(t *testing.T) {
	// SFO with only TITLE_ID — should succeed even without TITLE/APP_VER.
	raw := buildSFO(t, map[string]string{"TITLE_ID": "NPEB00301"})
	info, err := parseSFO(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.TitleID != "NPEB00301" {
		t.Errorf("TitleID: got %q, want NPEB00301", info.TitleID)
	}
	if info.AppVer != "" {
		t.Errorf("AppVer: expected empty, got %q", info.AppVer)
	}
}
