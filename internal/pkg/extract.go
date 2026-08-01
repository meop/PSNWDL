package pkg

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// itemRecordSize is the size of each item table entry (ITEM_RECORD_SIZE in PyKG).
const itemRecordSize = 0x20

// flagDir marks a PKG item as a directory (FLAG_DIR in PyKG).
const flagDir = 0x04

// chunkSize is the streaming chunk size for decryption writes.
const chunkSize = 512 * 1024

// pkgItem mirrors the fields unpacked by PyKG's ITEM_FMT (">IIQQII").
type pkgItem struct {
	nameOff      uint32
	nameSize     uint32
	itemDataOff  uint64
	itemDataSize uint64
	flags        uint32
}

// Extract decrypts and extracts a PS3 NPDRM PKG into destBase/<TITLE_ID>/.
// Returns the parsed SFO info on success.
func Extract(pkgPath, destBase string) (*SFOInfo, error) {
	f, err := os.Open(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("open PKG: %w", err)
	}
	defer f.Close()

	hdr, err := ReadHeader(f)
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Decrypt item table (starts at stream position 0, relative to dataOffset).
	tableRaw, err := decryptRegion(f, hdr, 0, uint32(hdr.itemCount)*itemRecordSize)
	if err != nil {
		return nil, fmt.Errorf("decrypt item table: %w", err)
	}

	items := make([]pkgItem, hdr.itemCount)
	for i := uint32(0); i < hdr.itemCount; i++ {
		rec := tableRaw[i*itemRecordSize : (i+1)*itemRecordSize]
		// ITEM_FMT = ">IIQQII" : nameOff[4] nameSize[4] itemDataOff[8] itemDataSize[8] flags[4] _[4]
		items[i] = pkgItem{
			nameOff:      binary.BigEndian.Uint32(rec[0:4]),
			nameSize:     binary.BigEndian.Uint32(rec[4:8]),
			itemDataOff:  binary.BigEndian.Uint64(rec[8:16]),
			itemDataSize: binary.BigEndian.Uint64(rec[16:24]),
			flags:        binary.BigEndian.Uint32(rec[24:28]),
		}
	}

	// Resolve SFO: find PARAM.SFO among items (mirrors find_title_id_and_version).
	sfo, err := findSFO(f, hdr, items)
	if err != nil {
		return nil, fmt.Errorf("find PARAM.SFO: %w", err)
	}

	// Decrypt all item names up front (mirrors PyKG's raw_names list).
	rawNames := make([]string, hdr.itemCount)
	for i, it := range items {
		if it.nameSize == 0 {
			rawNames[i] = ""
			continue
		}
		nameBytes, err := decryptRegion(f, hdr, uint64(it.nameOff), it.nameSize)
		if err != nil {
			return nil, fmt.Errorf("decrypt item name[%d]: %w", i, err)
		}
		// Strip null terminator.
		n := len(nameBytes)
		for n > 0 && nameBytes[n-1] == 0 {
			n--
		}
		rawNames[i] = string(nameBytes[:n])
	}

	// Detect path-traversal PKG (mirrors is_path_pkg).
	pathPKG := isPathPKG(rawNames)

	destRoot := filepath.Clean(destBase)

	for i, it := range items {
		name := rawNames[i]
		if it.nameSize == 0 || name == "" || name == "." || name == ".." {
			continue
		}

		isDir := it.flags&flagDir != 0

		var dest string
		if pathPKG {
			dest = resolvePathPKGDest(name, destRoot)
			if dest == "" {
				continue // unsafe path — skip silently (mirrors PyKG)
			}
		} else {
			// Normal PKG: strip leading slashes and backslashes, place under TITLE_ID.
			dest = resolveNormalPKGDest(name, destRoot, sfo.TitleID)
			if dest == "" {
				continue
			}
		}

		if isDir {
			// If a file exists where a directory should be, remove it first.
			if fi, err := os.Lstat(dest); err == nil && !fi.IsDir() {
				if err := os.Remove(dest); err != nil {
					return nil, fmt.Errorf("remove file blocking dir %q: %w", dest, err)
				}
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return nil, fmt.Errorf("mkdir %q: %w", dest, err)
			}
		} else {
			// Ensure parent directory exists.
			parent := filepath.Dir(dest)
			if fi, err := os.Lstat(parent); err == nil && !fi.IsDir() {
				if err := os.Remove(parent); err != nil {
					return nil, fmt.Errorf("remove file blocking parent dir %q: %w", parent, err)
				}
			}
			if err := os.MkdirAll(parent, 0o755); err != nil {
				return nil, fmt.Errorf("mkdir parent %q: %w", parent, err)
			}

			if err := writeItem(f, hdr, it, dest); err != nil {
				return nil, fmt.Errorf("write item %q: %w", name, err)
			}
		}
	}

	return sfo, nil
}

