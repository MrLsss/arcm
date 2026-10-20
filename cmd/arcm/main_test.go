package main

import "testing"

func TestParseInvocation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		kind    string
		browser browser
		from    browser
		to      browser
		rest    int
		wantErr bool
	}{
		{name: "export chrome", args: []string{"export", "chrome"}, kind: "export", browser: browserChrome},
		{name: "export edge with flags", args: []string{"export", "edge", "--out", "e.html"}, kind: "export", browser: browserEdge, rest: 2},
		{name: "import arc", args: []string{"import", "arc", "--source", "edge"}, kind: "import", browser: browserArc, rest: 2},
		{name: "transfer edge arc", args: []string{"edge", "arc", "--restart"}, kind: "transfer", from: browserEdge, to: browserArc, rest: 1},
		{name: "transfer arc chrome", args: []string{"arc", "chrome"}, kind: "transfer", from: browserArc, to: browserChrome},
		{name: "verify", args: []string{"verify", "edge"}, kind: "verify", rest: 1},
		{name: "version", args: []string{"version"}, kind: "version"},
		{name: "help", args: []string{"help"}, kind: "help"},
		{name: "help export", args: []string{"help", "export"}, kind: "help", rest: 1},
		{name: "short help", args: []string{"-h"}, kind: "help"},
		{name: "long help", args: []string{"--help"}, kind: "help"},
		{name: "export help", args: []string{"export", "--help"}, kind: "help", rest: 1},
		{name: "transfer help", args: []string{"edge", "-h"}, kind: "help", rest: 1},
		{name: "missing command", args: nil, wantErr: true},
		{name: "missing export browser", args: []string{"export"}, wantErr: true},
		{name: "unknown export browser", args: []string{"export", "firefox"}, wantErr: true},
		{name: "unknown command", args: []string{"sync"}, wantErr: true},
		{name: "old edge export", args: []string{"edge", "export"}, wantErr: true},
		{name: "old arc import", args: []string{"arc", "import"}, wantErr: true},
		{name: "same browser", args: []string{"arc", "arc"}, wantErr: true},
		{name: "missing destination", args: []string{"arc"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, err := parseInvocation(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseInvocation(%v) succeeded, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseInvocation(%v): %v", tt.args, err)
			}
			if inv.kind != tt.kind || inv.browser != tt.browser || inv.from != tt.from || inv.to != tt.to {
				t.Fatalf("got kind=%q browser=%q from=%q to=%q", inv.kind, inv.browser, inv.from, inv.to)
			}
			if len(inv.rest) != tt.rest {
				t.Fatalf("rest = %v, want %d items", inv.rest, tt.rest)
			}
		})
	}
}

func TestParseBrowserIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"ARC", "Chrome", " edge "} {
		if _, err := parseBrowser(name); err != nil {
			t.Errorf("parseBrowser(%q): %v", name, err)
		}
	}
}

func TestDefaultOutput(t *testing.T) {
	if got := defaultOutput(browserArc); got != "arc-bookmarks.html" {
		t.Fatalf("arc output = %q", got)
	}
	if got := defaultOutput(browserChrome); got != "chrome-bookmarks.html" {
		t.Fatalf("chrome output = %q", got)
	}
	if got := defaultOutput(browserEdge); got != "edge-bookmarks.html" {
		t.Fatalf("edge output = %q", got)
	}
}
