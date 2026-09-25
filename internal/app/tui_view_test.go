package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

func viewFixture(count int) tuiModel {
	m := tuiModel{width: 80, height: 24, directory: "/maps", selected: make(map[string]bool)}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("map-%03d.xmind", i)
		m.entries = append(m.entries, browserEntry{name: name, path: "/maps/" + name})
	}
	return m
}

func TestBrowserViewScrollKeepsCursorVisibleAtBounds(t *testing.T) {
	for _, cursor := range []int{-10, 0, 12, 13, 54, 99, 200} {
		t.Run(fmt.Sprint(cursor), func(t *testing.T) {
			m := viewFixture(100)
			m.cursor = cursor
			index := min(max(cursor, 0), 99)
			m.selected[m.entries[index].path] = true
			view := m.View()
			plain := ansi.Strip(view.Content)
			start, end := m.listBounds(len(m.entries))
			if index < start || index >= end || end-start != m.visibleRows() {
				t.Fatalf("cursor %d outside page [%d,%d), rows=%d", index, start, end, m.visibleRows())
			}
			if !strings.Contains(plain, "> [x] "+m.entries[index].name) {
				t.Fatalf("cursor or selection missing:\n%s", plain)
			}
			if !strings.Contains(view.Content, tuiHighlight+"> [x]") || !strings.Contains(plain, "┃") {
				t.Fatal("missing highlighted cursor bar or scrollbar thumb")
			}
			if !strings.Contains(plain, fmt.Sprintf("%d / 100", index+1)) {
				t.Fatalf("missing scroll position:\n%s", plain)
			}
			if !view.AltScreen || view.MouseMode != tea.MouseModeNone {
				t.Fatal("TUI must use an alternate screen with mouse disabled")
			}
		})
	}
}

func TestScrollbarThumbSizeAndEndpoints(t *testing.T) {
	for _, tc := range []struct {
		start int
		want  string
	}{{0, "┃┃┃┃┃│││││"}, {10, "│││││┃┃┃┃┃"}} {
		var track strings.Builder
		for row := 0; row < 10; row++ {
			track.WriteString(ansi.Strip(viewScrollbar(row, 10, 20, tc.start)))
		}
		if got := track.String(); got != tc.want {
			t.Fatalf("start=%d: track=%q, want %q", tc.start, got, tc.want)
		}
	}
	if got := viewScrollbar(0, 10, 10, 0); got != " " {
		t.Fatalf("unnecessary scrollbar: %q", got)
	}
}

func TestBrowserViewportStaysPutWhenMovingWithinPage(t *testing.T) {
	m := viewFixture(100)
	for i := 0; i < 20; i++ {
		updated, _ := m.Update(key(tea.KeyDown))
		m = updated.(tuiModel)
	}
	start, end := m.listBounds(len(m.entries))
	if start == 0 || m.cursor != 20 {
		t.Fatalf("expected scrolled page at cursor 20: start=%d, cursor=%d", start, m.cursor)
	}
	updated, _ := m.Update(key(tea.KeyUp))
	m = updated.(tuiModel)
	nextStart, nextEnd := m.listBounds(len(m.entries))
	if nextStart != start || nextEnd != end {
		t.Fatalf("moving within the page scrolled it: [%d,%d) → [%d,%d)", start, end, nextStart, nextEnd)
	}
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "> [ ] map-019.xmind") || !strings.Contains(plain, "r: refresh") {
		t.Fatalf("missing cursor or refresh control:\n%s", plain)
	}
}

