package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

// Exercise the complete selection -> destination picker -> export -> browse
// workflow using the real supplied archives, without typing any file paths.
func TestKeyboardWorkflowExportsRealSamples(t *testing.T) {
	root := t.TempDir()
	maps := filepath.Join(root, "maps")
	output := filepath.Join(root, "exported")
	for _, dir := range []string{maps, output} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	samples, err := filepath.Glob(filepath.Join("..", "..", "assets", "*.xmind"))
	if err != nil || len(samples) != 3 {
		t.Fatalf("expected three sample archives: %v, %v", samples, err)
	}
	for _, sample := range samples {
		data, err := os.ReadFile(sample)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(maps, filepath.Base(sample)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := tuiModel{ctx: ctx, cancel: cancel, directory: maps, selected: map[string]bool{}, width: 80, height: 24, convert: export.Convert}
	apply := func(msg tea.Msg) {
		t.Helper()
		var cmd tea.Cmd
		model, cmd := m.Update(msg)
		m = model.(tuiModel)
		for cmd != nil {
			model, next := m.Update(cmd())
			m, cmd = model.(tuiModel), next
		}
	}
	apply(readDirectory(maps)())
	apply(key('a'))
	if len(m.selected) != 3 {
		t.Fatalf("selected %d files", len(m.selected))
	}
	apply(key('o'))
	apply(key(tea.KeyLeft))
	for m.cursor < len(m.entries) && m.entries[m.cursor].name != "exported" {
		if m.cursor == len(m.entries)-1 {
			t.Fatal("output folder not shown")
		}
		apply(key(tea.KeyDown))
	}
	apply(key(tea.KeyEnter))
	apply(key(tea.KeySpace))
	if m.picker || m.opts.outputDir != output || len(m.selected) != 3 {
		t.Fatalf("output choice lost selection or destination: %+v", m)
	}
	apply(key('e'))
	if !m.done || len(m.results) != 3 {
		t.Fatalf("batch did not finish: %+v", m.results)
	}
	topics, images := 0, 0
	for _, result := range m.results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if filepath.Dir(result.result.Output) != output {
			t.Fatalf("wrong output: %s", result.result.Output)
		}
		topics += result.result.Topics
		images += result.result.Images
		markdown, err := os.ReadFile(result.result.Output)
		if err != nil || !strings.Contains(string(markdown), "assets/") {
			t.Fatalf("missing Markdown or image links: %v", err)
		}
	}
	assets, err := os.ReadDir(filepath.Join(output, "assets"))
	if err != nil || topics != 411 || images != 37 || len(assets) != 37 {
		t.Fatalf("topics=%d images=%d assets=%d err=%v", topics, images, len(assets), err)
	}
	apply(key('b'))
	if m.done || len(m.selected) != 0 || len(m.allResults()) != 3 {
		t.Fatal("returning to browse must retain completed exports and clear selection")
	}
}
