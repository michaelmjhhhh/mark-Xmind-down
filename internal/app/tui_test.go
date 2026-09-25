package app

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	model, _ = m.Update(listingMsg{directory: "missing", err: errors.New("permission denied"), request: m.listingRequest})
	m = model.(tuiModel)
	if m.directory != filepath.Join(dir, "nested") || !strings.Contains(m.message, "permission denied") {
		t.Fatal("failed navigation should preserve directory and display error")
	}
}

func TestBrowserExportsHighlightedFileWhenNothingSelected(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, dir, "a.xmind")
	b := fixture(t, dir, "b.xmind")
	for _, selected := range []bool{false, true} {
		name := "highlighted"
		if selected {
			name = "selection takes precedence"
		}
		t.Run(name, func(t *testing.T) {
			want := b
			selection := map[string]bool{}
			if selected {
				selection[a] = true
				want = a
			}
			var got []string
			m := tuiModel{
				ctx: context.Background(), selected: selection, cursor: 1,
				entries: []browserEntry{{name: "a.xmind", path: a}, {name: "b.xmind", path: b}},
				convert: func(_ context.Context, input string, _ export.Options) (export.Result, error) {
					got = append(got, input)
					return export.Result{}, nil
				},
			}
			model, cmd := m.Update(key('e'))
			m = model.(tuiModel)
			if !m.running || cmd == nil || len(m.jobs) != 1 || m.jobs[0].input != want {
				t.Fatalf("expected one job for %s, got %+v", want, m.jobs)
			}
			if len(m.selected) != len(selection) || (!selected && len(m.selected) != 0) {
				t.Fatal("quick export must not change selection")
			}
			model, next := m.Update(cmd())
			m = model.(tuiModel)
			if len(got) != 1 || got[0] != want || !m.done || next != nil {
				t.Fatalf("converted %v, want only %s", got, want)
			}
		})
	}
}

func TestBrowserQuickExportRequiresHighlightedFile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []browserEntry
	}{
		{name: "empty directory"},
		{name: "highlighted directory", entries: []browserEntry{{name: "maps", path: "maps", directory: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tuiModel{selected: map[string]bool{}, entries: tc.entries}
			model, cmd := m.Update(key('e'))
			m = model.(tuiModel)
			if m.running || cmd != nil || len(m.jobs) != 0 || !strings.Contains(m.message, "Highlight a .xmind file") {
				t.Fatalf("expected actionable message without export: %+v", m)
			}
		})
	}
}

func TestTUIOverwriteToggleBeforeExport(t *testing.T) {
	path := fixture(t, t.TempDir(), "map.xmind")
	for _, ready := range []bool{false, true} {
		name := "browser"
		if ready {
			name = "prepared inputs"
		}
		t.Run(name, func(t *testing.T) {
			var got []bool
			m := tuiModel{
				ctx: context.Background(), ready: ready, width: 80, height: 24,
				selected: map[string]bool{path: true},
				convert: func(_ context.Context, _ string, opts export.Options) (export.Result, error) {
					got = append(got, opts.Force)
					return export.Result{}, nil
				},
			}
			for _, want := range []bool{true, false} {
				model, cmd := m.Update(key('f'))
				m = model.(tuiModel)
				if cmd != nil || m.opts.force != want {
					t.Fatalf("force=%v, want %v", m.opts.force, want)
				}
				state := "Overwrite: off"
				if want {
					state = "Overwrite: on"
				}
				view := m.View().Content
				if !strings.Contains(view, state) || !strings.Contains(view, "f: toggle overwrite") {
					t.Fatalf("missing overwrite state or hint: %s", view)
				}
				started, exportCmd := m.Update(key('e'))
				if exportCmd == nil || !started.(tuiModel).running {
					t.Fatal("expected export command")
				}
				exportCmd()
				if got[len(got)-1] != want {
					t.Fatalf("converter force=%v, want %v", got[len(got)-1], want)
				}
			}
		})
	}
}

func TestTUIOverwriteToggleIgnoredAfterExportStarts(t *testing.T) {
	for _, state := range []string{"running", "done"} {
		t.Run(state, func(t *testing.T) {
			m := tuiModel{running: state == "running", done: state == "done"}
			model, cmd := m.Update(key('f'))
			m = model.(tuiModel)
			if m.opts.force || cmd != nil {
				t.Fatal("overwrite must not change after export starts")
			}
		})
	}
}

