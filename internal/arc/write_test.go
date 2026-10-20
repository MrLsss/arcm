package arc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"arcm/internal/model"
)

const fixture = `{
  "version": 1,
  "sidebar": {
    "containers": [
      {"global": {}},
      {
        "spaces": [
          "space-1",
          {
            "id": "space-1",
            "containerIDs": ["unpinned-1", "thebrowser.company.defaultPersonalSpacePinnedContainerID"],
            "customInfo": {}
          }
        ],
        "topAppsContainerIDs": [{"default": true}, "top-1"],
        "items": [
          "thebrowser.company.defaultPersonalSpacePinnedContainerID", {"id":"thebrowser.company.defaultPersonalSpacePinnedContainerID","data":{"itemContainer":{"containerType":{"spaceItems":{"_0":"space-1"}}}},"childrenIds":["old-tab","old-folder"],"originatingDevice":"device-1","title":null},
          "old-tab", {"id":"old-tab","data":{"tab":{"savedURL":"https://old.example/","savedTitle":"Old"}},"childrenIds":[],"createdAt":1,"parentID":"thebrowser.company.defaultPersonalSpacePinnedContainerID","title":"Old"},
          "old-folder", {"id":"old-folder","data":{"list":{}},"childrenIds":[],"createdAt":2,"parentID":"thebrowser.company.defaultPersonalSpacePinnedContainerID","title":"Old folder"},
          "top-1", {"id":"top-1","data":{"itemContainer":{"containerType":{"topApps":{"_0":{"default":true}}}}},"childrenIds":["app-1"],"title":null},
          "app-1", {"id":"app-1","data":{"tab":{"savedURL":"https://app.example/ ","savedTitle":"App"}},"childrenIds":[],"createdAt":3,"parentID":"top-1","title":"App"}
        ]
      }
    ]
  }
}`

func writeFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "StorableSidebar.json")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func TestReplaceClearsPinnedAndKeepsTopApps(t *testing.T) {
	dir, _ := writeFixture(t)
	nodes := []*model.Node{{
		Kind:  model.Folder,
		Title: "New folder",
		Children: []*model.Node{
			{Kind: model.Link, Title: "New link", URL: "https://new.example/"},
		},
	}}
	res, err := Replace(ReplaceOptions{Dir: dir, Nodes: nodes, SkipProcessCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.RemovedTabs != 1 || res.RemovedFolders != 1 || res.RemovedEasels != 0 {
		t.Fatalf("removed counts = %+v", res)
	}
	if res.TopApps != 1 || res.AddedLinks != 1 || res.AddedFolders != 1 {
		t.Fatalf("result = %+v", res)
	}
	if res.Backup == "" {
		t.Fatal("expected backup")
	}
	if _, err := os.Stat(res.Backup); err != nil {
		t.Fatalf("backup: %v", err)
	}

	store, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	pinnedLinks, pinnedFolders := model.Count(store.Spaces[0].Pinned)
	topLinks, _ := model.Count(store.Spaces[0].TopApps)
	if pinnedLinks != 1 || pinnedFolders != 1 {
		t.Fatalf("pinned = %d links, %d folders", pinnedLinks, pinnedFolders)
	}
	if topLinks != 1 {
		t.Fatalf("top apps = %d, want 1", topLinks)
	}

	b, err := os.ReadFile(filepath.Join(dir, "StorableSidebar.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	profile := profileContainer(root)
	if profile == nil {
		t.Fatal("profile missing")
	}
	items := pairMap(profile["items"])
	if items["old-tab"] != nil || items["old-folder"] != nil {
		t.Fatal("old pinned items were not removed")
	}
	if items["app-1"] == nil {
		t.Fatal("top app was removed")
	}
}

func TestReplaceDryRunDoesNotWrite(t *testing.T) {
	dir, path := writeFixture(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Replace(ReplaceOptions{Dir: dir, DryRun: true, SkipProcessCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Backup != "" {
		t.Fatal("dry run should not create a backup")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatal("dry run modified the sidebar")
	}
}

func TestReplaceIncrementalEmptyFolders(t *testing.T) {
	dir, _ := writeFixture(t)
	nodes := []*model.Node{
		{Kind: model.Folder, Title: "Old folder"},
		{Kind: model.Folder, Title: "New empty"},
	}
	res, err := Replace(ReplaceOptions{Dir: dir, Nodes: nodes, Incremental: true, SkipProcessCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.AddedFolders != 1 || res.AddedLinks != 0 {
		t.Fatalf("result = %+v, want 1 new empty folder", res)
	}
	store, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, n := range store.Spaces[0].Pinned {
		if n.Kind == model.Folder {
			counts[n.Title]++
		}
	}
	if counts["Old folder"] != 1 || counts["New empty"] != 1 {
		t.Fatalf("folders = %+v", counts)
	}
	res, err = Replace(ReplaceOptions{Dir: dir, Nodes: nodes, Incremental: true, SkipProcessCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.AddedFolders != 0 || res.AddedLinks != 0 {
		t.Fatalf("re-import added duplicates: %+v", res)
	}
}

func TestReplaceKeepsSameNamedTopFolders(t *testing.T) {
	dir, _ := writeFixture(t)
	nodes := []*model.Node{
		{Kind: model.Folder, Title: "Dup", Children: []*model.Node{
			{Kind: model.Link, Title: "A", URL: "https://a.example/"},
		}},
		{Kind: model.Folder, Title: "Dup", Children: []*model.Node{
			{Kind: model.Link, Title: "B", URL: "https://b.example/"},
		}},
	}
	res, err := Replace(ReplaceOptions{Dir: dir, Nodes: nodes, Incremental: true, SkipProcessCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.AddedFolders != 2 || res.AddedLinks != 2 {
		t.Fatalf("result = %+v, want 2 folders / 2 links", res)
	}
	store, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	dups := 0
	for _, n := range store.Spaces[0].Pinned {
		if n.Kind == model.Folder && n.Title == "Dup" {
			dups++
		}
	}
	if dups != 2 {
		t.Fatalf("Dup folders = %d, want 2", dups)
	}
	res, err = Replace(ReplaceOptions{Dir: dir, Nodes: nodes, Incremental: true, SkipProcessCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.AddedFolders != 0 || res.AddedLinks != 0 {
		t.Fatalf("re-import added duplicates: %+v", res)
	}
}

func TestReplaceKeepsSourceCreatedAt(t *testing.T) {
	dir, _ := writeFixture(t)
	const created = 1700000000.5
	nodes := []*model.Node{{Kind: model.Link, Title: "Dated", URL: "https://dated.example/", Created: created}}
	if _, err := Replace(ReplaceOptions{Dir: dir, Nodes: nodes, Incremental: true, SkipProcessCheck: true}); err != nil {
		t.Fatal(err)
	}
	store, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range store.Spaces[0].Pinned {
		if n.URL == "https://dated.example/" {
			if n.Created != created {
				t.Fatalf("created = %v, want %v", n.Created, created)
			}
			return
		}
	}
	t.Fatal("dated link not found")
}

func TestReplaceSelectsSpace(t *testing.T) {
	dir, path := writeFixture(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	profile := profileContainer(root)
	spaces := asSlice(profile["spaces"])
	spaces = append(spaces,
		"space-2",
		map[string]any{"id": "space-2", "containerIDs": []any{"space-2PinnedContainerID"}, "customInfo": map[string]any{}},
	)
	profile["spaces"] = spaces
	pairs := asSlice(profile["items"])
	pairs = append(pairs,
		"space-2PinnedContainerID", map[string]any{"id": "space-2PinnedContainerID", "data": map[string]any{"itemContainer": map[string]any{}}, "childrenIds": []any{}},
	)
	profile["items"] = pairs
	b, err = json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Replace(ReplaceOptions{Dir: dir, SkipProcessCheck: true}); err == nil {
		t.Fatal("multiple spaces were accepted without --space")
	}
	nodes := []*model.Node{{Kind: model.Link, Title: "Second", URL: "https://second.example/"}}
	res, err := Replace(ReplaceOptions{Dir: dir, SpaceID: "space-2", Nodes: nodes, SkipProcessCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.AddedLinks != 1 {
		t.Fatalf("result = %+v", res)
	}
	store, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range store.Spaces {
		links, _ := model.Count(sp.Pinned)
		switch sp.ID {
		case "space-1":
			if links != 1 {
				t.Fatalf("space-1 pinned = %d, want untouched old tab", links)
			}
		case "space-2":
			if links != 1 {
				t.Fatalf("space-2 pinned = %d, want 1", links)
			}
		}
	}
}

func TestNewUUIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}$`)
	for i := 0; i < 100; i++ {
		id := newUUID()
		if !re.MatchString(id) {
			t.Fatalf("invalid UUID %q", id)
		}
	}
}

func TestReplaceRejectsCycle(t *testing.T) {
	dir, path := writeFixture(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	profile := profileContainer(root)
	items := pairMap(profile["items"])
	items["old-tab"]["childrenIds"] = []any{"old-tab"}
	b, err = json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Replace(ReplaceOptions{Dir: dir, SkipProcessCheck: true})
	if err == nil || !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("err = %v, want cycle error", err)
	}
}

func TestReplaceIncrementalAddsOnlyNew(t *testing.T) {
	dir, _ := writeFixture(t)
	nodes := []*model.Node{
		{Kind: model.Link, Title: "Old duplicate", URL: "https://old.example/"},
		{Kind: model.Link, Title: "Top App duplicate", URL: "https://app.example/"},
		{Kind: model.Link, Title: "New", URL: "https://new.example/"},
		{Kind: model.Folder, Title: "Only Top App", Children: []*model.Node{
			{Kind: model.Link, Title: "Top App nested", URL: "https://app.example/"},
		}},
		{Kind: model.Folder, Title: "Old folder", Children: []*model.Node{
			{Kind: model.Link, Title: "Merged", URL: "https://merged.example/"},
		}},
		{Kind: model.Folder, Title: "New folder", Children: []*model.Node{
			{Kind: model.Link, Title: "Nested", URL: "https://nested.example/"},
		}},
	}
	res, err := Replace(ReplaceOptions{Dir: dir, Nodes: nodes, Incremental: true, SkipProcessCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.RemovedTabs != 0 || res.RemovedFolders != 0 || res.RemovedEasels != 0 {
		t.Fatalf("incremental removed items: %+v", res)
	}
	if res.AddedLinks != 3 || res.AddedFolders != 1 {
		t.Fatalf("added = %d links, %d folders; want 3/1", res.AddedLinks, res.AddedFolders)
	}
	store, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	links, folders := model.Count(store.Spaces[0].Pinned)
	if links != 4 || folders != 2 {
		t.Fatalf("pinned = %d links, %d folders; want 4/2", links, folders)
	}
	if len(store.Spaces[0].Pinned) < 4 ||
		store.Spaces[0].Pinned[0].Kind != model.Folder ||
		store.Spaces[0].Pinned[1].Kind != model.Folder ||
		store.Spaces[0].Pinned[2].Kind != model.Link ||
		store.Spaces[0].Pinned[3].Kind != model.Link {
		t.Fatalf("pinned is not folders-first: %+v", store.Spaces[0].Pinned)
	}
	var oldFolder *model.Node
	for _, n := range store.Spaces[0].Pinned {
		if n.Kind == model.Folder && n.Title == "Old folder" {
			oldFolder = n
		}
	}
	if oldFolder == nil || len(oldFolder.Children) != 1 || oldFolder.Children[0].URL != "https://merged.example/" {
		t.Fatalf("old folder not merged: %+v", oldFolder)
	}
	var walk func([]*model.Node)
	walk = func(nodes []*model.Node) {
		for _, n := range nodes {
			if n.Kind == model.Link && n.URL == "https://app.example/" {
				t.Fatalf("Top App URL was imported into pinned: %+v", n)
			}
			if n.Kind == model.Folder && n.Title == "Only Top App" {
				t.Fatalf("folder containing only a Top App URL was imported")
			}
			walk(n.Children)
		}
	}
	walk(store.Spaces[0].Pinned)
}
