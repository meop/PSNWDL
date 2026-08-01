package pkg

import (
	"encoding/binary"
	"os"
	"testing"
)

// buildMinimalPKG writes a minimal valid PS3 PKG header to a temp file and
// returns the *os.File. The caller must close and remove the file.
func buildMinimalPKG(t *testing.T, itemCount uint32, dataOffset uint64, contentID string, releaseType uint16) *os.File {
	t.Helper()

	buf := make([]byte, hdrSize)

	// magic
	copy(buf[0:4], PKG_MAGIC)
	// revision (release type)
	binary.BigEndian.PutUint16(buf[4:6], releaseType)
	// pkg_type = PS3
	binary.BigEndian.PutUint16(buf[6:8], pkgTypePS3)
	// meta_offset, meta_count, header_size — unused by ReadHeader, leave as 0
	// item_count @ offset 20
	binary.BigEndian.PutUint32(buf[20:24], itemCount)
	// total_size @ 24:32 — unused
	// data_offset @ 32:40
	binary.BigEndian.PutUint64(buf[32:40], dataOffset)
	// data_size @ 40:48 — unused
	// content_id @ 48:96 (48 bytes)
	cid := []byte(contentID)
	if len(cid) > 48 {
		cid = cid[:48]
	}
	copy(buf[48:96], cid)
	// digest @ 96:112 — leave as zero (homebrew)
	// riv (iv) @ 112:128 — leave as zero

	f, err := os.CreateTemp(t.TempDir(), "test-*.pkg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReadHeader_Valid(t *testing.T) {
	f := buildMinimalPKG(t, 42, 0x100, "EP0000-BCUS12345_00-TESTCONTENT00000", pkgReleaseTypeRetail)
	defer f.Close()

	hdr, err := ReadHeader(f)
	if err != nil {
		t.Fatalf("ReadHeader returned unexpected error: %v", err)
	}

	if hdr.itemCount != 42 {
		t.Errorf("itemCount: got %d, want 42", hdr.itemCount)
	}
	if hdr.dataOffset != 0x100 {
		t.Errorf("dataOffset: got %#x, want 0x100", hdr.dataOffset)
	}
	if hdr.contentID != "EP0000-BCUS12345_00-TESTCONTENT00000" {
		t.Errorf("contentID: got %q, want EP0000-BCUS12345_00-TESTCONTENT00000", hdr.contentID)
	}
	if hdr.releaseType != pkgReleaseTypeRetail {
		t.Errorf("releaseType: got %#x, want %#x", hdr.releaseType, pkgReleaseTypeRetail)
	}
}

func TestReadHeader_BadMagic(t *testing.T) {
	f := buildMinimalPKG(t, 1, 0x80, "", pkgReleaseTypeRetail)
	defer f.Close()

	// Overwrite magic with garbage.
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("JUNK")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}

	_, err := ReadHeader(f)
	if err == nil {
		t.Fatal("expected error for bad magic, got nil")
	}
}

func TestReadHeader_NonPS3Type(t *testing.T) {
	f := buildMinimalPKG(t, 1, 0x80, "", pkgReleaseTypeRetail)
	defer f.Close()

	// Overwrite pkg_type at offset 6 with a non-PS3 type.
	if _, err := f.Seek(6, 0); err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint16(make([]byte, 2), 0x0002)
	bad := []byte{0x00, 0x02}
	if _, err := f.Write(bad); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}

	_, err := ReadHeader(f)
	if err == nil {
		t.Fatal("expected error for non-PS3 pkg type, got nil")
	}
}

func TestReadHeader_TooSmall(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tiny-*.pkg")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write([]byte("short")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}

	_, err = ReadHeader(f)
	if err == nil {
		t.Fatal("expected error for too-small file, got nil")
	}
}

func TestReadHeader_DebugReleaseType(t *testing.T) {
	f := buildMinimalPKG(t, 5, 0x200, "NPXS10001", pkgReleaseTypeDebug)
	defer f.Close()

	hdr, err := ReadHeader(f)
	if err != nil {
		t.Fatalf("ReadHeader returned error: %v", err)
	}
	if hdr.releaseType != pkgReleaseTypeDebug {
		t.Errorf("releaseType: got %#x, want %#x", hdr.releaseType, pkgReleaseTypeDebug)
	}
}
