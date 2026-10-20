package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"arcm/internal/arc"
	"arcm/internal/chrome"
	"arcm/internal/edge"
	"arcm/internal/model"
	"arcm/internal/netscape"
)

const version = "1.0.0"

type browser string

const (
	browserArc    browser = "arc"
	browserChrome browser = "chrome"
	browserEdge   browser = "edge"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	inv, err := parseInvocation(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "arcm:", err)
		usage()
		return 2
	}
	switch inv.kind {
	case "help":
		return cmdHelp(inv.rest)
	case "export":
		cmdExport(inv.browser, inv.rest)
	case "import":
		cmdImport(inv.browser, inv.rest)
	case "transfer":
		cmdTransfer(inv.from, inv.to, inv.rest)
	case "doctor":
		cmdDoctor(inv.rest)
	case "preview":
		cmdPreview(inv.rest)
	case "verify":
		return cmdVerify(inv.rest)
	case "version":
		fmt.Println("arcm", version)
	}
	return 0
}

type invocation struct {
	kind    string
	browser browser
	from    browser
	to      browser
	rest    []string
}

func parseInvocation(args []string) (invocation, error) {
	if len(args) == 0 {
		return invocation{}, fmt.Errorf("missing command")
	}
	switch args[0] {
	case "export", "import":
		if wantsHelp(args[1:]) {
			return invocation{kind: "help", rest: []string{args[0]}}, nil
		}
		if len(args) < 2 {
			return invocation{}, fmt.Errorf("%s needs a browser: arc, chrome or edge", args[0])
		}
		b, err := parseBrowser(args[1])
		if err != nil {
			return invocation{}, err
		}
		return invocation{kind: args[0], browser: b, rest: args[2:]}, nil
	case "doctor", "preview", "verify":
		return invocation{kind: args[0], rest: args[1:]}, nil
	case "version", "-v", "--version":
		return invocation{kind: "version"}, nil
	case "help", "-h", "--help":
		return invocation{kind: "help", rest: args[1:]}, nil
	}

	from, err := parseBrowser(args[0])
	if err != nil {
		return invocation{}, fmt.Errorf("unknown command %q; expected export, import, or <from-browser> <to-browser>", args[0])
	}
	if wantsHelp(args[1:]) {
		return invocation{kind: "help", rest: []string{"transfer"}}, nil
	}
	if len(args) < 2 {
		return invocation{}, fmt.Errorf("missing destination browser after %q", args[0])
	}
	to, err := parseBrowser(args[1])
	if err != nil {
		return invocation{}, err
	}
	if from == to {
		return invocation{}, fmt.Errorf("source and destination are both %s; pick two different browsers", from)
	}
	return invocation{kind: "transfer", from: from, to: to, rest: args[2:]}, nil
}

func parseBrowser(name string) (browser, error) {
	switch browser(strings.ToLower(strings.TrimSpace(name))) {
	case browserArc:
		return browserArc, nil
	case browserChrome:
		return browserChrome, nil
	case browserEdge:
		return browserEdge, nil
	default:
		return "", fmt.Errorf("unknown browser %q; supported: arc, chrome, edge", name)
	}
}

func (b browser) label() string {
	switch b {
	case browserArc:
		return "Arc"
	case browserChrome:
		return "Chrome"
	case browserEdge:
		return "Edge"
	}
	return string(b)
}

func processName(b browser) string {
	switch b {
	case browserChrome:
		return "Google Chrome"
	case browserEdge:
		return "Microsoft Edge"
	case browserArc:
		return "Arc"
	}
	return string(b)
}

func defaultOutput(b browser) string {
	return string(b) + "-bookmarks.html"
}

func defaultProfile(b browser) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch b {
	case browserChrome:
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default"), nil
	case browserEdge:
		return filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "Default"), nil
	default:
		return "", fmt.Errorf("%s has no Chromium profile directory", b.label())
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.Usage = func() {
		printCommandHelp(name, os.Stderr)
	}
	return fs
}

func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "-help" {
			return true
		}
	}
	return false
}

func usage() {
	printGlobalHelp(os.Stderr)
}

