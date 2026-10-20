package arc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arcm/internal/model"
)

// Space is one Arc sidebar space.
type Space struct {
	ID               string
	PinnedContainerID string
	Pinned           []*model.Node
	TopApps          []*model.Node
}

// Store is a read-only snapshot of Arc's StorableSidebar.json.
type Store struct {
	Path     string
	Size     int64
	Mtime    time.Time
	Version  int
	Spaces   []Space
	TopApps  []*model.Node
	Warnings []string
	Easels   int
}

func DefaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Arc"), nil
}

func DataDir() (string, error) {
	if v := os.Getenv("ARC_DATA_DIR"); v != "" {
		return v, nil
	}
	return DefaultDataDir()
}

// Read parses StorableSidebar.json. dir defaults to ARC_DATA_DIR or the
// standard macOS Arc data directory.
func Read(dir string) (*Store, error) {
	if dir == "" {
		var err error
		dir, err = DataDir()
		if err != nil {
			return nil, err
		}
	}
	path := filepath.Join(dir, "StorableSidebar.json")
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("Arc sidebar: %w", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Arc sidebar: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("parse Arc sidebar: %w", err)
	}
	version := asInt(root["version"])
	if version != 1 {
		return nil, fmt.Errorf("unsupported StorableSidebar version %d", version)
	}
	s := &Store{Path: path, Size: info.Size(), Mtime: info.ModTime(), Version: version}

	sidebar := asMap(root["sidebar"])
	var profile map[string]any
	for _, c := range asSlice(sidebar["containers"]) {
		m := asMap(c)
		if m == nil {
			continue
		}
		if _, ok := m["spaces"]; ok {
			profile = m
			break
		}
	}
	if profile == nil {
		return nil, errors.New("Arc profile container not found")
	}
	items := pairMap(profile["items"])
	topApps := s.readTopApps(profile, items)
	s.TopApps = topApps
	spaces := asSlice(profile["spaces"])
	for i := 0; i+1 < len(spaces); i += 2 {
		id := asString(spaces[i])
		sp := asMap(spaces[i+1])
		if sp == nil {
			continue
		}
		space := Space{ID: id, TopApps: topApps}
		for _, c := range asSlice(sp["containerIDs"]) {
			cid := asString(c)
			if strings.HasSuffix(cid, "PinnedContainerID") {
				space.PinnedContainerID = cid
			}
		}
		if pc := items[space.PinnedContainerID]; pc != nil {
			space.Pinned = s.buildChildren(pc, items, map[string]bool{})
		}
		s.Spaces = append(s.Spaces, space)
	}
	if len(s.Spaces) == 0 {
		return nil, errors.New("no Arc space found")
	}
	return s, nil
}

func (s *Store) readTopApps(profile map[string]any, items map[string]map[string]any) []*model.Node {
	var out []*model.Node
	seen := map[string]bool{}
	for _, raw := range asSlice(profile["topAppsContainerIDs"]) {
		id := asString(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cont := items[id]
		if cont == nil {
			continue
		}
		if asMap(cont["data"])["itemContainer"] == nil {
			continue
		}
		out = append(out, s.buildChildren(cont, items, map[string]bool{})...)
	}
	return out
}

// SelectSpace returns one space by ID. When id is empty it succeeds only if
// the sidebar has exactly one space, so an omitted --space can never silently
// drop another space.
func (s *Store) SelectSpace(id string) (*Space, error) {
	ids := make([]string, 0, len(s.Spaces))
	for i := range s.Spaces {
		ids = append(ids, s.Spaces[i].ID)
		if id != "" && s.Spaces[i].ID == id {
			return &s.Spaces[i], nil
		}
	}
	if id != "" {
		return nil, fmt.Errorf("Arc space %q not found; available: %s", id, strings.Join(ids, ", "))
	}
	if len(s.Spaces) == 1 {
		return &s.Spaces[0], nil
	}
	return nil, fmt.Errorf("Arc has %d spaces; pass --space <id>; available: %s", len(s.Spaces), strings.Join(ids, ", "))
}

func (s *Store) buildChildren(parent map[string]any, items map[string]map[string]any, seen map[string]bool) []*model.Node {
	var out []*model.Node
	for _, raw := range asSlice(parent["childrenIds"]) {
		if n := s.buildNode(asString(raw), items, seen); n != nil {
			out = append(out, n)
		}
	}
	return out
}

func (s *Store) buildNode(id string, items map[string]map[string]any, seen map[string]bool) *model.Node {
	if id == "" {
		return nil
	}
	if seen[id] {
		s.Warnings = append(s.Warnings, "cycle detected at "+id)
		return nil
	}
	obj := items[id]
	if obj == nil {
		s.Warnings = append(s.Warnings, "missing item "+id)
		return nil
	}
	seen[id] = true
	defer delete(seen, id)

	data := asMap(obj["data"])
	if data == nil {
		s.Warnings = append(s.Warnings, "item without data "+id)
		return nil
	}
	if tab := asMap(data["tab"]); tab != nil {
		title := asString(tab["savedTitle"])
		if title == "" {
			title = asString(obj["title"])
		}
		url := asString(tab["savedURL"])
		if title == "" {
			title = url
		}
		return &model.Node{Kind: model.Link, Title: title, URL: url, Created: asFloat(obj["createdAt"])}
	}
	if _, ok := data["list"]; ok {
		title := asString(obj["title"])
		if title == "" {
			title = "(untitled folder)"
		}
		n := &model.Node{Kind: model.Folder, Title: title, Created: asFloat(obj["createdAt"])}
		n.Children = s.buildChildren(obj, items, seen)
		return n
	}
	if _, ok := data["easel"]; ok {
		s.Easels++
		return nil
	}
	if _, ok := data["itemContainer"]; ok {
		return nil
	}
	s.Warnings = append(s.Warnings, "unknown Arc item type "+id)
	return nil
}

func pairMap(v any) map[string]map[string]any {
	out := map[string]map[string]any{}
	arr := asSlice(v)
	for i := 0; i+1 < len(arr); i += 2 {
		id := asString(arr[i])
		obj := asMap(arr[i+1])
		if id != "" && obj != nil {
			out[id] = obj
		}
	}
	return out
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

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

func asInt(v any) int {
	return int(asFloat(v))
}