func TestBrowserHintsFitStandardTerminal(t *testing.T) {
	m := tuiModel{width: 80, height: 24, selected: map[string]bool{}, message: "Highlight a .xmind file or select files with Space."}
	for range 30 {
		m.entries = append(m.entries, browserEntry{name: "map.xmind", path: "map.xmind"})
	}
	view := m.View().Content
	for _, hint := range []string{"e: export", "a: select all", "←: parent", "f: toggle overwrite", "q/Esc: quit"} {
		if !strings.Contains(view, hint) {
			t.Errorf("missing hint %q in standard terminal: %s", hint, view)
		}
	}
	if strings.Count(view, "\n") > m.height {
		t.Fatalf("view has %d lines for %d-row terminal", strings.Count(view, "\n"), m.height)
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

func TestTUIKeyboardPagingAndClamping(t *testing.T) {
	m := tuiModel{width: 80, height: 24, selected: map[string]bool{}}
	for i := range 60 {
		m.entries = append(m.entries, browserEntry{name: fmt.Sprintf("%02d.xmind", i)})
	}
	for _, step := range []struct {
		code rune
		want int
	}{
		{tea.KeyPgDown, m.visibleRows()},
		{tea.KeyPgUp, 0},
		{tea.KeyEnd, 59},
		{tea.KeyDown, 59},
		{tea.KeyHome, 0},
		{tea.KeyUp, 0},
		{'j', 1},
		{'k', 0},
	} {
		model, cmd := m.Update(key(step.code))
		m = model.(tuiModel)
		if cmd != nil || m.cursor != step.want {
			t.Fatalf("key %s: cursor=%d, want %d", key(step.code).String(), m.cursor, step.want)
		}
		start, end := m.listBounds(len(m.entries))
		if m.cursor < start || m.cursor >= end {
			t.Fatalf("cursor %d outside viewport [%d,%d)", m.cursor, start, end)
		}
	}
}

func TestTUIFileSelectionUsesEnterAndSpace(t *testing.T) {
	path := fixture(t, t.TempDir(), "map.xmind")
	m := tuiModel{entries: []browserEntry{{name: "map.xmind", path: path}}, selected: map[string]bool{}}
	model, cmd := m.Update(key(tea.KeyEnter))
	m = model.(tuiModel)
	if cmd != nil || !m.selected[path] || m.running {
		t.Fatal("Enter on a file should select it without starting conversion")
	}
	model, cmd = m.Update(key(tea.KeySpace))
	m = model.(tuiModel)
	if cmd != nil || m.selected[path] {
		t.Fatal("Space should toggle file selection")
	}
}

func TestTUIOutputPickerPreservesSourceAndSetsDestination(t *testing.T) {
	dir := t.TempDir()
	path := fixture(t, dir, "map.xmind")
	destination := filepath.Join(dir, "output")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	m := tuiModel{directory: dir, selected: map[string]bool{path: true}, opts: options{output: filepath.Join(dir, "original.md")}}
	model, _ := m.Update(readDirectory(dir)())
	m = model.(tuiModel)
	m.cursor = 2 // .., output/, map.xmind
	model, cmd := m.Update(key('o'))
	m = model.(tuiModel)
	if !m.picker || !m.loading || cmd == nil {
		t.Fatal("o must open an asynchronous output folder picker")
	}
	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	for _, entry := range m.entries {
		if !entry.directory {
			t.Fatalf("output picker contains file %+v", entry)
		}
	}
	m.cursor = 1
	model, cmd = m.Update(key(tea.KeyEnter))
	m = model.(tuiModel)
	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	if m.directory != destination {
		t.Fatalf("picker directory=%s, want %s", m.directory, destination)
	}
	model, cmd = m.Update(key(tea.KeySpace))
	m = model.(tuiModel)
	if cmd != nil || m.picker || m.directory != dir || m.cursor != 2 || len(m.entries) != 3 || !m.selected[path] {
		t.Fatalf("choosing output must restore source browser and selection: %+v", m)
	}
	if m.opts.output != "" || m.opts.outputDir != destination {
		t.Fatalf("bad destination options: %+v", m.opts)
	}
}

func TestTUIOutputPickerCancelDiscardsPendingListing(t *testing.T) {
	dir := t.TempDir()
	path := fixture(t, dir, "map.xmind")
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprintf("ready=%v", ready), func(t *testing.T) {
			m := tuiModel{directory: dir, selected: map[string]bool{path: true}, ready: ready, opts: options{output: filepath.Join(dir, "custom.md")}}
			model, _ := m.Update(readDirectory(dir)())
			m = model.(tuiModel)
			m.cursor = 1
			if ready {
				m.cursor = 0
			}
			wantCursor := m.cursor
			model, pending := m.Update(key('o'))
			m = model.(tuiModel)
			model, quit := m.Update(key(tea.KeyEscape))
			m = model.(tuiModel)
			if quit != nil || m.picker || m.loading || m.ready != ready || m.cursor != wantCursor || len(m.entries) != 2 || m.opts.output == "" {
				t.Fatalf("Escape should restore source without changing output: %+v", m)
			}
			model, _ = m.Update(pending())
			m = model.(tuiModel)
			if m.cursor != wantCursor || len(m.entries) != 2 || !m.selected[path] {
				t.Fatal("stale output listing changed the restored browser")
			}
		})
	}
}

