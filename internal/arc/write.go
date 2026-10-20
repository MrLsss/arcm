package arc

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"arcm/internal/model"
)

// ReplaceOptions controls a destructive Arc sidebar rewrite.
type ReplaceOptions struct {
	Dir              string
	SpaceID          string
	Nodes            []*model.Node
	DryRun           bool
	Force            bool
	Restart          bool
	Append           bool
	Incremental      bool // add only URLs not already present in pinned or Top Apps
	SkipProcessCheck bool // for tests
}

// ReplaceResult reports what changed.
type ReplaceResult struct {
	Path           string
	Backup         string
	RemovedTabs    int
	RemovedFolders int
	RemovedEasels  int
	RemovedOther   int
	AddedLinks     int
	AddedFolders   int
	TopApps        int
	ArcRunning     bool
	Restarted      bool
}

// Replace rewrites the pinned sidebar: by default it removes every pinned
// item (tabs, folders, easels) and inserts Nodes. Top Apps are never touched.
func Replace(opts ReplaceOptions) (*ReplaceResult, error) {
	dir := opts.Dir
	if dir == "" {
		var err error
		dir, err = DataDir()
		if err != nil {
			return nil, err
		}
	}
	path := filepath.Join(dir, "StorableSidebar.json")
	running := false
	if !opts.SkipProcessCheck {
		running = processRunning("Arc")
	}
	if running && !opts.DryRun && !opts.Force && !opts.Restart {
		return nil, errors.New("Arc is running; quit Arc first or use --restart")
	}
	if running && opts.Restart && !opts.DryRun {
		if err := quitArc(); err != nil {
			return nil, err
		}
		running = false
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("Arc sidebar: %w", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Arc sidebar: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(original, &root); err != nil {
		return nil, fmt.Errorf("parse Arc sidebar: %w", err)
	}
	if asInt(root["version"]) != 1 {
		return nil, fmt.Errorf("unsupported StorableSidebar version %d", asInt(root["version"]))
	}
	profile := profileContainer(root)
	if profile == nil {
		return nil, errors.New("Arc profile container not found")
	}
	items := pairMap(profile["items"])
	spaces := asSlice(profile["spaces"])
	space, err := selectSpaceFromRaw(spaces, opts.SpaceID)
	if err != nil {
		return nil, err
	}
	var pinnedID string
	for _, raw := range asSlice(space["containerIDs"]) {
		if id := asString(raw); strings.HasSuffix(id, "PinnedContainerID") {
			pinnedID = id
		}
	}
	pinned := items[pinnedID]
	if pinned == nil {
		return nil, errors.New("Arc pinned container not found")
	}

	topApps, err := countTopApps(profile, items)
	if err != nil {
		return nil, err
	}
	res := &ReplaceResult{Path: path, TopApps: topApps, ArcRunning: running}

	startChildren := asSlice(pinned["childrenIds"])
	if !opts.Append && !opts.Incremental {
		startChildren = nil
		visiting := map[string]bool{}
		var walk func(string) error
		walk = func(id string) error {
			if visiting[id] {
				return fmt.Errorf("cycle detected in Arc sidebar at %s", id)
			}
			obj := items[id]
			if obj == nil {
				return nil
			}
			visiting[id] = true
			for _, child := range asSlice(obj["childrenIds"]) {
				if err := walk(asString(child)); err != nil {
					return err
				}
			}
			delete(visiting, id)
			data := asMap(obj["data"])
			switch {
			case data["tab"] != nil:
				res.RemovedTabs++
			case data["list"] != nil:
				res.RemovedFolders++
			case data["easel"] != nil:
				res.RemovedEasels++
			default:
				res.RemovedOther++
			}
			delete(items, id)
			return nil
		}
		for _, child := range asSlice(pinned["childrenIds"]) {
			if err := walk(asString(child)); err != nil {
				return nil, err
			}
		}
	}

	deviceID := asString(pinned["originatingDevice"])
	if deviceID == "" {
		for _, obj := range items {
			if v := asString(obj["originatingDevice"]); v != "" {
				deviceID = v
				break
			}
		}
	}
	if deviceID == "" {
		deviceID = newUUID()
	}
	now := appleNow()

	added := make([]any, 0)
	newChildIDs := make([]any, 0)
	for _, child := range startChildren {
		newChildIDs = append(newChildIDs, child)
	}
	var addNode func(*model.Node, string) string
	addNode = func(n *model.Node, parentID string) string {
		if n == nil {
			return ""
		}
		if n.Kind == model.Link {
			url := strings.TrimSpace(n.URL)
			if url == "" {
				return ""
			}
			title := strings.TrimSpace(n.Title)
			if title == "" {
				title = url
			}
			id := newUUID()
			obj := map[string]any{
				"id":                id,
				"data":              map[string]any{"tab": map[string]any{"savedURL": url, "savedTitle": title, "savedMuteStatus": "allowAudio", "timeLastActiveAt": now}},
				"createdAt":         createdAt(n, now),
				"isUnread":          false,
				"parentID":          parentID,
				"childrenIds":       []any{},
				"originatingDevice": deviceID,
				"title":             title,
			}
			items[id] = obj
			added = append(added, id, obj)
			res.AddedLinks++
			return id
		}
		title := strings.TrimSpace(n.Title)
		if title == "" {
			title = "(untitled folder)"
		}
		id := newUUID()
		obj := map[string]any{
			"id":                id,
			"data":              map[string]any{"list": map[string]any{}},
			"createdAt":         createdAt(n, now),
			"isUnread":          false,
			"parentID":          parentID,
			"childrenIds":       []any{},
			"originatingDevice": deviceID,
			"title":             title,
		}
		items[id] = obj
		added = append(added, id, obj)
		res.AddedFolders++
		var kids []any
		for _, child := range n.Children {
			if cid := addNode(child, id); cid != "" {
				kids = append(kids, cid)
			}
		}
		obj["childrenIds"] = kids
		return id
	}
	seen := map[string]bool{}
	idx := pinnedIndex{}
	if opts.Incremental {
		var err error
		idx, err = indexPinned(profile, items, pinned)
		if err != nil {
			return nil, err
		}
		for u := range idx.urls {
			seen[u] = true
		}
	}
	var addIncremental func(*model.Node, string) string
	addIncremental = func(n *model.Node, parentID string) string {
		if n == nil {
			return ""
		}
		if n.Kind == model.Link {
			url := strings.TrimSpace(n.URL)
			if url == "" || seen[url] {
				return ""
			}
			id := addNode(n, parentID)
			if id != "" {
				seen[url] = true
			}
			return id
		}
		title := strings.TrimSpace(n.Title)
		if title == "" {
			title = "(untitled folder)"
		}
		if parentID == pinnedID {
			if fid := idx.takeTopFolder(title); fid != "" {
				if !hasImportable(n, seen) {
					return ""
				}
				obj := items[fid]
				kids := asSlice(obj["childrenIds"])
				for _, child := range n.Children {
					if cid := addIncremental(child, fid); cid != "" {
						kids = append(kids, cid)
					}
				}
				obj["childrenIds"] = kids
				return ""
			}
		}
		if !hasImportable(n, seen) {
			return ""
		}
		id := addNode(&model.Node{Kind: model.Folder, Title: title, Created: n.Created}, parentID)
		if id == "" {
			return ""
		}
		obj := items[id]
		var kids []any
		for _, child := range n.Children {
			if cid := addIncremental(child, id); cid != "" {
				kids = append(kids, cid)
			}
		}
		obj["childrenIds"] = kids
		return id
	}
	for _, n := range opts.Nodes {
		var id string
		if opts.Incremental {
			id = addIncremental(n, pinnedID)
		} else {
			id = addNode(n, pinnedID)
		}
		if id != "" {
			newChildIDs = append(newChildIDs, id)
		}
	}
	newChildIDs = foldersFirstArc(items, newChildIDs)
	pinned["childrenIds"] = newChildIDs

	oldPairs := asSlice(profile["items"])
	newPairs := make([]any, 0, len(oldPairs)+len(added))
	for i := 0; i+1 < len(oldPairs); i += 2 {
		id := asString(oldPairs[i])
		if obj, ok := items[id]; ok {
			newPairs = append(newPairs, id, obj)
		}
	}
	newPairs = append(newPairs, added...)
	profile["items"] = newPairs

	if opts.DryRun {
		return res, nil
	}

	backup := uniqueBackupPath(path)
	if err := os.WriteFile(backup, original, info.Mode().Perm()); err != nil {
		return nil, fmt.Errorf("write backup: %w", err)
	}
	res.Backup = backup

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Arc sidebar: %w", err)
	}
	out = append(out, '\n')
	tmp := path + ".arcm-tmp"
	if err := os.WriteFile(tmp, out, info.Mode().Perm()); err != nil {
		return nil, fmt.Errorf("write temp sidebar: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, fmt.Errorf("replace Arc sidebar: %w", err)
	}

	if opts.Restart {
		if err := exec.Command("open", "-a", "Arc").Run(); err != nil {
			return nil, fmt.Errorf("restart Arc: %w", err)
		}
		res.Restarted = true
	}
	return res, nil
}

func selectSpaceFromRaw(spaces []any, id string) (map[string]any, error) {
	ids := make([]string, 0, len(spaces)/2)
	var selected map[string]any
	selected = nil
	for i := 0; i+1 < len(spaces); i += 2 {
		spaceID := asString(spaces[i])
		obj := asMap(spaces[i+1])
		if spaceID == "" || obj == nil {
			continue
		}
		ids = append(ids, spaceID)
		if id == "" {
			selected = obj
		} else if spaceID == id {
			selected = obj
		}
	}
	if id != "" {
		if selected == nil {
			return nil, fmt.Errorf("Arc space %q not found; available: %s", id, strings.Join(ids, ", "))
		}
		return selected, nil
	}
	if len(ids) == 1 {
		return selected, nil
	}
	return nil, fmt.Errorf("Arc has %d spaces; pass --space <id>; available: %s", len(ids), strings.Join(ids, ", "))
}

func profileContainer(root map[string]any) map[string]any {
	sidebar := asMap(root["sidebar"])
	if sidebar == nil {
		return nil
	}
	for _, raw := range asSlice(sidebar["containers"]) {
		m := asMap(raw)
		if m == nil {
			continue
		}
		if _, ok := m["spaces"]; ok {
			return m
		}
	}
	return nil
}

type pinnedIndex struct {
	urls           map[string]bool
	topFolders     map[string][]string
	usedTopFolders map[string]bool
}

func (idx *pinnedIndex) takeTopFolder(title string) string {
	for _, id := range idx.topFolders[title] {
		if !idx.usedTopFolders[id] {
			idx.usedTopFolders[id] = true
			return id
		}
	}
	return ""
}

func indexPinned(profile map[string]any, items map[string]map[string]any, pinned map[string]any) (pinnedIndex, error) {
	idx := pinnedIndex{
		urls:           map[string]bool{},
		topFolders:     map[string][]string{},
		usedTopFolders: map[string]bool{},
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var walk func(string) error
	walk = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("cycle detected in Arc sidebar at %s", id)
		}
		if visited[id] {
			return nil
		}
		obj := items[id]
		if obj == nil {
			return nil
		}
		visiting[id] = true
		data := asMap(obj["data"])
		if data["tab"] != nil {
			if url := strings.TrimSpace(asString(asMap(data["tab"])["savedURL"])); url != "" {
				idx.urls[url] = true
			}
			delete(visiting, id)
			visited[id] = true
			return nil
		}
		for _, child := range asSlice(obj["childrenIds"]) {
			if err := walk(asString(child)); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for _, raw := range asSlice(pinned["childrenIds"]) {
		id := asString(raw)
		obj := items[id]
		if obj == nil {
			continue
		}
		if asMap(obj["data"])["list"] != nil {
			if title := asString(obj["title"]); title != "" {
				idx.topFolders[title] = append(idx.topFolders[title], id)
			}
		}
		if err := walk(id); err != nil {
			return pinnedIndex{}, err
		}
	}
	// Top Apps are also "existing Arc bookmarks" for dedup purposes. A URL
	// pinned in Top Apps must never be imported into the sidebar.
	for _, raw := range asSlice(profile["topAppsContainerIDs"]) {
		id := asString(raw)
		if id == "" {
			continue
		}
		obj := items[id]
		if obj == nil || asMap(obj["data"])["itemContainer"] == nil {
			continue
		}
		if err := walk(id); err != nil {
			return pinnedIndex{}, err
		}
	}
	return idx, nil
}

// hasImportable reports whether the source folder contains a link not yet in
// seen, or an empty folder. A folder whose descendants are all duplicates is
// skipped as a whole.
func hasImportable(n *model.Node, seen map[string]bool) bool {
	if n == nil {
		return false
	}
	if n.Kind == model.Link {
		url := strings.TrimSpace(n.URL)
		return url != "" && !seen[url]
	}
	if len(n.Children) == 0 {
		return true
	}
	for _, child := range n.Children {
		if hasImportable(child, seen) {
			return true
		}
	}
	return false
}

// foldersFirstArc stably partitions pinned container children so folders come
// before links. Easels and unknown item types count as non-folders.
func foldersFirstArc(items map[string]map[string]any, ids []any) []any {
	out := make([]any, 0, len(ids))
	for _, raw := range ids {
		if obj := items[asString(raw)]; obj != nil && asMap(obj["data"])["list"] != nil {
			out = append(out, raw)
		}
	}
	for _, raw := range ids {
		if obj := items[asString(raw)]; obj != nil && asMap(obj["data"])["list"] == nil {
			out = append(out, raw)
		}
	}
	return out
}

func countTopApps(profile map[string]any, items map[string]map[string]any) (int, error) {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var walk func(string) (int, error)
	walk = func(id string) (int, error) {
		if visiting[id] {
			return 0, fmt.Errorf("cycle detected in Arc Top Apps at %s", id)
		}
		if visited[id] {
			return 0, nil
		}
		obj := items[id]
		if obj == nil {
			return 0, nil
		}
		visiting[id] = true
		data := asMap(obj["data"])
		if data["tab"] != nil {
			delete(visiting, id)
			visited[id] = true
			return 1, nil
		}
		n := 0
		for _, c := range asSlice(obj["childrenIds"]) {
			count, err := walk(asString(c))
			if err != nil {
				return 0, err
			}
			n += count
		}
		delete(visiting, id)
		visited[id] = true
		return n, nil
	}
	n := 0
	for _, raw := range asSlice(profile["topAppsContainerIDs"]) {
		id := asString(raw)
		if id == "" {
			continue
		}
		if obj := items[id]; obj != nil && asMap(obj["data"])["itemContainer"] != nil {
			count, err := walk(id)
			if err != nil {
				return 0, err
			}
			n += count
		}
	}
	return n, nil
}

func processRunning(name string) bool {
	return exec.Command("pgrep", "-x", name).Run() == nil
}

func quitArc() error {
	_ = exec.Command("osascript", "-e", `tell application "Arc" to quit`).Run()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning("Arc") {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("Arc did not quit within 20s; quit it manually and retry")
}

func uniqueBackupPath(path string) string {
	base := fmt.Sprintf("%s.arcm-backup-%s", path, time.Now().Format("20060102-150405"))
	out := base
	for i := 2; ; i++ {
		if _, err := os.Stat(out); os.IsNotExist(err) {
			return out
		}
		out = fmt.Sprintf("%s-%d", base, i)
	}
}

func createdAt(n *model.Node, fallback float64) float64 {
	if n != nil && n.Created > 0 {
		return n.Created
	}
	return fallback
}

func appleNow() float64 {
	return float64(time.Now().UnixNano())/1e9 - 978307200.0
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.ToUpper(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}
