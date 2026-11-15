package psn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"PSNWDL/internal/activity"
)

// fakeCDN answers requests by "METHOD host/path"; anything unknown is a
// transport error, like an unreachable region.
type fakeCDN struct {
	mu        sync.Mutex
	responses map[string]fakeResponse
	requests  []string
}

type fakeResponse struct {
	status        int
	body          string
	contentLength int64
}

func (f *fakeCDN) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.Method + " " + req.URL.Host + req.URL.Path
	f.mu.Lock()
	f.requests = append(f.requests, key)
	resp, ok := f.responses[key]
	f.mu.Unlock()
	if !ok {
		return nil, errors.New("unreachable: " + key)
	}
	length := resp.contentLength
	if length == 0 {
		length = int64(len(resp.body))
	}
	return &http.Response{
		StatusCode:    resp.status,
		Body:          io.NopCloser(strings.NewReader(resp.body)),
		ContentLength: length,
		Header:        make(http.Header),
	}, nil
}

func (f *fakeCDN) client() (*Client, *activity.Sink) {
	sink := activity.NewSink(nil)
	return &Client{http: &http.Client{Transport: f}, activity: sink}, sink
}

func hasEntry(sink *activity.Sink, level activity.Level, fragment string) bool {
	for _, e := range sink.GetEntries() {
		if e.Scope == "psn" && e.Level == level && strings.Contains(e.Message, fragment) {
			return true
		}
	}
	return false
}

func TestLookupPS3FirmwareFansOutAndFillsSizes(t *testing.T) {
	cdn := &fakeCDN{responses: map[string]fakeResponse{
		"GET fus01.ps3.update.playstation.net/update/ps3/list/us/ps3-updatelist.txt": {status: 200,
			body: "CompatibleSystemSoftwareVersion=0490;SystemSoftwarePackageUrl=https://dus01.ps3.update.playstation.net/us/PS3UPDAT.PUP\n"},
		"HEAD dus01.ps3.update.playstation.net/us/PS3UPDAT.PUP": {status: 200, contentLength: 200_000_000},
		"GET fjp01.ps3.update.playstation.net/update/ps3/list/jp/ps3-updatelist.txt": {status: 200,
			body: "SystemSoftwareVersion=4.9100;CDN=http://djp01.ps3.update.playstation.net/jp/PS3UPDAT.PUP\n"},
		"HEAD djp01.ps3.update.playstation.net/jp/PS3UPDAT.PUP":                      {status: 404},
		"GET feu01.ps3.update.playstation.net/update/ps3/list/eu/ps3-updatelist.txt": {status: 503},
	}}
	client, sink := cdn.client()

	list, err := client.LookupPS3Firmware(context.Background())
	if err != nil {
		t.Fatalf("LookupPS3Firmware: %v", err)
	}
	if list.Console != "ps3" || len(list.Entries) != 2 {
		t.Fatalf("list = %+v, want two entries", list)
	}
	jp, us := list.Entries[0], list.Entries[1]
	if jp.Region != "jp" || jp.Version != "4.91" || jp.Size != 0 {
		t.Errorf("jp entry = %+v (a failed HEAD leaves size unknown)", jp)
	}
	if us.Region != "us" || us.Version != "4.90" || us.Size != 200_000_000 || us.Type != "Firmware" {
		t.Errorf("us entry = %+v", us)
	}
	if !hasEntry(sink, activity.LevelWarn, "PS3 firmware eu: unreachable (status 503)") {
		t.Error("an unavailable region was not narrated")
	}
	if !hasEntry(sink, activity.LevelInfo, "PS3 firmware us: 1 entry") ||
		!hasEntry(sink, activity.LevelInfo, "PS3 firmware: 2 downloadable entries") {
		t.Error("the fan-out result was not narrated")
	}
}

