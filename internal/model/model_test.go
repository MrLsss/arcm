package model

import "testing"

func TestFoldersFirstIsStableAndRecursive(t *testing.T) {
	folder := &Node{Kind: Folder, Title: "F", Children: []*Node{
		{Kind: Link, Title: "inside link", URL: "https://inside.example/"},
		{Kind: Folder, Title: "nested", Children: []*Node{
			{Kind: Link, Title: "nested link", URL: "https://nested.example/"},
		}},
	}}
	nodes := []*Node{
		{Kind: Link, Title: "a", URL: "https://a.example/"},
		folder,
		{Kind: Link, Title: "b", URL: "https://b.example/"},
	}
	got := FoldersFirst(nodes)
	if len(got) != 3 || got[0] != folder || got[1].Title != "a" || got[2].Title != "b" {
		t.Fatalf("top-level order = %+v", got)
	}
	if len(folder.Children) != 2 || folder.Children[0].Title != "nested" || folder.Children[1].Title != "inside link" {
		t.Fatalf("nested order = %+v", folder.Children)
	}
}
