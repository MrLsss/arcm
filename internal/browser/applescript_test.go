package browser

import (
	"testing"

	"arcm/internal/model"
)

func TestConvert(t *testing.T) {
	nodes := Convert([]rawNode{
		{Kind: "link", Title: "A", URL: " https://a.example/ "},
		{Kind: "link", URL: "https://b.example/"},
		{Kind: "link", URL: " "},
		{Kind: "folder", Title: "F", Children: []rawNode{
			{Kind: "link", Title: "C", URL: "https://c.example/"},
		}},
		{Kind: "folder", Children: []rawNode{}},
	})
	links, folders := model.Count(nodes)
	if links != 3 || folders != 2 {
		t.Fatalf("Convert = %d links, %d folders", links, folders)
	}
	if nodes[0].Title != "A" || nodes[0].URL != "https://a.example/" {
		t.Fatalf("link trimmed incorrectly: %+v", nodes[0])
	}
	if nodes[1].Title != "https://b.example/" {
		t.Fatalf("missing title fallback: %+v", nodes[1])
	}
	if nodes[3].Title != "(untitled folder)" {
		t.Fatalf("missing folder title fallback: %+v", nodes[3])
	}
}