func TestLookupXMLFirmwarePerConsole(t *testing.T) {
	const noSizeXML = `<?xml version="1.0"?>
<update_data_list>
  <region id="eu">
    <system_pup label="9.00" auto_update_version="09.000.000">
      <update_data update_type="full"><image>https://example.com/FULL.PUP</image></update_data>
    </system_pup>
  </region>
</update_data_list>`

	for _, tc := range []struct {
		console string
		lookup  func(*Client, context.Context) (*FirmwareList, error)
		url     func(region string) string
	}{
		{"ps4", (*Client).LookupPS4Firmware, func(r string) string {
			return fmt.Sprintf("f%s01.ps4.update.playstation.net/update/ps4/list/%s/ps4-updatelist.xml", r, r)
		}},
		{"ps5", (*Client).LookupPS5Firmware, func(r string) string {
			return fmt.Sprintf("f%s01.ps5.update.playstation.net/update/ps5/official/%s/list/%s/updatelist.xml", r, ps5FirmwareToken, r)
		}},
		{"psvita", (*Client).LookupVitaFirmware, func(r string) string {
			return fmt.Sprintf("f%s01.psp2.update.playstation.net/update/psp2/list/%s/psp2-updatelist.xml", r, r)
		}},
	} {
		t.Run(tc.console, func(t *testing.T) {
			cdn := &fakeCDN{responses: map[string]fakeResponse{
				"GET " + tc.url("us"):                  {status: 200, body: validPS4FirmwareXML},
				"GET " + tc.url("eu"):                  {status: 200, body: noSizeXML},
				"HEAD example.com/FULL.PUP":            {status: 200, contentLength: 1234},
				"GET " + tc.url("jp"):                  {status: 200, body: "<update_data_list"},
				"GET " + tc.url("kr"):                  {status: 500},
				"HEAD example.com/PS4UPDATE.PUP":       {status: 500},
				"HEAD example.com/PS4RECOVERY.PUP":     {status: 500},
				"HEAD example.com/not-requested.PUP":   {status: 500},
				"GET example.com/never-requested.json": {status: 500},
			}}
			client, sink := cdn.client()

			list, err := tc.lookup(client, context.Background())
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if list.Console != tc.console || len(list.Entries) != 3 {
				t.Fatalf("list = %+v, want 3 entries from us and eu", list)
			}
			// Sorted by region, then type, then newest version first.
			if list.Entries[0].Region != "eu" || list.Entries[0].Size != 1234 {
				t.Errorf("eu entry = %+v, want its size filled by HEAD", list.Entries[0])
			}
			if list.Entries[1].Region != "us" || list.Entries[1].Type > list.Entries[2].Type {
				t.Errorf("entries not sorted: %+v", list.Entries)
			}
			if list.Entries[1].Size == 0 || list.Entries[2].Size == 0 {
				t.Errorf("sizes from the XML were lost: %+v", list.Entries)
			}
			for _, region := range []string{"jp", "kr", "cn"} {
				if !hasEntry(sink, activity.LevelWarn, tc.console+" firmware "+region+": unreachable") {
					t.Errorf("region %s failure was not narrated", region)
				}
			}
			for _, req := range cdn.requests {
				if strings.Contains(req, "PS4UPDATE.PUP") || strings.Contains(req, "PS4RECOVERY.PUP") {
					t.Errorf("sized entry was HEAD-probed again: %s", req)
				}
			}
		})
	}
}

