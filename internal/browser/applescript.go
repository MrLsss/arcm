// Package browser reads bookmarks from Chromium-based browsers through their
// macOS AppleScript dictionary. Chrome and Edge expose the bookmark model as
// read-only AppleScript objects; the profile's Bookmarks file is not used here.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"arcm/internal/model"
)

type Options struct {
	AppName    string // e.g. "Google Chrome", "Microsoft Edge"
	OtherLabel string // e.g. "其他书签", "其他收藏夹"
}

type rawNode struct {
	Kind     string    `json:"kind"`
	Title    string    `json:"title"`
	URL      string    `json:"url,omitempty"`
	Children []rawNode `json:"children,omitempty"`
}

type rawExport struct {
	Bar   []rawNode `json:"bar"`
	Other []rawNode `json:"other"`
}

const scriptTemplate = `
const app = Application(%s);
if (!app.running()) throw new Error(%s);
function children(folder) {
  const out = [];
  const folders = folder.bookmarkFolders().map(f => ({
    i: f.index(), kind: "folder", title: f.title() || "", children: children(f)
  }));
  const items = folder.bookmarkItems().map(b => ({
    i: b.index(), kind: "link", title: b.title() || "", url: b.url() || ""
  }));
  return folders.concat(items).sort((a, b) => a.i - b.i).map(({i, ...rest}) => rest);
}
let other = [];
try { other = children(app.otherBookmarks); } catch (e) {}
JSON.stringify({bar: children(app.bookmarksBar), other: other});
`

// Read returns the browser's bookmark bar as a model tree. The bar's children
// become top-level nodes; a non-empty "other bookmarks" folder is appended.
func Read(opts Options) ([]*model.Node, error) {
	app := strings.TrimSpace(opts.AppName)
	if app == "" {
		return nil, fmt.Errorf("browser app name is required")
	}
	script := fmt.Sprintf(scriptTemplate, strconv.Quote(app), strconv.Quote(app+" is not running"))

	var out []byte
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		out, err = exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script).Output()
		cancel()
		if err == nil {
			break
		}
		msg := strings.TrimSpace(string(out))
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		if strings.Contains(msg, "is not running") {
			return nil, fmt.Errorf("%s is not running; open it first", app)
		}
		if attempt < 3 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("read %s bookmarks via AppleScript: %w: %s", app, err, msg)
	}

	var raw rawExport
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse %s AppleScript output: %w", app, err)
	}
	nodes := Convert(raw.Bar)
	if len(raw.Other) > 0 {
		label := strings.TrimSpace(opts.OtherLabel)
		if label == "" {
			label = "其他书签"
		}
		nodes = append(nodes, &model.Node{
			Kind:     model.Folder,
			Title:    label,
			Children: Convert(raw.Other),
		})
	}
	return nodes, nil
}

// Convert maps the JXA bridge's raw nodes into model nodes.
func Convert(in []rawNode) []*model.Node {
	out := make([]*model.Node, 0, len(in))
	for _, n := range in {
		if n.Kind == "link" {
			url := strings.TrimSpace(n.URL)
			if url == "" {
				continue
			}
			title := strings.TrimSpace(n.Title)
			if title == "" {
				title = url
			}
			out = append(out, &model.Node{Kind: model.Link, Title: title, URL: url})
			continue
		}
		title := strings.TrimSpace(n.Title)
		if title == "" {
			title = "(untitled folder)"
		}
		out = append(out, &model.Node{Kind: model.Folder, Title: title, Children: Convert(n.Children)})
	}
	return out
}