// writeItem streams-decrypts a single file item and writes it to dest.
func writeItem(f *os.File, hdr *header, it pkgItem, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer out.Close()

	written := uint64(0)
	for written < it.itemDataSize {
		chunk := uint32(chunkSize)
		if uint64(chunk) > it.itemDataSize-written {
			chunk = uint32(it.itemDataSize - written)
		}
		data, err := decryptRegion(f, hdr, it.itemDataOff+written, chunk)
		if err != nil {
			return fmt.Errorf("decrypt chunk at %d: %w", it.itemDataOff+written, err)
		}
		if len(data) == 0 {
			return fmt.Errorf("unexpected EOF while decrypting at offset %d", it.itemDataOff+written)
		}
		if _, err := out.Write(data); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		written += uint64(len(data))
	}
	return nil
}

// findSFO locates and parses PARAM.SFO from the PKG item list.
// Mirrors PyKG's find_title_id_and_version().
func findSFO(f *os.File, hdr *header, items []pkgItem) (*SFOInfo, error) {
	for _, it := range items {
		if it.nameSize == 0 || it.itemDataSize == 0 {
			continue
		}
		nameBytes, err := decryptRegion(f, hdr, uint64(it.nameOff), it.nameSize)
		if err != nil {
			continue
		}
		n := len(nameBytes)
		for n > 0 && nameBytes[n-1] == 0 {
			n--
		}
		name := string(nameBytes[:n])

		if strings.HasSuffix(strings.ToUpper(name), "PARAM.SFO") {
			// Read the entire SFO data item.
			if it.itemDataSize > 4*1024*1024 {
				return nil, fmt.Errorf("PARAM.SFO suspiciously large (%d bytes)", it.itemDataSize)
			}
			sfoData, err := readFullItem(f, hdr, it)
			if err != nil {
				return nil, fmt.Errorf("read PARAM.SFO data: %w", err)
			}
			return parseSFO(sfoData)
		}
	}
	return nil, fmt.Errorf("PARAM.SFO not found in PKG")
}

// readFullItem decrypts an entire item into a single byte slice.
// Used for small items like PARAM.SFO.
func readFullItem(f *os.File, hdr *header, it pkgItem) ([]byte, error) {
	buf := make([]byte, 0, it.itemDataSize)
	written := uint64(0)
	for written < it.itemDataSize {
		chunk := uint32(chunkSize)
		if uint64(chunk) > it.itemDataSize-written {
			chunk = uint32(it.itemDataSize - written)
		}
		data, err := decryptRegion(f, hdr, it.itemDataOff+written, chunk)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, io.ErrUnexpectedEOF
		}
		buf = append(buf, data...)
		written += uint64(len(data))
	}
	return buf, nil
}

// isPathPKG returns true when any item name starts with "../" or "..\",
// indicating a path-traversal PKG. Mirrors PyKG's is_path_pkg().
func isPathPKG(names []string) bool {
	for _, n := range names {
		if strings.HasPrefix(n, "../") || strings.HasPrefix(n, `..\ `) {
			return true
		}
		// Also catch Windows-style separators: "..\" (backslash without trailing space).
		if len(n) >= 3 && n[0] == '.' && n[1] == '.' && n[2] == '\\' {
			return true
		}
	}
	return false
}