func TestLookupPS4ResolvesManifests(t *testing.T) {
	const verXML = `<?xml version="1.0"?>
<titlepatch status="OK" titleid="CUSA00001">
  <tag name="00_00_00">
    <package version="01.02" size="300" manifest_url="https://gs2.ww.prod.dl.playstation.net/m/split.json" ps4_system_ver="09.00"/>
    <package version="01.03" size="100" manifest_url="https://gs2.ww.prod.dl.playstation.net/m/single.json" ps4_system_ver="09.00"/>
    <package version="01.04" size="100" manifest_url="https://gs2.ww.prod.dl.playstation.net/m/broken.json" ps4_system_ver="09.00"/>
    <package version="01.05" size="100" url="https://gs2.ww.prod.dl.playstation.net/direct.pkg" ps4_system_ver="09.00"/>
  </tag>
</titlepatch>`
	verKey := "GET gs-sec.ww.np.dl.playstation.net/plo/np/CUSA00001/" + ps4HMAC("CUSA00001") + "/CUSA00001-ver.xml"
	cdn := &fakeCDN{responses: map[string]fakeResponse{
		verKey: {status: 200, body: verXML},
		"GET gs2.ww.prod.dl.playstation.net/m/split.json": {status: 200, body: `{"pieces":[
			{"url":"https://p/a.pkg","hashValue":"aa","fileSize":100},
			{"url":"","hashValue":"skip","fileSize":1},
			{"url":"https://p/b.pkg","hashValue":"bb","fileSize":200}]}`},
		"GET gs2.ww.prod.dl.playstation.net/m/single.json": {status: 200, body: `{"pieces":[{"url":"https://p/c.pkg","hashValue":"cc","fileSize":100}]}`},
		"GET gs2.ww.prod.dl.playstation.net/m/broken.json": {status: 500},
	}}
	client, sink := cdn.client()

	title, err := client.LookupPS4(context.Background(), "CUSA00001")
	if err != nil {
		t.Fatalf("LookupPS4: %v", err)
	}
	got := []string{}
	for _, u := range title.Updates {
		got = append(got, u.Version+"="+u.URL+"#"+u.SHA1Sum)
	}
	want := []string{
		"01.02 part 1=https://p/a.pkg#aa",
		"01.02 part 3=https://p/b.pkg#bb",
		"01.03=https://p/c.pkg#cc",
		"01.04=https://gs2.ww.prod.dl.playstation.net/m/broken.json#",
		"01.05=https://gs2.ww.prod.dl.playstation.net/direct.pkg#",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("updates:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !hasEntry(sink, activity.LevelWarn, "resolve ps4 manifest: manifest status 500") {
		t.Error("the failed manifest was not narrated")
	}
}

func TestLookupPS4AndVitaReportFailures(t *testing.T) {
	ps4Key := "GET gs-sec.ww.np.dl.playstation.net/plo/np/CUSA00001/" + ps4HMAC("CUSA00001") + "/CUSA00001-ver.xml"
	vitaKey := "GET gs-sec.ww.np.dl.playstation.net/pl/np/PCSE00001/" + vitaHMAC("PCSE00001") + "/PCSE00001-ver.xml"

	for name, tc := range map[string]struct {
		key    string
		lookup func(*Client) (*Title, error)
	}{
		"ps4":  {ps4Key, func(c *Client) (*Title, error) { return c.LookupPS4(context.Background(), "CUSA00001") }},
		"vita": {vitaKey, func(c *Client) (*Title, error) { return c.LookupVita(context.Background(), "PCSE00001") }},
	} {
		t.Run(name, func(t *testing.T) {
			for label, resp := range map[string]*fakeResponse{
				"transport":   nil,
				"status 500":  {status: 500},
				"malformed":   {status: 200, body: "<titlepatch"},
				"wrong title": {status: 200, body: `<titlepatch status="OK" titleid="BLES00001"></titlepatch>`},
			} {
				cdn := &fakeCDN{responses: map[string]fakeResponse{}}
				if resp != nil {
					cdn.responses[tc.key] = *resp
				}
				client, _ := cdn.client()
				if title, err := tc.lookup(client); err == nil {
					t.Errorf("%s: lookup = %+v, want an error", label, title)
				}
			}
			cdn := &fakeCDN{}
			client, _ := cdn.client()
			if name == "ps4" {
				if _, err := client.LookupPS4(context.Background(), "bad"); err == nil {
					t.Error("LookupPS4 accepted an invalid title id")
				}
			} else if _, err := client.LookupVita(context.Background(), "bad"); err == nil {
				t.Error("LookupVita accepted an invalid title id")
			}
			if len(cdn.requests) != 0 {
				t.Errorf("an invalid title id reached the network: %v", cdn.requests)
			}
		})
	}
}

func TestLookupVitaParsesUpdates(t *testing.T) {
	key := "GET gs-sec.ww.np.dl.playstation.net/pl/np/PCSA00001/" + vitaHMAC("PCSA00001") + "/PCSA00001-ver.xml"
	cdn := &fakeCDN{responses: map[string]fakeResponse{key: {status: 200, body: validVitaVerXML}}}
	client, _ := cdn.client()
	title, err := client.LookupVita(context.Background(), "PCSA00001")
	if err != nil {
		t.Fatalf("LookupVita: %v", err)
	}
	want, err := parseVitaVerXML([]byte(validVitaVerXML), "PCSA00001")
	if err != nil {
		t.Fatal(err)
	}
	if len(title.Updates) == 0 || len(title.Updates) != len(want.Updates) {
		t.Fatalf("updates = %+v, want %+v", title.Updates, want.Updates)
	}
}

func TestFirmwareRequestsHonorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &Client{
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
		})},
		activity: activity.NewSink(nil),
	}
	list, err := client.LookupPS4Firmware(ctx)
	if err != nil || len(list.Entries) != 0 {
		t.Fatalf("canceled lookup = %+v, %v; want an empty list", list, err)
	}
	if client.contentLength(ctx, "https://example.com/x") != 0 {
		t.Fatal("contentLength of a canceled request should be 0")
	}
	if client.contentLength(context.Background(), "::bad url") != 0 {
		t.Fatal("contentLength of a bad URL should be 0")
	}
}