func TestTUIRefreshKeepsSelectionAndClampsCursor(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, dir, "a.xmind")
	b := fixture(t, dir, "b.xmind")
	m := tuiModel{directory: dir, selected: map[string]bool{a: true}}
	model, _ := m.Update(readDirectory(dir)())
	m = model.(tuiModel)
	m.cursor = 2
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	model, cmd := m.Update(key('r'))
	m = model.(tuiModel)
	if cmd == nil || !m.loading {
		t.Fatal("refresh should read the directory asynchronously")
	}
	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	if m.loading || m.cursor != 1 || len(m.entries) != 2 || !m.selected[a] {
		t.Fatalf("refresh should preserve selection and clamp cursor: %+v", m)
	}
}

func TestTUIResultsScrollAndSecondBatchKeepsAllOutcomes(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, dir, "a.xmind")
	b := fixture(t, dir, "b.xmind")
	var calls []string
	m := tuiModel{ctx: context.Background(), directory: dir, selected: map[string]bool{a: true, b: true}, width: 80, height: 24,
		convert: func(_ context.Context, input string, opts export.Options) (export.Result, error) {
			calls = append(calls, input)
			return export.Result{Output: opts.Output}, nil
		},
	}
	model, _ := m.Update(readDirectory(dir)())
	m = model.(tuiModel)
	m.cursor = 2
	model, cmd := m.Update(key('e'))
	m = model.(tuiModel)
	for cmd != nil {
		model, cmd = m.Update(cmd())
		m = model.(tuiModel)
	}
	if !m.done || m.cursor != 1 {
		t.Fatalf("first batch incomplete: %+v", m)
	}
	model, _ = m.Update(key(tea.KeyHome))
	m = model.(tuiModel)
	if m.cursor != 0 {
		t.Fatal("result rows must support keyboard scrolling")
	}
	model, cmd = m.Update(key('b'))
	m = model.(tuiModel)
	if m.done || m.ready || !m.loading || len(m.selected) != 0 || len(m.results) != 0 || len(m.jobs) != 0 || len(m.history) != 2 {
		t.Fatalf("back should reset batch and preserve history: %+v", m)
	}
	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	if m.cursor != 2 {
		t.Fatalf("back should restore browser highlight, got %d", m.cursor)
	}
	model, cmd = m.Update(key('e'))
	m = model.(tuiModel)
	if cmd == nil || len(m.jobs) != 1 || m.jobs[0].input != b {
		t.Fatal("second batch should export highlighted file without stale jobs")
	}
	model, cmd = m.Update(cmd())
	m = model.(tuiModel)
	if cmd != nil || !m.done || len(m.results) != 1 || len(m.allResults()) != 3 || len(calls) != 3 || calls[2] != b {
		t.Fatalf("second batch did not finish cleanly: %+v, calls=%v", m, calls)
	}
}

func TestTUIMouseMessagesDoNothing(t *testing.T) {
	m := tuiModel{entries: []browserEntry{{name: "map.xmind", path: "map.xmind"}}, selected: map[string]bool{}}
	for _, msg := range []tea.Msg{tea.MouseClickMsg{}, tea.MouseWheelMsg{}, tea.MouseMotionMsg{}} {
		model, cmd := m.Update(msg)
		m = model.(tuiModel)
		if cmd != nil || m.cursor != 0 || len(m.selected) != 0 || m.running || m.picker {
			t.Fatalf("mouse input changed keyboard-only browser: %+v", m)
		}
	}
}