// resolvePathPKGDest safely resolves a path-traversal item name into an
// absolute destination path. Returns "" for any name that would escape
// destRoot. Mirrors PyKG's resolve_path_pkg_dest().
func resolvePathPKGDest(rawName, destRoot string) string {
	name := strings.ReplaceAll(rawName, "\\", "/")
	parts := strings.Split(name, "/")

	resolved := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if p == ".." {
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
		} else if p != "." {
			resolved = append(resolved, p)
		}
	}

	if len(resolved) == 0 {
		return ""
	}

	// Build the candidate path.
	elems := make([]string, len(resolved)+1)
	elems[0] = destRoot
	copy(elems[1:], resolved)
	candidate := filepath.Join(elems...)

	if !pathWithin(destRoot, candidate) {
		return ""
	}

	return candidate
}

// resolveNormalPKGDest confines ordinary PKG entries to
// destRoot/<TITLE_ID>. Malformed SFO title IDs and embedded traversal segments
// must never redirect extraction outside destRoot.
func resolveNormalPKGDest(rawName, destRoot, titleID string) string {
	name := strings.ReplaceAll(rawName, "\\", "/")
	name = strings.TrimLeft(name, "/")
	if name == "" || strings.Contains(name, "\x00") {
		return ""
	}

	titleRoot := filepath.Join(destRoot, titleID)
	if !pathWithin(destRoot, titleRoot) {
		return ""
	}
	candidate := filepath.Join(titleRoot, filepath.FromSlash(name))
	if !pathWithin(titleRoot, candidate) {
		return ""
	}
	return candidate
}

func pathWithin(root, candidate string) bool {
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	// filepath.Rel returns an error or a ".." prefix if abs is outside root.
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

// DiscoveredPKG carries the metadata about a single PKG found on disk,
// used by OrderForBatchInstall to establish the safe extraction sequence.
type DiscoveredPKG struct {
	Path    string
	TitleID string
	AppVer  string
	Title   string
}

// pkgEntry pairs a DiscoveredPKG with its parsed version tuple for sorting.
type pkgEntry struct {
	pkg DiscoveredPKG
	ver [2]int
}

// OrderForBatchInstall groups PKGs by TITLE_ID and sorts each group by
// version ascending, then returns them as groups in TITLE_ID order.
//
// This mirrors the ordering logic in PyKG's worker_fn:
//
//	groups = defaultdict(list)
//	for each pkg: groups[tid].append(...)
//	for tid in groups: groups[tid].sort(key=lambda x: x[2])   # sort by parsed version
//	for tid in sorted(groups.keys()):
//	    for pkg in groups[tid]: ordered_pkgs.append(pkg)
//
// The "skip remaining versions of a title if any earlier one failed" invariant
// is load-bearing: lower versions must be installed before higher ones.
// Callers should iterate each inner slice in order and skip the entire group
// on the first failure — that is the correct PS3 install ordering.
func OrderForBatchInstall(pkgs []DiscoveredPKG) [][]DiscoveredPKG {
	groups := make(map[string][]pkgEntry)
	for _, p := range pkgs {
		groups[p.TitleID] = append(groups[p.TitleID], pkgEntry{pkg: p, ver: parseVersion(p.AppVer)})
	}

	// Collect and sort title IDs (matches Python's sorted(groups.keys())).
	tids := make([]string, 0, len(groups))
	for tid := range groups {
		tids = append(tids, tid)
	}
	sortStrings(tids)

	result := make([][]DiscoveredPKG, 0, len(tids))
	for _, tid := range tids {
		grp := groups[tid]
		// Sort by version ascending (matches Python's groups[tid].sort(key=lambda x: x[2])).
		sortPkgEntries(grp)
		out := make([]DiscoveredPKG, len(grp))
		for i, e := range grp {
			out[i] = e.pkg
		}
		result = append(result, out)
	}
	return result
}

// sortPkgEntries sorts a slice of pkgEntry by version ascending (insertion sort).
func sortPkgEntries(grp []pkgEntry) {
	for i := 1; i < len(grp); i++ {
		for j := i; j > 0; j-- {
			a, b := grp[j-1].ver, grp[j].ver
			if a[0] > b[0] || (a[0] == b[0] && a[1] > b[1]) {
				grp[j-1], grp[j] = grp[j], grp[j-1]
			} else {
				break
			}
		}
	}
}

// DiscoverPKGs walks `root` and discovers all *.pkg files, reading their
// headers + PARAM.SFO to extract TitleID/AppVer/Title.
//
// Each file is opened once and only its (small) header + PARAM.SFO item are
// decrypted — it is never slurped into memory, so this is safe for multi-GB
// PKGs. Files that are not valid PS3 NPDRM PKGs are skipped with a per-file
// error surfaced to the caller via the returned aggregate error, rather than
// aborting the whole walk (one bad/unrelated .pkg shouldn't hide the rest).
func DiscoverPKGs(root string) ([]DiscoveredPKG, error) {
	var out []DiscoveredPKG
	var skipped []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".pkg") {
			return nil
		}

		info2, perr := probeSFO(path)
		if perr != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", path, perr))
			return nil
		}

		out = append(out, DiscoveredPKG{
			Path:    path,
			TitleID: info2.TitleID,
			AppVer:  info2.AppVer,
			Title:   info2.Title,
		})
		return nil
	})

	if err != nil {
		return out, err
	}
	// Don't fail the whole discovery if some .pkg files were unreadable, but
	// surface it so the caller can log it.
	if len(skipped) > 0 {
		return out, fmt.Errorf("skipped %d unreadable pkg(s): %s", len(skipped), strings.Join(skipped, "; "))
	}
	return out, nil
}

