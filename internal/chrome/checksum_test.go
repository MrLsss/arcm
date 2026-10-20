package chrome_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"arcm/internal/chrome"
	"arcm/internal/model"
)

func TestChecksumMatchesChromiumFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	path := filepath.Join(home, "Library/Application Support/Arc/User Data/Default/Bookmarks")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("Arc Chromium bookmarks unavailable: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	roots, _ := data["roots"].(map[string]any)
	want, _ := data["checksum"].(string)
	if got := chrome.Checksum(roots); got != want {
		t.Fatalf("checksum = %s, want %s", got, want)
	}
}

func TestImportCreatesValidFile(t *testing.T) {
	dir := t.TempDir()
	nodes := []*model.Node{
		{Kind: model.Folder, Title: "Arc", Children: []*model.Node{
			{Kind: model.Link, Title: "Example", URL: "https://example.com/"},
			{Kind: model.Folder, Title: "Sub", Children: []*model.Node{
				{Kind: model.Link, Title: "Gitee", URL: "https://gitee.com/"},
			}},
		}},
	}
	res, err := chrome.Import(chrome.ImportOptions{Nodes: nodes, ProfileDir: dir, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Links != 2 || res.Folders != 2 {
		t.Fatalf("links=%d folders=%d, want 2/2", res.Links, res.Folders)
	}
	f, err := chrome.Load(filepath.Join(dir, "Bookmarks"))
	if err != nil {
		t.Fatal(err)
	}
	if got := chrome.Checksum(f.Roots()); got != res.Checksum {
		t.Fatalf("saved checksum %s != recomputed %s", res.Checksum, got)
	}
}

func TestImportPreservesSourceFolders(t *testing.T) {
	dir := t.TempDir()
	nodes := []*model.Node{
		{Kind: model.Folder, Title: "Arc", Children: []*model.Node{
			{Kind: model.Link, Title: "One", URL: "https://one.example/"},
			{Kind: model.Folder, Title: "Sub", Children: []*model.Node{
				{Kind: model.Link, Title: "Two", URL: "https://two.example/"},
			}},
		}},
		{Kind: model.Folder, Title: "Arc Top Apps", Children: []*model.Node{
			{Kind: model.Link, Title: "App", URL: "https://app.example/"},
		}},
	}
	res, err := chrome.Import(chrome.ImportOptions{Nodes: nodes, ProfileDir: dir, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Links != 3 || res.Folders != 3 {
		t.Fatalf("links=%d folders=%d, want 3/3", res.Links, res.Folders)
	}
	kids := barChildren(t, dir)
	if len(kids) != 2 {
		t.Fatalf("bookmark_bar children = %d, want 2", len(kids))
	}
	for i, want := range []string{"Arc", "Arc Top Apps"} {
		if kind(kids[i]) != "folder" || name(kids[i]) != want {
			t.Fatalf("child %d = %+v, want folder %q", i, kids[i], want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".arcm-state.json")); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func TestImportIsIdempotentInFlatMode(t *testing.T) {
	dir := t.TempDir()
	nodes := func() []*model.Node {
		return []*model.Node{
			{Kind: model.Link, Title: "One", URL: "https://one.example/"},
			{Kind: model.Folder, Title: "Sub", Children: []*model.Node{
				{Kind: model.Link, Title: "Two", URL: "https://two.example/"},
			}},
		}
	}
	if _, err := chrome.Import(chrome.ImportOptions{Nodes: nodes(), ProfileDir: dir, Force: true}); err != nil {
		t.Fatal(err)
	}
	res, err := chrome.Import(chrome.ImportOptions{Nodes: nodes(), ProfileDir: dir, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("removed = %d, want 2", res.Removed)
	}
	f, err := chrome.Load(filepath.Join(dir, "Bookmarks"))
	if err != nil {
		t.Fatal(err)
	}
	bar, _ := f.Roots()["bookmark_bar"].(map[string]any)
	if kids, _ := bar["children"].([]any); len(kids) != 2 {
		t.Fatalf("bookmark_bar children = %d, want 2 after re-import", len(kids))
	}
}

func TestImportPreservesTopAppsFolderAndSorts(t *testing.T) {
	dir := t.TempDir()
	nodes := []*model.Node{
		{Kind: model.Link, Title: "One", URL: "https://one.example/"},
		{Kind: model.Folder, Title: "top apps", Children: []*model.Node{
			{Kind: model.Link, Title: "App", URL: "https://app.example/"},
		}},
		{Kind: model.Folder, Title: "Sub", Children: []*model.Node{
			{Kind: model.Link, Title: "Two", URL: "https://two.example/"},
		}},
	}
	res, err := chrome.Import(chrome.ImportOptions{Nodes: nodes, ProfileDir: dir, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Links != 3 || res.Folders != 2 || res.Skipped != 0 {
		t.Fatalf("result = %+v, want 3 links / 2 folders / 0 skipped", res)
	}
	kids := barChildren(t, dir)
	if len(kids) != 3 {
		t.Fatalf("bookmark_bar children = %d, want 3", len(kids))
	}
	if name(kids[0]) != "top apps" || name(kids[1]) != "Sub" {
		t.Fatalf("folders are not first: %q, %q", name(kids[0]), name(kids[1]))
	}
	for i, k := range kids {
		if i < 2 && kind(k) != "folder" {
			t.Fatalf("child %d is not a folder: %+v", i, k)
		}
		if i >= 2 && kind(k) != "url" {
			t.Fatalf("child %d is not a link: %+v", i, k)
		}
	}
	topApps := asNodeMap(kids[0])
	children := asNodeSlice(topApps["children"])
	if len(children) != 1 || urlOf(children[0]) != "https://app.example/" {
		t.Fatalf("top apps children = %+v", children)
	}
	for _, k := range kids {
		if kind(k) == "url" && urlOf(k) == "https://app.example/" {
			t.Fatal("top apps link leaked to the bookmark bar")
		}
	}
}

func TestImportDeduplicatesBookmarkBar(t *testing.T) {
	dir := t.TempDir()
	if _, err := chrome.Import(chrome.ImportOptions{
		Nodes: []*model.Node{
			{Kind: model.Folder, Title: "Existing", Children: []*model.Node{
				{Kind: model.Link, Title: "Keep", URL: "https://keep.example/"},
			}},
			{Kind: model.Link, Title: "One", URL: "https://one.example/"},
		},
		ProfileDir: dir, Force: true,
	}); err != nil {
		t.Fatal(err)
	}
	// Remove arcm state so the first import looks like ordinary user data.
	if err := os.Remove(filepath.Join(dir, ".arcm-state.json")); err != nil {
		t.Fatal(err)
	}
	nodes := []*model.Node{
		{Kind: model.Link, Title: "Duplicate", URL: "https://one.example/"},
		{Kind: model.Folder, Title: "top apps", Children: []*model.Node{
			{Kind: model.Link, Title: "Duplicate nested", URL: "https://keep.example/"},
		}},
		{Kind: model.Folder, Title: "New", Children: []*model.Node{
			{Kind: model.Link, Title: "Nested", URL: "https://nested.example/"},
		}},
		{Kind: model.Folder, Title: "Empty"},
		{Kind: model.Link, Title: "New link", URL: "https://new.example/"},
	}
	res, err := chrome.Import(chrome.ImportOptions{Nodes: nodes, ProfileDir: dir, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Links != 2 || res.Folders != 2 || res.Skipped != 3 {
		t.Fatalf("result = %+v, want 2 links / 2 folders / 3 skipped", res)
	}
	kids := barChildren(t, dir)
	if len(kids) != 5 {
		t.Fatalf("bookmark_bar children = %d, want 5: %+v", len(kids), kids)
	}
	for i, k := range kids {
		if i < 3 && kind(k) != "folder" {
			t.Fatalf("child %d is not a folder: %+v", i, k)
		}
		if i >= 3 && kind(k) != "url" {
			t.Fatalf("child %d is not a link: %+v", i, k)
		}
	}
	counts := map[string]int{}
	var walk func([]any)
	walk = func(list []any) {
		for _, k := range list {
			if kind(k) == "url" {
				counts[urlOf(k)]++
				continue
			}
			walk(asNodeSlice(asNodeMap(k)["children"]))
		}
	}
	walk(kids)
	for _, u := range []string{"https://one.example/", "https://keep.example/"} {
		if counts[u] != 1 {
			t.Fatalf("%s count = %d, want 1", u, counts[u])
		}
	}
	if counts["https://nested.example/"] != 1 || counts["https://new.example/"] != 1 {
		t.Fatalf("new links missing: %+v", counts)
	}
}

func TestImportKeepsUnmanagedArcFolder(t *testing.T) {
	dir := t.TempDir()
	if _, err := chrome.Import(chrome.ImportOptions{
		Nodes: []*model.Node{{Kind: model.Folder, Title: "Arc", Children: []*model.Node{
			{Kind: model.Link, Title: "Keep", URL: "https://keep.example/"},
		}}},
		ProfileDir: dir, Force: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ".arcm-state.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := chrome.Import(chrome.ImportOptions{
		Nodes:      []*model.Node{{Kind: model.Link, Title: "Other", URL: "https://other.example/"}},
		ProfileDir: dir, Force: true,
	}); err != nil {
		t.Fatal(err)
	}
	kids := barChildren(t, dir)
	var arc map[string]any
	for _, k := range kids {
		if kind(k) == "folder" && name(k) == "Arc" {
			arc = asNodeMap(k)
		}
	}
	if arc == nil {
		t.Fatalf("unmanaged Arc folder was deleted: %+v", kids)
	}
	children := asNodeSlice(arc["children"])
	if len(children) != 1 || urlOf(children[0]) != "https://keep.example/" {
		t.Fatalf("Arc children = %+v", children)
	}
}

func barChildren(t *testing.T, dir string) []any {
	t.Helper()
	f, err := chrome.Load(filepath.Join(dir, "Bookmarks"))
	if err != nil {
		t.Fatal(err)
	}
	bar := asNodeMap(f.Roots()["bookmark_bar"])
	kids, _ := bar["children"].([]any)
	return kids
}

func asNodeMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asNodeSlice(v any) []any        { s, _ := v.([]any); return s }
func name(v any) string              { return asNodeMap(v)["name"].(string) }
func kind(v any) string              { return asNodeMap(v)["type"].(string) }
func urlOf(v any) string             { return asNodeMap(v)["url"].(string) }

func TestImportGenericChromiumTarget(t *testing.T) {
	dir := t.TempDir()
	nodes := []*model.Node{
		{Kind: model.Link, Title: "Edge one", URL: "https://edge.example/"},
	}
	res, err := chrome.Import(chrome.ImportOptions{
		Nodes: nodes, ProfileDir: dir, Force: true,
		BrowserName: "Edge", ProcessName: "Microsoft Edge",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Links != 1 || res.Folders != 0 {
		t.Fatalf("links=%d folders=%d, want 1/0", res.Links, res.Folders)
	}
	f, err := chrome.Load(filepath.Join(dir, "Bookmarks"))
	if err != nil {
		t.Fatal(err)
	}
	if got := chrome.Checksum(f.Roots()); got != res.Checksum {
		t.Fatalf("saved checksum %s != recomputed %s", res.Checksum, got)
	}
}