func TestTUIViewFitsTerminalAndKeepsEssentialControls(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 12}, {30, 12}, {120, 40}, {20, 8}, {1, 1}} {
		for _, mode := range []string{"browser", "picker", "ready", "running", "done"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], mode), func(t *testing.T) {
				m := viewFixture(40)
				m.width, m.height, m.cursor = size[0], size[1], 25
				m.entries[25].name = strings.Repeat("文件😀地图", 25) + ".xmind"
				m.entries[25].path = "/" + m.entries[25].name
				m.directory = "/" + strings.Repeat("長いパス/", 30)
				m.opts.outputDir = "/" + strings.Repeat("导出文件/", 30)
				m.selected[m.entries[25].path] = true
				m.message = strings.Repeat("Important message. ", 30)
				switch mode {
				case "picker":
					m.picker = true
				case "ready":
					m.ready = true
				case "running":
					m.running = true
					m.jobs = []job{{input: m.entries[25].path, output: "/out.md"}}
				case "done":
					m.done = true
					m.results = []outcome{{input: "bad.xmind", err: errors.New("bad map")}}
				}
				content := m.View().Content
				lines := strings.Split(content, "\n")
				if len(lines) > m.height {
					t.Fatalf("%d lines exceed terminal height %d:\n%s", len(lines), m.height, content)
				}
				for _, line := range lines {
					if got := ansi.StringWidth(line); got > m.width {
						t.Fatalf("line width %d exceeds %d: %q", got, m.width, line)
					}
				}
				plain := ansi.Strip(content)
				if m.width >= 30 && m.height >= 12 {
					var controls []string
					switch mode {
					case "browser":
						controls = []string{"e", "export", "Space", "o", "output", "q", "quit"}
					case "picker":
						controls = []string{"Space", "folder", "Esc", "back"}
					case "ready":
						controls = []string{"Enter/e", "export", "q/Esc", "quit"}
					case "running":
						controls = []string{"Ctrl+C", "cancel"}
					case "done":
						controls = []string{"b:", "browse", "Enter/q/Esc", "close"}
					}
					for _, text := range controls {
						if !strings.Contains(plain, text) {
							t.Errorf("missing essential control %q:\n%s", text, plain)
						}
					}
				}
			})
		}
	}
}

func TestTUIViewSanitizesAllExternalText(t *testing.T) {
	injection := "\x1b]52;c;clipboard\a\n\r\u202Eevil"
	m := viewFixture(1)
	m.directory = injection
	m.entries[0] = browserEntry{name: injection + ".xmind", path: injection}
	m.opts.outputDir = injection
	m.message = injection
	for _, results := range []bool{false, true} {
		if results {
			m.done = true
			m.results = []outcome{
				{input: injection, err: errors.New(injection)},
				{input: injection, result: export.Result{Output: injection, Warnings: []string{injection}}},
			}
		}
		view := m.View().Content
		if strings.Contains(view, "\x1b]") || strings.ContainsRune(view, '\u202E') {
			t.Fatalf("untrusted terminal sequence in view: %q", view)
		}
		for _, r := range ansi.Strip(view) {
			if r != '\n' && unicode.IsControl(r) {
				t.Fatalf("control character U+%04X in rendered text", r)
			}
		}
	}
}

func TestResultViewScrollsAndShowsCurrentError(t *testing.T) {
	m := viewFixture(0)
	m.done, m.cursor = true, 29
	for i := 0; i < 30; i++ {
		m.results = append(m.results, outcome{input: fmt.Sprintf("/maps/map-%02d.xmind", i), result: export.Result{Output: fmt.Sprintf("/exports/map-%02d.md", i), Topics: i}})
	}
	m.results[29].err = errors.New("destination is not writable")
	plain := ansi.Strip(m.View().Content)
	for _, want := range []string{"Finished 30", "30 / 30", "> ✗ map-29.xmind", "Error: destination is not writable", "b: browse more files"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q:\n%s", want, plain)
		}
	}
}

func TestOutputPickerTakesPrecedenceOverPreparedInputs(t *testing.T) {
	m := viewFixture(0)
	m.picker, m.ready = true, true
	m.selected["/maps/map.xmind"] = true
	m.entries = []browserEntry{{name: "exports", path: "/maps/exports", directory: true}}
	plain := ansi.Strip(m.View().Content)
	for _, want := range []string{"OUTPUT FOLDER", "1 selected", "▸ exports/", "Space: use this folder", "Esc: back"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "[x] map.xmind") {
		t.Fatal("picker must show folders, not the prepared file list")
	}
}