// probeSFO opens a PS3 NPDRM PKG and returns just the PARAM.SFO metadata,
// reusing the same header/decrypt/findSFO path as Extract (so it is correct
// for real PS3 PKGs). It reads only the header + PARAM.SFO item, not the
// whole file.
func probeSFO(pkgPath string) (*SFOInfo, error) {
	f, err := os.Open(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	hdr, err := ReadHeader(f)
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Decrypt just the item table to locate PARAM.SFO, same as Extract does.
	tableRaw, err := decryptRegion(f, hdr, 0, uint32(hdr.itemCount)*itemRecordSize)
	if err != nil {
		return nil, fmt.Errorf("decrypt item table: %w", err)
	}
	items := make([]pkgItem, hdr.itemCount)
	for i := uint32(0); i < hdr.itemCount; i++ {
		rec := tableRaw[i*itemRecordSize : (i+1)*itemRecordSize]
		items[i] = pkgItem{
			nameOff:      binary.BigEndian.Uint32(rec[0:4]),
			nameSize:     binary.BigEndian.Uint32(rec[4:8]),
			itemDataOff:  binary.BigEndian.Uint64(rec[8:16]),
			itemDataSize: binary.BigEndian.Uint64(rec[16:24]),
			flags:        binary.BigEndian.Uint32(rec[24:28]),
		}
	}

	sfo, err := findSFO(f, hdr, items)
	if err != nil {
		return nil, fmt.Errorf("find PARAM.SFO: %w", err)
	}
	return sfo, nil
}

// parseVersion converts a version string like "01.05" into a comparable [2]int.
// Mirrors PyKG's parse_version().
func parseVersion(v string) [2]int {
	v = strings.TrimSpace(v)
	parts := strings.SplitN(v, ".", 2)
	var out [2]int
	for i, p := range parts {
		if i >= 2 {
			break
		}
		n := 0
		for _, ch := range p {
			if ch >= '0' && ch <= '9' {
				n = n*10 + int(ch-'0')
			} else {
				break
			}
		}
		out[i] = n
	}
	return out
}

// sortStrings sorts a string slice in place (ascending), using a simple
// insertion sort — kept stdlib-only.
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
