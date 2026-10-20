package chrome

import (
	"arcm/internal/browser"
	"arcm/internal/model"
)

// Read returns Chrome's bookmark bar (and other bookmarks, when non-empty) as
// a model tree. It uses Chrome's AppleScript dictionary, not the profile file.
func Read() ([]*model.Node, error) {
	return browser.Read(browser.Options{
		AppName:    "Google Chrome",
		OtherLabel: "其他书签",
	})
}
