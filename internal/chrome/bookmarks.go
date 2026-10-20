// Package chrome reads, validates and writes Chromium's Bookmarks JSON file.
package chrome

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"arcm/internal/model"
)

var rootKeys = []string{"bookmark_bar", "other", "synced"}

// Checksum implements BookmarkCodec's checksum: for every node in pre-order,
// hash id, UTF-16LE title, the literal "url"/"folder", then the URL for links.
func Checksum(roots map[string]any) string {
	h := md5.New()
	for _, key := range rootKeys {
		if r := asMap(roots[key]); r != nil {
			writeChecksumNode(h, r)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeChecksumNode(h hash.Hash, n map[string]any) {
	io.WriteString(h, asString(n["id"]))
	writeUTF16LE(h, asString(n["name"]))
	if asString(n["type"]) == "url" {
		io.WriteString(h, "url")
		io.WriteString(h, asString(n["url"]))
	} else {
		io.WriteString(h, "folder")
	}
	for _, c := range asSlice(n["children"]) {
		if m := asMap(c); m != nil {
			writeChecksumNode(h, m)
		}
	}
}

func writeUTF16LE(h io.Writer, s string) {
	u := utf16.Encode([]rune(s))
	buf := make([]byte, len(u)*2)
	for i, v := range u {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	h.Write(buf)
}

// File is a Chromium Bookmarks document. Data is kept as generic JSON so
// fields unknown to arcm survive a read/modify/write cycle.
type File struct {
	Path string
	Data map[string]any
}

func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if asMap(data["roots"]) == nil {
		return nil, fmt.Errorf("%s has no roots object", path)
	}
	return &File{Path: path, Data: data}, nil
}

func NewFile(path string) *File {
	now := nowChrome()
	return &File{
		Path: path,
		Data: map[string]any{
			"checksum": "",
			"version":  1,
			"roots": map[string]any{
				"bookmark_bar": map[string]any{
					"children": []any{}, "date_added": now, "date_last_used": "0", "date_modified": "0",
					"guid": newGUID(), "id": "1", "name": "Bookmarks bar", "type": "folder",
				},
				"other": map[string]any{
					"children": []any{}, "date_added": now, "date_last_used": "0", "date_modified": "0",
					"guid": newGUID(), "id": "2", "name": "Other bookmarks", "type": "folder",
				},
				"synced": map[string]any{
					"children": []any{}, "date_added": now, "date_last_used": "0", "date_modified": "0",
					"guid": newGUID(), "id": "3", "name": "Mobile bookmarks", "type": "folder",
				},
			},
		},
	}
}

func (f *File) Roots() map[string]any {
	return asMap(f.Data["roots"])
}

// Save recomputes the checksum, backs up an existing file and atomically
// replaces it. It returns the backup path (empty when there was no old file).
func (f *File) Save() (string, error) {
	roots := f.Roots()
	f.Data["checksum"] = Checksum(roots)
	body, err := json.MarshalIndent(f.Data, "", "  ")
	if err != nil {
		return "", err
	}
	backup := ""
	if _, err := os.Stat(f.Path); err == nil {
		old, err := os.ReadFile(f.Path)
		if err != nil {
			return "", err
		}
		backup = uniqueBackupPath(f.Path)
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return "", fmt.Errorf("backup: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return "", err
	}
	tmp := f.Path + ".arcm-tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		return "", err
	}
	return backup, nil
}

func uniqueBackupPath(path string) string {
	base := fmt.Sprintf("%s.arcm-backup-%s", path, time.Now().Format("20060102-150405.000000000"))
	out := base
	for i := 2; ; i++ {
		if _, err := os.Stat(out); os.IsNotExist(err) {
			return out
		}
		out = fmt.Sprintf("%s-%d", base, i)
	}
}

type ImportOptions struct {
	Nodes      []*model.Node
	ProfileDir string
	DryRun     bool
	Force      bool
	// BrowserName/ProcessName identify the Chromium target for messages and
	// the "browser is running" guard. Empty values mean Chrome.
	BrowserName string
	ProcessName string
}

type ImportResult struct {
	Path     string
	Backup   string
	Checksum string
	State    string
	Links    int
	Folders  int
	Removed  int
	Skipped  int
}

const stateName = ".arcm-state.json"

type stateFile struct {
	Version int         `json:"version"`
	Nodes   []stateNode `json:"nodes"`
}

type stateNode struct {
	GUID  string `json:"guid"`
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Import adds Nodes to the target Chromium browser's bookmark bar. Imported
// items are placed directly in the bar; the previous arcm import is removed
// using a per-profile state file. Existing browser bookmarks are preserved.
func Import(opts ImportOptions) (*ImportResult, error) {
	if opts.ProfileDir == "" {
		return nil, errors.New("profile dir is required")
	}
	browserName := strings.TrimSpace(opts.BrowserName)
	if browserName == "" {
		browserName = "Chrome"
	}
	processName := strings.TrimSpace(opts.ProcessName)
	if processName == "" {
		processName = "Google Chrome"
	}
	if !opts.DryRun && !opts.Force && processRunning(processName) {
		return nil, fmt.Errorf("%s is running; quit it first or pass --force", browserName)
	}
	path := filepath.Join(opts.ProfileDir, "Bookmarks")
	f, err := Load(path)
	createdFile := false
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		f = NewFile(path)
		createdFile = true
	}
	roots := f.Roots()
	bar := asMap(roots["bookmark_bar"])
	if bar == nil {
		return nil, errors.New("bookmark_bar root is missing")
	}

	nodes := model.FoldersFirst(opts.Nodes)
	statePath := filepath.Join(opts.ProfileDir, stateName)
	managed, err := loadState(statePath)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{Path: path}
	var createdNodes []map[string]any
	if len(managed) > 0 {
		res.Removed += removeManaged(bar, managed)
	}

	maxID := maxNodeID(roots)
	nextID := maxID + 1

	// Existing bookmark-bar URLs, including nested folders, are the dedup
	// source. Remove arcm's previous nodes first so a repeated import is not
	// mistaken for a fully duplicated one.
	seen := collectURLs(asSlice(bar["children"]))
	kept, skipped := filterNewNodes(nodes, seen)
	res.Skipped = skipped
	created := make([]map[string]any, 0, len(kept))
	children := asSlice(bar["children"])
	for _, n := range kept {
		m := toChrome(n, &nextID)
		children = append(children, m)
		created = append(created, m)
	}
	children = foldersFirstChrome(children)
	bar["children"] = children
	bar["date_modified"] = nowChrome()
	createdNodes = created

	links, folders := model.Count(kept)
	res.Links, res.Folders = links, folders

	if createdFile {
		// Ensure the parent directory exists before a dry run as well.
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	roots = f.Roots()
	res.Checksum = Checksum(roots)
	if opts.DryRun {
		return res, nil
	}
	backup, err := f.Save()
	if err != nil {
		return nil, err
	}
	res.Backup = backup
	res.Checksum = asString(f.Data["checksum"])
	if err := saveState(statePath, createdNodes); err != nil {
		return nil, fmt.Errorf("bookmarks saved, but writing %s failed: %w", statePath, err)
	}
	res.State = statePath
	return res, nil
}

func collectURLs(nodes []any) map[string]bool {
	seen := map[string]bool{}
	var walk func([]any)
	walk = func(list []any) {
		for _, v := range list {
			m := asMap(v)
			if m == nil {
				continue
			}
			if asString(m["type"]) == "url" {
				if url := strings.TrimSpace(asString(m["url"])); url != "" {
					seen[url] = true
				}
				continue
			}
			walk(asSlice(m["children"]))
		}
	}
	walk(nodes)
	return seen
}

// filterNewNodes drops links whose URL already exists in the target bookmark
// bar and folders that end up with no new descendants. Empty source folders
// are kept; a folder that only contained duplicates is skipped entirely.
func filterNewNodes(nodes []*model.Node, seen map[string]bool) ([]*model.Node, int) {
	out := make([]*model.Node, 0, len(nodes))
	skipped := 0
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if n.Kind == model.Link {
			url := strings.TrimSpace(n.URL)
			if url == "" || seen[url] {
				skipped++
				continue
			}
			seen[url] = true
			clone := *n
			clone.URL = url
			out = append(out, &clone)
			continue
		}
		kept, childSkipped := filterNewNodes(n.Children, seen)
		skipped += childSkipped
		if len(n.Children) > 0 && len(kept) == 0 {
			skipped++
			continue
		}
		clone := *n
		clone.Children = kept
		out = append(out, &clone)
	}
	return out, skipped
}

// foldersFirstChrome is a stable top-level partition of bookmark-bar nodes.
func foldersFirstChrome(nodes []any) []any {
	out := make([]any, 0, len(nodes))
	for _, v := range nodes {
		if m := asMap(v); m != nil && asString(m["type"]) == "folder" {
			out = append(out, v)
		}
	}
	for _, v := range nodes {
		if m := asMap(v); m != nil && asString(m["type"]) != "folder" {
			out = append(out, v)
		}
	}
	return out
}

func removeManaged(bar map[string]any, managed map[string]bool) int {
	children, removed := filterChildren(asSlice(bar["children"]), managed)
	bar["children"] = children
	return removed
}

func filterChildren(nodes []any, remove map[string]bool) ([]any, int) {
	out := make([]any, 0, len(nodes))
	removed := 0
	for _, v := range nodes {
		m := asMap(v)
		if m == nil {
			continue
		}
		if remove[asString(m["guid"])] {
			removed++
			continue
		}
		if kids := asSlice(m["children"]); kids != nil {
			kept, r := filterChildren(kids, remove)
			if r > 0 {
				m["children"] = kept
				removed += r
			}
		}
		out = append(out, m)
	}
	return out, removed
}

func loadState(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	var st stateFile
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if st.Version != 1 {
		return nil, fmt.Errorf("unsupported arcm state version %d in %s", st.Version, path)
	}
	m := make(map[string]bool, len(st.Nodes))
	for _, n := range st.Nodes {
		if n.GUID != "" {
			m[n.GUID] = true
		}
	}
	return m, nil
}

func saveState(path string, nodes []map[string]any) error {
	st := stateFile{Version: 1}
	var walk func(map[string]any)
	walk = func(m map[string]any) {
		if m == nil {
			return
		}
		st.Nodes = append(st.Nodes, stateNode{
			GUID: asString(m["guid"]), Type: asString(m["type"]),
			Title: asString(m["name"]), URL: asString(m["url"]),
		})
		for _, c := range asSlice(m["children"]) {
			walk(asMap(c))
		}
	}
	for _, m := range nodes {
		walk(m)
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newFolder(name string, nextID *int64) map[string]any {
	id := *nextID
	*nextID++
	now := nowChrome()
	return map[string]any{
		"children": []any{}, "date_added": now, "date_last_used": "0", "date_modified": now,
		"guid": newGUID(), "id": strconv.FormatInt(id, 10), "name": name, "type": "folder",
	}
}

func toChrome(n *model.Node, nextID *int64) map[string]any {
	if n.Kind == model.Link {
		id := *nextID
		*nextID++
		return map[string]any{
			"date_added": chromeDate(n.Created), "date_last_used": "0",
			"guid": newGUID(), "id": strconv.FormatInt(id, 10), "name": n.Title,
			"type": "url", "url": n.URL,
		}
	}
	folder := newFolder(n.Title, nextID)
	children := make([]any, 0, len(n.Children))
	for _, c := range n.Children {
		children = append(children, toChrome(c, nextID))
	}
	folder["children"] = children
	return folder
}

func maxNodeID(roots map[string]any) int64 {
	var max int64
	var walk func(any)
	walk = func(v any) {
		m := asMap(v)
		if m == nil {
			return
		}
		if id, err := strconv.ParseInt(asString(m["id"]), 10, 64); err == nil && id > max {
			max = id
		}
		for _, c := range asSlice(m["children"]) {
			walk(c)
		}
	}
	for _, key := range rootKeys {
		walk(roots[key])
	}
	return max
}

func chromeDate(appleSeconds float64) string {
	if appleSeconds <= 0 {
		appleSeconds = float64(time.Now().Unix()) - 978307200
	}
	return strconv.FormatInt(int64(math.Round(appleSeconds*1_000_000))+12622780800*1_000_000, 10)
}

func nowChrome() string {
	return chromeDate(float64(time.Now().Unix()) - 978307200)
}

func newGUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func processRunning(name string) bool {
	out, err := exec.Command("pgrep", "-x", name).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
