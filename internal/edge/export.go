// Package edge reads Microsoft Edge bookmarks through Edge's AppleScript
// bookmark model.
package edge

import (
	"arcm/internal/browser"
	"arcm/internal/model"
)

// Read returns Edge's bookmark bar (and other bookmarks, when non-empty) as a
// model tree.
func Read() ([]*model.Node, error) {
	return browser.Read(browser.Options{
		AppName:    "Microsoft Edge",
		OtherLabel: "其他收藏夹",
	})
}