func cmdExport(b browser, args []string) {
	fs := newFlagSet("export")
	out := fs.String("out", "", "输出文件；- 表示标准输出；默认 <浏览器>-bookmarks.html")
	format := fs.String("format", "html", "输出格式：html 或 json")
	arcDir := fs.String("arc-dir", "", "Arc 数据目录，仅 arc 可用")
	space := fs.String("space", "", "Arc Space ID；存在多个 Space 时必须指定")
	noTop := fs.Bool("no-topapps", false, "不导出 Arc Top Apps")
	fs.Parse(args)
	if fs.NArg() > 0 {
		check(fmt.Errorf("unexpected arguments: %v", fs.Args()))
	}
	if *format != "html" && *format != "json" {
		check(fmt.Errorf("unknown format %q (want html or json)", *format))
	}
	if b != browserArc {
		if flagWasSet(fs, "arc-dir") || flagWasSet(fs, "space") || flagWasSet(fs, "no-topapps") {
			check(fmt.Errorf("--arc-dir, --space and --no-topapps only apply to arc"))
		}
	}

	src, err := loadSource(b, *arcDir, !*noTop, *space)
	check(err)
	target := *out
	if target == "" {
		target = defaultOutput(b)
	}
	if target == "-" {
		printSourceTo(os.Stderr, src, "")
	} else {
		printSource(src, "")
	}
	check(writeExport(src.groups, target, *format))
}

