package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestBrowserSelectionAndAsyncConversion(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, dir, "a.xmind")
	b := fixture(t, dir, "b.xmind")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := 0
	m := tuiModel{ctx: ctx, cancel: cancel, directory: dir, selected: make(map[string]bool), width: 80, height: 24, convert: func(_ context.Context, input string, opts export.Options) (export.Result, error) {
		called++
		if input == a {
			return export.Result{}, errors.New("bad map")
		}
		return export.Result{Output: opts.Output, Topics: 7}, nil
	}}
	model, _ := m.Update(readDirectory(dir)())
	m = model.(tuiModel)
	if len(m.entries) != 3 || !m.entries[0].directory {
		t.Fatalf("entries: %+v", m.entries)
	}
	model, _ = m.Update(key('a'))
	m = model.(tuiModel)
	if !m.selected[a] || !m.selected[b] {
		t.Fatal("select all did not select files")
	}
	model, cmd := m.Update(key('e'))
	m = model.(tuiModel)
	if !m.running || cmd == nil || called != 0 {
		t.Fatal("conversion must run in an async command")
	}
	model, cmd = m.Update(cmd())
	m = model.(tuiModel)
	if len(m.results) != 1 || m.results[0].err == nil || cmd == nil {
		t.Fatal("must continue after conversion error")
	}
	model, cmd = m.Update(cmd())
	m = model.(tuiModel)
	if !m.done || m.running || len(m.results) != 2 || called != 2 || cmd != nil {
		t.Fatalf("bad completion: %+v", m)
	}
	if view := m.View().Content; !strings.Contains(view, "Finished 2") || !strings.Contains(view, "bad map") {
		t.Fatalf("view=%s", view)
	}
}

func TestBrowserDirectoryNavigationAndErrors(t *testing.T) {
	dir := t.TempDir()
	fixture(t, dir, "nested/map.xmind")
	m := tuiModel{directory: dir, selected: map[string]bool{}}
	model, _ := m.Update(readDirectory(dir)())
	m = model.(tuiModel)
	m.cursor = 1
	model, cmd := m.Update(key(tea.KeyEnter))
	m = model.(tuiModel)
	if !m.loading || cmd == nil {
		t.Fatal("directory open should issue a command")
	}
	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	if m.directory != filepath.Join(dir, "nested") {
		t.Fatalf("directory=%s", m.directory)
	}
	model, _ = m.Update(listingMsg{directory: "missing", err: errors.New("permission denied")})
	m = model.(tuiModel)
	if m.directory != filepath.Join(dir, "nested") || !strings.Contains(m.message, "permission denied") {
		t.Fatal("failed navigation should preserve directory and display error")
	}
}

func TestTUISelectionValidationAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := tuiModel{ctx: ctx, cancel: cancel, selected: map[string]bool{}, width: 80, height: 24}
	model, cmd := m.Update(key('e'))
	m = model.(tuiModel)
	if cmd != nil || m.message == "" {
		t.Fatal("empty selection must not export")
	}
	m.running = true
	model, cmd = m.Update(key('q'))
	m = model.(tuiModel)
	if !m.canceled || ctx.Err() == nil || cmd != nil {
		t.Fatal("cancel must stop worker before exiting")
	}
	model, cmd = m.Update(convertedMsg{err: context.Canceled})
	m = model.(tuiModel)
	if m.running || cmd == nil {
		t.Fatal("worker completion should quit canceled session")
	}
}

func TestTUIRejectsSelectedOutputCollisions(t *testing.T) {
	dir := t.TempDir()
	m := tuiModel{selected: map[string]bool{filepath.Join(dir, "a/Map.xmind"): true, filepath.Join(dir, "b/map.xmind"): true}, opts: options{outputDir: dir}}
	model, cmd := m.start()
	m = model.(tuiModel)
	if m.running || cmd != nil || !strings.Contains(m.message, "collision") {
		t.Fatalf("bad preflight: %+v", m)
	}
}

func TestTUIParentCancellationWaitsForActiveWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := tuiModel{ctx: ctx, cancel: cancel, ready: true, running: true}
	watch := m.Init()
	cancel()
	model, cmd := m.Update(watch())
	m = model.(tuiModel)
	if !m.canceled || !m.running || cmd != nil {
		t.Fatal("parent cancellation must await active converter cleanup")
	}
	model, cmd = m.Update(convertedMsg{err: context.Canceled})
	m = model.(tuiModel)
	if m.running || cmd == nil || len(m.results) != 1 {
		t.Fatal("converter completion should retain result and quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected quit command")
	}
}
