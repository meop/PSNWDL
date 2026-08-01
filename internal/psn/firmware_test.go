package psn

import (
	"testing"
)

// ---- PS3 firmware TXT parser tests ----

const validPS3FirmwareTXT = `# REGION us
SystemSoftwarePackageUrl=https://example.com/ps3-updatelist-us.pkg;CompatibleSystemSoftwareVersion=0490;SystemSoftwarePackageUrl=https://example.com/PS3UPDAT.PUP;CompatibleSystemSoftwareVersion=0490`

// simplePS3FirmwareTXT uses a cleaner single-record format to test the core
// parsing path without relying on multi-token edge cases.
const simplePS3FirmwareTXT = `CompatibleSystemSoftwareVersion=0490;SystemSoftwarePackageUrl=https://example.com/PS3UPDAT.PUP`

const currentPS3FirmwareTXT = `# US
Dest=84;CompatibleSystemSoftwareVersion=4.9300-;
Dest=84;IncrementalUpdateVersion=00010b72-00010b72;ImageVersion=00010b94;SystemSoftwareVersion=4.9300;CDN=http://example.com/PS3PATCH.PUP;CDN_Timeout=30;
Dest=84;ImageVersion=00010b94;SystemSoftwareVersion=4.9300;CDN=http://example.com/PS3UPDAT.PUP;CDN_Timeout=30;`

func TestParsePS3FirmwareTXT_Simple(t *testing.T) {
	entries, err := parsePS3FirmwareTXT([]byte(simplePS3FirmwareTXT), "us")
	if err != nil {
		t.Fatalf("parsePS3FirmwareTXT: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Region != "us" {
		t.Errorf("Region = %q, want us", e.Region)
	}
	if e.Version != "4.90" {
		t.Errorf("Version = %q, want 4.90", e.Version)
	}
	if e.URL != "https://example.com/PS3UPDAT.PUP" {
		t.Errorf("URL = %q", e.URL)
	}
}

func TestParsePS3FirmwareTXT_CurrentSonyFormat(t *testing.T) {
	entries, err := parsePS3FirmwareTXT([]byte(currentPS3FirmwareTXT), "us")
	if err != nil {
		t.Fatalf("parsePS3FirmwareTXT: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if entries[0].Version != "4.93" {
		t.Errorf("Version = %q, want 4.93", entries[0].Version)
	}
	if entries[0].URL != "http://example.com/PS3UPDAT.PUP" {
		t.Errorf("URL = %q", entries[0].URL)
	}
}

func TestParsePS3FirmwareTXT_Empty(t *testing.T) {
	entries, err := parsePS3FirmwareTXT(nil, "us")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from empty input, got %d", len(entries))
	}
}

func TestNormalisePS3Version(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"0490", "4.90"},
		{"4.9300", "4.93"},
		{"4.9300-", "4.93"},
		{"0355", "3.55"},
		{"0482", "4.82"},
		{"0100", "1.00"},
		// non-standard: pass through unchanged
		{"490", "490"},
		{"", ""},
	}
	for _, tc := range cases {
		got := normalisePS3Version(tc.in)
		if got != tc.want {
			t.Errorf("normalisePS3Version(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---- PS4 / PS5 / Vita firmware XML parser tests ----

const validPS4FirmwareXML = `<?xml version="1.0"?>
<update_data_list>
  <region id="us">
    <system_pup label="13.52" version="13.520.000">
      <update_data update_type="full">
        <image size="503310848">https://example.com/PS4UPDATE.PUP</image>
      </update_data>
    </system_pup>
    <recovery_pup type="default">
      <system_pup label="13.52" version="13.520.000"/>
      <image size="1083618304">https://example.com/PS4RECOVERY.PUP</image>
    </recovery_pup>
  </region>
</update_data_list>`

func TestParseFirmwareXML_PS4(t *testing.T) {
	entries, err := parseFirmwareXML([]byte(validPS4FirmwareXML), "us")
	if err != nil {
		t.Fatalf("parseFirmwareXML: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}
	e := entries[0]
	if e.Region != "us" {
		t.Errorf("Region = %q, want us", e.Region)
	}
	if e.Version != "13.52" {
		t.Errorf("Version = %q, want 13.52", e.Version)
	}
	if e.URL != "https://example.com/PS4UPDATE.PUP" {
		t.Errorf("URL = %q", e.URL)
	}
	if e.Size != 503310848 {
		t.Errorf("Size = %d, want 503310848", e.Size)
	}
	if entries[1].Type != "Recovery" {
		t.Errorf("entries[1].Type = %q, want Recovery", entries[1].Type)
	}
}

func TestParseFirmwareXML_PS5ShortVersion(t *testing.T) {
	const body = `<?xml version="1.0"?>
<update_data_list>
  <region id="us">
    <system_pup auto_update_version="00.00" label="26.04-13.40.00.02-00.00.00.0.1">
      <update_data update_type="full">
        <image size="1246756352">https://example.com/PS5UPDATE.PUP</image>
      </update_data>
    </system_pup>
  </region>
</update_data_list>`
	entries, err := parseFirmwareXML([]byte(body), "us")
	if err != nil {
		t.Fatalf("parseFirmwareXML: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if entries[0].Version != "26.04-13.40.00" {
		t.Errorf("Version = %q, want 26.04-13.40.00", entries[0].Version)
	}
}

func TestParseFirmwareXML_VitaRecoveryTypes(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<update_data_list>
  <region id="us">
    <version system_version="03.740.000" label="3.74">
      <update_data update_type="full">
        <image size="133834240">https://example.com/PSP2UPDAT.PUP</image>
      </update_data>
    </version>
    <recovery spkg_type="systemdata">
      <image spkg_version="01.000.010" size="56778752">https://example.com/PSP2FONT.PUP</image>
    </recovery>
    <recovery spkg_type="preinst">
      <image spkg_version="01.000.000" size="128798720">https://example.com/PSP2PREINST.PUP</image>
    </recovery>
  </region>
</update_data_list>`
	entries, err := parseFirmwareXML([]byte(body), "us")
	if err != nil {
		t.Fatalf("parseFirmwareXML: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(entries))
	}
	if entries[1].Type != "Fonts" {
		t.Errorf("entries[1].Type = %q, want Fonts", entries[1].Type)
	}
	if entries[2].Type != "Preinst" {
		t.Errorf("entries[2].Type = %q, want Preinst", entries[2].Type)
	}
}

func TestParseFirmwareXML_Empty(t *testing.T) {
	entries, err := parseFirmwareXML(nil, "us")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseFirmwareXML_Malformed(t *testing.T) {
	_, err := parseFirmwareXML([]byte("<bad-xml"), "us")
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

// TestPS5FirmwareToken confirms the token constant has not been accidentally
// altered — it is load-bearing in the PS5 URL path.
func TestPS5FirmwareToken(t *testing.T) {
	const want = "tJMRE80IbXnE9YuG0jzTXgKEjIMoabr6"
	if ps5FirmwareToken != want {
		t.Errorf("ps5FirmwareToken = %q, want %q", ps5FirmwareToken, want)
	}
}

// TestFirmwareRegions verifies the region list is populated and includes the
// primary regions used by Sony.
func TestFirmwareRegions(t *testing.T) {
	required := []string{"us", "eu", "jp"}
	regionSet := make(map[string]struct{}, len(firmwareRegions))
	for _, region := range firmwareRegions {
		regionSet[region] = struct{}{}
	}
	for _, region := range required {
		if _, ok := regionSet[region]; !ok {
			t.Errorf("firmwareRegions missing required region %q", region)
		}
	}
}