func writeExport(groups []netscape.Group, out, format string) error {
	var buf bytes.Buffer
	switch format {
	case "html":
		if err := netscape.Export(groups, &buf); err != nil {
			return err
		}
	case "json":
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(exportDoc{Version: 1, Groups: toDTOGroups(groups)}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown format %q", format)
	}
	if out == "-" {
		_, err := os.Stdout.Write(buf.Bytes())
		return err
	}
	dir := filepath.Dir(out)
	tmp, err := os.CreateTemp(dir, ".arcm-export-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, out); err != nil {
		return err
	}
	links, folders := model.Count(groupsAsNodes(groups))
	fmt.Printf("Wrote %s (%d links, %d folders)\n", out, links, folders)
	return nil
}

type sourceData struct {
	label    string
	groups   []netscape.Group
	links    int
	folders  int
	warnings []string
	detail   string
}

func loadSource(b browser, arcDir string, includeTop bool, spaceID string) (*sourceData, error) {
	switch b {
	case browserArc:
		store, err := arc.Read(arcDir)
		if err != nil {
			return nil, err
		}
		space, err := store.SelectSpace(spaceID)
		if err != nil {
			return nil, err
		}
		groups := buildGroups(space, includeTop)
		links, folders := model.Count(groupsAsNodes(groups))
		return &sourceData{
			label:    "Arc",
			groups:   groups,
			links:    links,
			folders:  folders,
			warnings: store.Warnings,
			detail: fmt.Sprintf("Arc data: %s\nSpace: %s\nSpaces: %d, easels skipped: %d",
				store.Path, space.ID, len(store.Spaces), store.Easels),
		}, nil
	case browserChrome:
		nodes, err := chrome.Read()
		if err != nil {
			return nil, err
		}
		return sourceForNodes("Chrome", nodes), nil
	case browserEdge:
		nodes, err := edge.Read()
		if err != nil {
			return nil, err
		}
		return sourceForNodes("Edge", nodes), nil
	default:
		return nil, fmt.Errorf("unknown source browser %s", b)
	}
}

func sourceForNodes(label string, nodes []*model.Node) *sourceData {
	groups := []netscape.Group{{Title: "", Nodes: nodes}}
	links, folders := model.Count(nodes)
	return &sourceData{label: label, groups: groups, links: links, folders: folders}
}

func printSource(src *sourceData, prefix string) {
	printSourceTo(os.Stdout, src, prefix)
}

func printSourceTo(out io.Writer, src *sourceData, prefix string) {
	if src.detail != "" {
		fmt.Fprintln(out, src.detail)
	}
	fmt.Fprintf(out, "%s%s: %d links, %d folders\n", prefix, src.label, src.links, src.folders)
	for _, w := range src.warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
}

func cmdImport(to browser, args []string) {
	fs := newFlagSet("import")
	source := fs.String("source", "", "书签文件或来源浏览器（arc|chrome|edge）")
	arcDir := fs.String("arc-dir", "", "Arc 数据目录")
	space := fs.String("space", "", "Arc Space ID；来源或目标为 Arc 且存在多个 Space 时必须指定")
	profileDir := fs.String("profile-dir", "", "目标 Chromium profile 目录（含 Bookmarks）")
	noTop := fs.Bool("no-topapps", false, "来源为 Arc 时不导入 Top Apps")
	appendMode := fs.Bool("append", false, "Arc：保留已有项并追加，可能重复")
	dry := fs.Bool("dry-run", false, "只预览，不写文件")
	force := fs.Bool("force", false, "目标浏览器运行时仍强制写入（仅测试）")
	restart := fs.Bool("restart", false, "Arc：退出后写入，再重新打开")
	fs.Parse(reorderArgs(fs, args))

	if fs.NArg() > 1 {
		check(fmt.Errorf("expected at most one bookmark file, got %v", fs.Args()))
	}
	if fs.NArg() == 1 && *source != "" {
		check(fmt.Errorf("specify the bookmark source as an argument or with --source, not both"))
	}
	src := *source
	if fs.NArg() == 1 {
		src = fs.Arg(0)
	}
	if src == "" {
		check(fmt.Errorf("missing bookmark source: pass FILE or --source FILE|BROWSER"))
	}

	srcBrowser, srcErr := parseBrowser(src)
	srcIsFile := srcErr != nil
	if err := validateImportFlags(fs, srcBrowser, to, srcIsFile); err != nil {
		check(err)
	}
	if !srcIsFile && srcBrowser == to {
		check(fmt.Errorf("source and destination are both %s; import needs a different source browser or a bookmark file", to))
	}

	var nodes []*model.Node
	var srcInfo *sourceData
	if srcIsFile {
		var err error
		nodes, err = readBookmarkFile(src)
		check(err)
		links, folders := model.Count(nodes)
		srcInfo = &sourceData{label: src, links: links, folders: folders}
	} else {
		var err error
		srcInfo, err = loadSource(srcBrowser, *arcDir, !*noTop, *space)
		check(err)
		nodes = groupsAsNodes(srcInfo.groups)
	}
	printSource(srcInfo, "Source ")
	nodes = model.FoldersFirst(nodes)

	check(importNodes(to, nodes, importSettings{
		arcDir:     *arcDir,
		spaceID:    *space,
		profileDir: *profileDir,
		dryRun:     *dry,
		force:      *force,
		restart:    *restart,
		appendMode: *appendMode,
	}))
}

func validateImportFlags(fs *flag.FlagSet, src, dst browser, srcIsFile bool) error {
	if flagWasSet(fs, "profile-dir") && dst != browserChrome && dst != browserEdge {
		return fmt.Errorf("--profile-dir only applies to chrome or edge")
	}
	if flagWasSet(fs, "arc-dir") && src != browserArc && dst != browserArc {
		return fmt.Errorf("--arc-dir only applies when arc is the source or destination")
	}
	if flagWasSet(fs, "space") && src != browserArc && dst != browserArc {
		return fmt.Errorf("--space only applies when arc is the source or destination")
	}
	if flagWasSet(fs, "no-topapps") && src != browserArc {
		return fmt.Errorf("--no-topapps only applies when the source is arc")
	}
	if flagWasSet(fs, "append") && dst != browserArc {
		return fmt.Errorf("--append only applies when the destination is arc")
	}
	if flagWasSet(fs, "restart") && dst != browserArc {
		return fmt.Errorf("--restart only applies when the destination is arc")
	}
	if srcIsFile && flagWasSet(fs, "no-topapps") {
		return fmt.Errorf("--no-topapps only applies when the source is arc")
	}
	return nil
}

type importSettings struct {
	arcDir     string
	spaceID    string
	profileDir string
	dryRun     bool
	force      bool
	restart    bool
	appendMode bool
}

func importNodes(to browser, nodes []*model.Node, s importSettings) error {
	switch to {
	case browserArc:
		res, err := arc.Replace(arc.ReplaceOptions{
			Dir:         s.arcDir,
			SpaceID:     s.spaceID,
			Nodes:       nodes,
			DryRun:      s.dryRun,
			Force:       s.force,
			Restart:     s.restart,
			Append:      s.appendMode,
			Incremental: !s.appendMode,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Arc sidebar: %s\n", res.Path)
		fmt.Printf("Top Apps kept: %d\n", res.TopApps)
		fmt.Printf("Added: %d links, %d folders\n", res.AddedLinks, res.AddedFolders)
		if s.dryRun {
			return nil
		}
		if res.Backup != "" {
			fmt.Printf("Backup: %s\n", res.Backup)
		}
		if res.Restarted {
			fmt.Println("Arc restarted; open the sidebar to inspect.")
		} else if res.ArcRunning {
			fmt.Println("WARNING: Arc was running; restart Arc to load the new sidebar.")
		} else {
			fmt.Println("Start Arc to load the new sidebar.")
		}
		return nil
	case browserChrome, browserEdge:
		profile := s.profileDir
		if profile == "" {
			var err error
			profile, err = defaultProfile(to)
			if err != nil {
				return err
			}
		}
		res, err := chrome.Import(chrome.ImportOptions{
			Nodes:       nodes,
			ProfileDir:  profile,
			DryRun:      s.dryRun,
			Force:       s.force,
			BrowserName: to.label(),
			ProcessName: processName(to),
		})
		if err != nil {
			return err
		}
		fmt.Printf("%s Bookmarks: %s\n", to.label(), res.Path)
		fmt.Printf("Added to bookmark bar: %d links, %d folders (removed previous arcm items: %d, skipped duplicates: %d)\n",
			res.Links, res.Folders, res.Removed, res.Skipped)
		fmt.Printf("Checksum: %s\n", res.Checksum)
		if s.dryRun {
			fmt.Println("Dry run: no file written.")
			return nil
		}
		if res.Backup != "" {
			fmt.Printf("Backup: %s\n", res.Backup)
		}
		if res.State != "" {
			fmt.Printf("State: %s\n", res.State)
		}
		fmt.Printf("Imported. Quit %s first if it is running, then open its bookmark manager.\n", to.label())
		return nil
	default:
		return fmt.Errorf("unknown destination browser %s", to)
	}
}

func cmdTransfer(from, to browser, args []string) {
	if wantsHelp(args) {
		printCommandHelp("transfer", os.Stdout)
		return
	}
	for _, a := range args {
		if a == "--source" || strings.HasPrefix(a, "--source=") {
			check(fmt.Errorf("<from> <to> does not take --source; pass it only with the import command"))
		}
	}
	cmdImport(to, append([]string{"--source", string(from)}, args...))
}

func cmdVerify(args []string) int {
	fs := newFlagSet("verify")
	profileDir := fs.String("profile-dir", "", "Chromium profile 目录（Chrome/Edge/Brave 等）")
	fs.Parse(reorderArgs(fs, args))
	if fs.NArg() > 1 {
		check(fmt.Errorf("expected at most one browser, got %v", fs.Args()))
	}
	b := browserChrome
	if fs.NArg() == 1 {
		var err error
		b, err = parseBrowser(fs.Arg(0))
		check(err)
	}
	if b == browserArc {
		check(fmt.Errorf("Arc does not use a Chromium Bookmarks checksum; use doctor/preview for Arc"))
	}
	if *profileDir == "" {
		var err error
		*profileDir, err = defaultProfile(b)
		check(err)
	}
	path := filepath.Join(*profileDir, "Bookmarks")
	f, err := chrome.Load(path)
	check(err)
	roots := f.Roots()
	fmt.Printf("Browser: %s\n", b.label())
	fmt.Printf("File: %s\n", path)
	fmt.Printf("Checksum in file: %s\n", asString(f.Data["checksum"]))
	fmt.Printf("Computed checksum: %s\n", chrome.Checksum(roots))
	if asString(f.Data["checksum"]) != chrome.Checksum(roots) {
		fmt.Println("MISMATCH: file checksum does not match contents")
		return 1
	}
	fmt.Println("Checksum OK")
	var totalLinks, totalFolders int
	for _, key := range []string{"bookmark_bar", "other", "synced"} {
		if r := asMap(roots[key]); r != nil {
			l, fo := countChrome(r)
			totalLinks += l
			totalFolders += fo
			fmt.Printf("root %-12s links=%-4d folders=%d\n", key, l, fo)
		}
	}
	fmt.Printf("total links=%d folders=%d\n", totalLinks, totalFolders)
	return 0
}

func cmdDoctor(args []string) {
	fs := newFlagSet("doctor")
	arcDir := fs.String("arc-dir", "", "Arc 数据目录")
	fs.Parse(args)
	store, err := arc.Read(*arcDir)
	check(err)
	var pinnedLinks, pinnedFolders int
	fmt.Printf("Arc data: %s\n", store.Path)
	fmt.Printf("Size: %d bytes, mtime: %s, schema: %d\n", store.Size, store.Mtime.Format("2006-01-02 15:04:05"), store.Version)
	fmt.Printf("Spaces: %d\n", len(store.Spaces))
	for _, sp := range store.Spaces {
		l, f := model.Count(sp.Pinned)
		pinnedLinks += l
		pinnedFolders += f
		fmt.Printf("  %s: pinned %d links, %d folders\n", sp.ID, l, f)
	}
	topLinks, _ := model.Count(store.TopApps)
	fmt.Printf("Top Apps: %d links\n", topLinks)
	fmt.Printf("Easels skipped: %d\n", store.Easels)
	fmt.Printf("Total pinned: %d links, %d folders\n", pinnedLinks, pinnedFolders)
	fmt.Printf("Total exportable: %d links\n", pinnedLinks+topLinks)
	if len(store.Warnings) > 0 {
		fmt.Printf("Warnings:\n")
		for _, w := range store.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}
	chromeProfile, err := defaultProfile(browserChrome)
	check(err)
	fmt.Printf("Chrome profile: %s\n", chromeProfile)
	if _, err := os.Stat(filepath.Join(chromeProfile, "Bookmarks")); err == nil {
		fmt.Println("Chrome Bookmarks: present")
	} else {
		fmt.Printf("Chrome Bookmarks: not found (%v)\n", err)
	}
	edgeProfile, err := defaultProfile(browserEdge)
	check(err)
	fmt.Printf("Edge profile: %s\n", edgeProfile)
	if _, err := os.Stat(filepath.Join(edgeProfile, "Bookmarks")); err == nil {
		fmt.Println("Edge Bookmarks: present")
	} else {
		fmt.Printf("Edge Bookmarks: not found (%v)\n", err)
	}
}

func cmdPreview(args []string) {
	fs := newFlagSet("preview")
	arcDir := fs.String("arc-dir", "", "Arc 数据目录")
	noTop := fs.Bool("no-topapps", false, "不显示 Top Apps")
	fs.Parse(args)
	store, err := arc.Read(*arcDir)
	check(err)
	for _, sp := range store.Spaces {
		fmt.Printf("Space %s\n", sp.ID)
		fmt.Printf("  Pinned\n")
		for _, n := range sp.Pinned {
			printNode(n, 2)
		}
	}
	if !*noTop {
		fmt.Printf("Top Apps\n")
		for _, n := range store.TopApps {
			printNode(n, 1)
		}
	}
}

func printNode(n *model.Node, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}
	if n.Kind == model.Folder {
		fmt.Printf("%s[folder] %s\n", indent, n.Title)
		for _, c := range n.Children {
			printNode(c, depth+1)
		}
		return
	}
	fmt.Printf("%s- %s  %s\n", indent, n.Title, n.URL)
}

func readBookmarkFile(path string) ([]*model.Node, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		var doc exportDoc
		if err := json.NewDecoder(f).Decode(&doc); err != nil {
			return nil, err
		}
		if doc.Version != 1 {
			return nil, fmt.Errorf("unsupported bookmark JSON version %d", doc.Version)
		}
		var out []*model.Node
		for _, g := range doc.Groups {
			nodes, err := fromDTOs(g.Nodes)
			if err != nil {
				return nil, fmt.Errorf("group %q: %w", g.Title, err)
			}
			if g.Title == "" {
				out = append(out, nodes...)
				continue
			}
			out = append(out, &model.Node{
				Kind:     model.Folder,
				Title:    g.Title,
				Children: nodes,
			})
		}
		return out, nil
	}
	return netscape.Parse(f)
}

func fromDTOs(in []nodeDTO) ([]*model.Node, error) {
	out := make([]*model.Node, 0, len(in))
	for i, d := range in {
		switch d.Kind {
		case "link":
			if strings.TrimSpace(d.URL) == "" {
				return nil, fmt.Errorf("node %d is a link without url", i)
			}
			if len(d.Children) > 0 {
				return nil, fmt.Errorf("link %q has children", d.Title)
			}
		case "folder":
			if strings.TrimSpace(d.URL) != "" {
				return nil, fmt.Errorf("folder %q has url", d.Title)
			}
		default:
			return nil, fmt.Errorf("node %d has unknown kind %q", i, d.Kind)
		}
		n := &model.Node{Title: d.Title, URL: strings.TrimSpace(d.URL), Created: d.Created}
		if d.Kind == "folder" {
			n.Kind = model.Folder
			children, err := fromDTOs(d.Children)
			if err != nil {
				return nil, err
			}
			n.Children = children
		} else {
			n.Kind = model.Link
		}
		out = append(out, n)
	}
	return out, nil
}

const topAppsFolder = "top apps"

func buildGroups(space *arc.Space, includeTopApps bool) []netscape.Group {
	groups := []netscape.Group{{Title: "", Nodes: model.FoldersFirst(space.Pinned)}}
	if includeTopApps && len(space.TopApps) > 0 {
		groups = append(groups, netscape.Group{Title: topAppsFolder, Nodes: model.FoldersFirst(space.TopApps)})
	}
	return groups
}

// groupsAsNodes materializes group titles as folders. A group without a title
// contributes its nodes directly; a titled group such as "top apps" becomes a
// real folder on the destination bookmark bar.
func groupsAsNodes(groups []netscape.Group) []*model.Node {
	var out []*model.Node
	for _, g := range groups {
		if g.Title == "" {
			out = append(out, g.Nodes...)
			continue
		}
		out = append(out, &model.Node{Kind: model.Folder, Title: g.Title, Children: g.Nodes})
	}
	return out
}

type nodeDTO struct {
	Kind     string    `json:"kind"`
	Title    string    `json:"title"`
	URL      string    `json:"url,omitempty"`
	Created  float64   `json:"createdAt,omitempty"`
	Children []nodeDTO `json:"children,omitempty"`
}

type groupDTO struct {
	Title string    `json:"title"`
	Nodes []nodeDTO `json:"nodes"`
}

type exportDoc struct {
	Version int        `json:"version"`
	Groups  []groupDTO `json:"groups"`
}

func toDTOGroups(groups []netscape.Group) []groupDTO {
	out := make([]groupDTO, 0, len(groups))
	for _, g := range groups {
		d := groupDTO{Title: g.Title}
		for _, n := range g.Nodes {
			d.Nodes = append(d.Nodes, toDTO(n))
		}
		out = append(out, d)
	}
	return out
}

func toDTO(n *model.Node) nodeDTO {
	d := nodeDTO{Title: n.Title, URL: n.URL, Created: n.Created}
	if n.Kind == model.Folder {
		d.Kind = "folder"
		for _, c := range n.Children {
			d.Children = append(d.Children, toDTO(c))
		}
	} else {
		d.Kind = "link"
	}
	return d
}

func countChrome(n map[string]any) (links, folders int) {
	if asString(n["type"]) == "url" {
		return 1, 0
	}
	folders = 1
	for _, c := range asSlice(n["children"]) {
		if m := asMap(c); m != nil {
			l, f := countChrome(m)
			links += l
			folders += f
		}
	}
	return links, folders
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asSlice(v any) []any        { s, _ := v.([]any); return s }
func asString(v any) string      { s, _ := v.(string); return s }

// reorderArgs lets positional arguments appear before or after flags. Go's
// flag package stops at the first positional argument.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) > 0 && a[0] == '-' && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				continue
			}
			f := fs.Lookup(name)
			if f == nil {
				continue
			}
			if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "arcm:", err)
		os.Exit(1)
	}
}
