package model

// Kind distinguishes a folder from a link.
type Kind int

const (
	Folder Kind = iota
	Link
)

// Node is the browser-independent bookmark tree used by every adapter.
type Node struct {
	Kind     Kind
	Title    string
	URL      string
	Created  float64 // Apple/CoreFoundation epoch seconds (2001-01-01); 0 = unknown
	Children []*Node
}

func (n *Node) IsFolder() bool { return n.Kind == Folder }

// FoldersFirst returns a copy of nodes with folders before links at every
// level. The sort is stable: items of the same kind keep their relative order.
func FoldersFirst(nodes []*Node) []*Node {
	out := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.Kind == Folder {
			out = append(out, n)
		}
	}
	for _, n := range nodes {
		if n != nil && n.Kind != Folder {
			out = append(out, n)
		}
	}
	for _, n := range out {
		if n.Kind == Folder {
			n.Children = FoldersFirst(n.Children)
		}
	}
	return out
}

// Count returns the number of links and folders below nodes.
func Count(nodes []*Node) (links, folders int) {
	for _, n := range nodes {
		if n.Kind == Link {
			links++
			continue
		}
		folders++
		l, f := Count(n.Children)
		links += l
		folders += f
	}
	return
}
