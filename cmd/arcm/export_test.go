package main

import (
	"os"
	"path/filepath"
	"testing"

	"arcm/internal/arc"
	"arcm/internal/model"
)

func TestBuildGroupsKeepsTopAppsAsFolder(t *testing.T) {
	space := &arc.Space{
		Pinned: []*model.Node{
			{Kind: model.Link, Title: "Pinned", URL: "https://pinned.example/"},
			{Kind: model.Folder, Title: "Folder", Children: []*model.Node{
				{Kind: model.Link, Title: "Nested", URL: "https://nested.example/"},
			}},
		},
		TopApps: []*model.Node{
			{Kind: model.Link, Title: "App", URL: "https://app.example/"},
		},
	}
	groups := buildGroups(space, true)
	if len(groups) != 2 || groups[0].Title != "" || groups[1].Title != "top apps" {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].Nodes[0].Title != "Folder" {
		t.Fatalf("pinned export is not folders-first: %+v", groups[0].Nodes)
	}

	nodes := groupsAsNodes(groups)
	if len(nodes) != 3 {
		t.Fatalf("nodes = %+v", nodes)
	}
	if nodes[0].Title != "Folder" || nodes[1].Title != "Pinned" {
		t.Fatalf("pinned export is not folders-first: %+v", nodes)
	}
	if nodes[2].Kind != model.Folder || nodes[2].Title != "top apps" {
		t.Fatalf("top apps folder was lost: %+v", nodes[2])
	}
}

func TestReadBookmarkFileKeepsJSONGroups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arc.json")
	data := `{"version":1,"groups":[
		{"title":"Arc","nodes":[{"kind":"link","title":"Pinned","url":"https://pinned.example/"}]},
		{"title":"top apps","nodes":[{"kind":"link","title":"App","url":"https://app.example/"}]}
	]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	nodes, err := readBookmarkFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %+v", nodes)
	}
	if nodes[0].Kind != model.Folder || nodes[0].Title != "Arc" || len(nodes[0].Children) != 1 {
		t.Fatalf("Arc group was not preserved: %+v", nodes[0])
	}
	if nodes[1].Kind != model.Folder || nodes[1].Title != "top apps" || len(nodes[1].Children) != 1 {
		t.Fatalf("top apps group was lost: %+v", nodes[1])
	}
}

func TestReadBookmarkFileRejectsInvalidJSON(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"version", `{"version":2,"groups":[]}`},
		{"unknown kind", `{"version":1,"groups":[{"nodes":[{"kind":"tab"}]}]}`},
		{"link without url", `{"version":1,"groups":[{"nodes":[{"kind":"link","title":"bad"}]}]}`},
		{"link with children", `{"version":1,"groups":[{"nodes":[{"kind":"link","url":"https://x.example/","children":[{"kind":"link","url":"https://y.example/"}]}]}]}`},
		{"folder with url", `{"version":1,"groups":[{"nodes":[{"kind":"folder","title":"bad","url":"https://x.example/"}]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.json")
			if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readBookmarkFile(path); err == nil {
				t.Fatal("invalid bookmark JSON was accepted")
			}
		})
	}
}
