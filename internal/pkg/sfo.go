package pkg

import (
	"encoding/binary"
	"fmt"
)

// SFOInfo carries the three PARAM.SFO fields we care about.
type SFOInfo struct {
	TitleID string `json:"title_id"`
	AppVer  string `json:"app_ver"`
	Title   string `json:"title"`
}

// sfoMagic is the 4-byte magic at the start of every PARAM.SFO file.
var sfoMagic = []byte{0x00, 'P', 'S', 'F'}

// parseSFO parses raw PARAM.SFO bytes and returns an SFOInfo.
// Returns an error if the magic is wrong or TITLE_ID is absent.
// Faithfully ported from PyKG's parse_sfo().
func parseSFO(data []byte) (*SFOInfo, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("SFO data too short (%d bytes)", len(data))
	}
	if data[0] != sfoMagic[0] || data[1] != sfoMagic[1] || data[2] != sfoMagic[2] || data[3] != sfoMagic[3] {
		return nil, fmt.Errorf("not a valid SFO (magic=%q)", data[0:4])
	}

	// All SFO integers are little-endian ("<" in Python's struct).
	keyTableStart := binary.LittleEndian.Uint32(data[0x08:0x0C])
	dataTableStart := binary.LittleEndian.Uint32(data[0x0C:0x10])
	entryCount := binary.LittleEndian.Uint32(data[0x10:0x14])

	info := &SFOInfo{}
	foundTitleID := false

	for i := uint32(0); i < entryCount; i++ {
		entryOff := 0x14 + i*0x10
		if int(entryOff)+16 > len(data) {
			break
		}

		// Entry layout: key_off[2] fmt[2] size[4] max_size[4] data_off[4]
		keyOff := binary.LittleEndian.Uint16(data[entryOff:])
		// fmt [entryOff+2:entryOff+4] — not used for these fields
		size := binary.LittleEndian.Uint32(data[entryOff+4:])
		// max_size [entryOff+8:entryOff+12] — not used
		dataOff := binary.LittleEndian.Uint32(data[entryOff+12:])

		// Decode key: null-terminated string in the key table.
		keyStart := int(keyTableStart) + int(keyOff)
		if keyStart >= len(data) {
			continue
		}
		keyEnd := keyStart
		for keyEnd < len(data) && data[keyEnd] != 0 {
			keyEnd++
		}
		key := string(data[keyStart:keyEnd])

		// Decode value: fixed-size bytes from the data table, strip null padding.
		valStart := int(dataTableStart) + int(dataOff)
		valEnd := valStart + int(size)
		if valEnd > len(data) {
			valEnd = len(data)
		}
		if valStart >= len(data) {
			continue
		}
		raw := data[valStart:valEnd]
		// Strip trailing null bytes, like Python's .rstrip(b'\0').decode().
		nullIdx := len(raw)
		for nullIdx > 0 && raw[nullIdx-1] == 0 {
			nullIdx--
		}
		value := string(raw[:nullIdx])

		switch key {
		case "TITLE_ID":
			info.TitleID = value
			foundTitleID = true
		case "APP_VER":
			info.AppVer = value
		case "TITLE":
			info.Title = value
		}
	}

	if !foundTitleID {
		return nil, fmt.Errorf("SFO missing TITLE_ID")
	}
	return info, nil
}

// ParseSFO parses raw PARAM.SFO bytes and returns an SFOInfo.
// Returns an error if the magic is wrong or TITLE_ID is absent.
// Exported for use by the library reconciliation code.
func ParseSFO(data []byte) (*SFOInfo, error) {
	return parseSFO(data)
}
