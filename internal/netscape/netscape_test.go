package netscape_test

import (
	"bytes"
	"math"
	"testing"

	"arcm/internal/model"
	"arcm/internal/netscape"
)

func TestRoundTrip(t *testing.T) {
	groups := []netscape.Group{{Title: "Arc", Nodes: []*model.Node{
		{Kind: model.Link, Title: "A & B", URL: "https://example.com/?a=1&b=2"},
		{Kind: model.Folder, Title: "Folder", Children: []*model.Node{
			{Kind: model.Link, Title: "Nested", URL: "https://example.org/"},
		}},
	}}}
	var buf bytes.Buffer
	if err := netscape.Export(groups, &buf); err != nil {
		t.Fatal(err)
	}
	nodes, err := netscape.Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	links, folders := model.Count(nodes)
	if links != 2 || folders != 2 {
		t.Fatalf("links=%d folders=%d, want 2/2\n%s", links, folders, buf.String())
	}
	if nodes[0].Title != "Arc" || nodes[0].Children[0].URL != "https://example.com/?a=1&b=2" {
		t.Fatalf("unexpected tree: %#v", nodes[0])
	}
}

func TestExportUntitledGroupDoesNotCreateWrapper(t *testing.T) {	groups := []netscape.Group{{Title: "", Nodes: []*model.Node{
		{Kind: model.Link, Title: "A", URL: "https://a.example/"},
		{Kind: model.Folder, Title: "F", Children: []*model.Node{
			{Kind: model.Link, Title: "B", URL: "https://b.example/"},
		}},
	}}}
	var buf bytes.Buffer
	if err := netscape.Export(groups, &buf); err != nil {
		t.Fatal(err)
	}
	nodes, err := netscape.Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	links, folders := model.Count(nodes)
	if links != 2 || folders != 1 {
		t.Fatalf("links=%d folders=%d, want 2/1\n%s", links, folders, buf.String())
	}
	if nodes[0].Kind != model.Link || nodes[0].Title != "A" {
		t.Fatalf("unexpected first node: %#v", nodes[0])
	}
}

func TestParseUnquotedHref(t *testing.T) {
	input := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
<DT><A HREF=https://unquoted.example/path ADD_DATE="1700000000123456">Unquoted</A>
</DL><p>`
	nodes, err := netscape.Parse(bytes.NewBufferString(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].URL != "https://unquoted.example/path" {
		t.Fatalf("nodes = %+v", nodes)
	}
}

func TestParsePreservesAddDate(t *testing.T) {
	const created = 1700000000.123456
	groups := []netscape.Group{{Title: "", Nodes: []*model.Node{
		{Kind: model.Link, Title: "Dated", URL: "https://dated.example/", Created: created},
	}}}
	var buf bytes.Buffer
	if err := netscape.Export(groups, &buf); err != nil {
		t.Fatal(err)
	}
	nodes, err := netscape.Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || math.Abs(nodes[0].Created-created) > 1e-6 {
		t.Fatalf("created = %v, want %v", nodes[0].Created, created)
	}
}

func TestParseRejectsCorruptHTML(t *testing.T) {
	for _, input := range []string{
		`<html><body><p>not a bookmark file</p></body></html>`,
		`<!DOCTYPE NETSCAPE-Bookmark-file-1>`,
	} {
		if _, err := netscape.Parse(bytes.NewBufferString(input)); err == nil {
			t.Fatalf("corrupt HTML was accepted: %q", input)
		}
	}
}