func TestTUIViewportScrollsOnlyAtArrowBoundaries(t *testing.T) {
	m := tuiModel{width: 80, height: 24, entries: make([]browserEntry, 80)}
	rows := m.visibleRows()
	for range rows + 3 {
		model, _ := m.Update(key(tea.KeyDown))
		m = model.(tuiModel)
	}
	if m.offset != 4 || m.cursor != rows+3 {
		t.Fatalf("unexpected scrolled position: cursor=%d, offset=%d", m.cursor, m.offset)
	}
	model, _ := m.Update(key(tea.KeyUp))
	m = model.(tuiModel)
	if m.offset != 4 || m.cursor != rows+2 {
		t.Fatalf("Up within viewport must keep page still: cursor=%d, offset=%d", m.cursor, m.offset)
	}
	for m.cursor > m.offset {
		model, _ = m.Update(key(tea.KeyUp))
		m = model.(tuiModel)
	}
	model, _ = m.Update(key(tea.KeyUp))
	m = model.(tuiModel)
	if m.cursor != 3 || m.offset != 3 {
		t.Fatalf("Up across viewport top must scroll one row: cursor=%d, offset=%d", m.cursor, m.offset)
	}
}

func TestTUIPageKeysMoveEntireViewport(t *testing.T) {
	m := tuiModel{width: 80, height: 24, entries: make([]browserEntry, 80)}
	rows := m.visibleRows()
	for _, step := range []struct {
		code           rune
		cursor, offset int
	}{
		{tea.KeyPgDown, rows, rows},
		{tea.KeyPgDown, rows * 2, rows * 2},
		{tea.KeyPgUp, rows, rows},
		{tea.KeyHome, 0, 0},
		{tea.KeyPgUp, 0, 0},
		{tea.KeyEnd, 79, 80 - rows},
		{tea.KeyPgDown, 79, 80 - rows},
	} {
		model, _ := m.Update(key(step.code))
		m = model.(tuiModel)
		if m.cursor != step.cursor || m.offset != step.offset {
			t.Fatalf("%s: cursor/offset=%d/%d, want %d/%d", key(step.code).String(), m.cursor, m.offset, step.cursor, step.offset)
		}
	}
}

func TestTUIOutputPickerRestoresViewportAfterResize(t *testing.T) {
	for _, height := range []int{24, 20, 12} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			m := tuiModel{directory: t.TempDir(), width: 80, height: 24, entries: make([]browserEntry, 60), cursor: 25, offset: 20}
			model, pending := m.Update(key('o'))
			m = model.(tuiModel)
			model, _ = m.Update(pending())
			m = model.(tuiModel)
			model, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
			m = model.(tuiModel)
			model, _ = m.Update(key(tea.KeyEscape))
			m = model.(tuiModel)
			wantOffset := max(20, 25-m.visibleRows()+1)
			if m.picker || m.cursor != 25 || m.offset != wantOffset {
				t.Fatalf("source viewport not restored/clamped: cursor=%d offset=%d, want offset=%d", m.cursor, m.offset, wantOffset)
			}
			start, end := m.listBounds(len(m.entries))
			if start != wantOffset || m.cursor < start || m.cursor >= end {
				t.Fatalf("restored viewport [%d,%d) misses cursor %d", start, end, m.cursor)
			}
		})
	}
}

func TestTUIRefreshAndBatchReturnRestoreViewport(t *testing.T) {
	dir := t.TempDir()
	for i := range 35 {
		fixture(t, dir, fmt.Sprintf("%02d.xmind", i))
	}
	m := tuiModel{ctx: context.Background(), directory: dir, selected: map[string]bool{}, width: 80, height: 24,
		convert: func(_ context.Context, _ string, opts export.Options) (export.Result, error) {
			return export.Result{Output: opts.Output}, nil
		},
	}
	model, _ := m.Update(readDirectory(dir)())
	m = model.(tuiModel)
	m.cursor, m.offset = 25, 20
	model, cmd := m.Update(key('r'))
	m = model.(tuiModel)
	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	if m.cursor != 25 || m.offset != 20 {
		t.Fatal("refresh moved unchanged viewport")
	}
	model, cmd = m.Update(key('e'))
	m = model.(tuiModel)
	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	model, cmd = m.Update(key('b'))
	m = model.(tuiModel)
	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	if m.cursor != 25 || m.offset != 20 || len(m.allResults()) != 1 {
		t.Fatalf("return from results moved source viewport or lost result: %+v", m)
	}
}
