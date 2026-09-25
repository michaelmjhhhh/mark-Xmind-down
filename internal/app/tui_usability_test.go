package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

// Exercise the controls people actually need at each breakpoint, independently
// of the view helpers' layout decisions. Text must remain useful without color.
func TestUsabilityEssentialActionsFitAtEveryBreakpoint(t *testing.T) {
	for _, width := range []int{30, 40, 60, 80, 120} {
		for _, height := range []int{12, 18, 24} {
			for _, mode := range []string{"file", "selected", "folder", "empty", "ready", "picker", "results"} {
				t.Run(fmt.Sprintf("%dx%d/%s", width, height, mode), func(t *testing.T) {
					m := usabilityFixture(mode, width, height)
					plain := usabilityScreen(t, m)
					lines := strings.Split(plain, "\n")
					footer := strings.Join(lines[len(lines)-3:], "\n")
					if strings.Contains(footer, "…") {
						t.Errorf("a keyboard instruction was truncated:\n%s", footer)
					}
					want := []string{"[?]"}
					switch mode {
					case "file", "selected":
						want = append(want, "[e] Export", "[Space]", "[o] Output", "[q] Quit")
					case "folder":
						want = append(want, "[Enter] Open", "[←] Parent", "[o] Output", "[q] Quit")
					case "empty":
						want = append(want, "[o] Output", "[q] Quit")
						if strings.Contains(footer, "[e] Export") {
							t.Error("empty browser presents Export as an available primary action")
						}
					case "ready":
						want = append(want, "[Enter] Export", "[o] Output", "[q] Quit")
					case "picker":
						want = append(want, "[Space] Use this folder", "[Enter] Open", "[Esc] Back")
					case "results":
						want = append(want, "[b] Browse", "[↑↓] Inspect", "[q] Close")
					}
					for _, text := range want {
						if !strings.Contains(footer, text) {
							t.Errorf("missing complete action %q:\n%s", text, footer)
						}
					}
				})
			}
		}
	}
}

func TestUsabilityHelpReachesEveryBindingAndReturns(t *testing.T) {
	for _, width := range []int{30, 40, 60, 80, 120} {
		for _, height := range []int{12, 18, 24} {
			for _, mode := range []string{"selected", "ready", "picker", "results"} {
				t.Run(fmt.Sprintf("%dx%d/%s", width, height, mode), func(t *testing.T) {
					m := usabilityFixture(mode, width, height)
					before := m.View().Content
					updated, command := m.Update(key('?'))
					m = updated.(tuiModel)
					if !m.help || command != nil {
						t.Fatal("? did not open help without an unrelated command")
					}
					var pages strings.Builder
					for page := 0; page < 100; page++ {
						plain := usabilityScreen(t, m)
						pages.WriteString(plain)
						pages.WriteByte('\n')
						lines := strings.Split(plain, "\n")
						footer := strings.Join(lines[len(lines)-2:], "\n")
						for _, action := range []string{"[↑↓] Scroll", "[Esc] Back", "[?] Back", "[q] Quit"} {
							if !strings.Contains(footer, action) {
								t.Errorf("help lost %q:\n%s", action, footer)
							}
						}
						previous := m.helpOffset
						updated, _ = m.Update(key(tea.KeyPgDown))
						m = updated.(tuiModel)
						if m.helpOffset == previous {
							break
						}
						if page == 99 {
							t.Fatal("help never reached its final page")
						}
					}
					want := []string{"NAVIGATE", "[↑ ↓ / k j]", "[PgUp PgDn / Home End]", "SESSION", "[? / Esc]", "left off"}
					switch mode {
					case "selected":
						want = append(want, "[Enter / → / l]", "[← / Backspace / h]", "SELECT & EXPORT", "[Space / Enter]", "[a]", "[e]", "OUTPUT & TOOLS", "[o]", "[f]", "[r]", "[q / Esc / Ctrl+C]")
					case "ready":
						want = append(want, "EXPORT", "[Enter / e]", "OUTPUT", "[o]", "[f]", "[q / Esc / Ctrl+C]")
					case "picker":
						want = append(want, "[Enter / → / l]", "[← / Backspace / h]", "DESTINATION", "[Space]", "[Esc]", "[r]", "[q / Ctrl+C]")
					case "results":
						want = append(want, "RESULTS", "[b]", "[Enter / q / Esc]", "[Ctrl+C]")
					}
					for _, text := range want {
						if !strings.Contains(pages.String(), text) {
							t.Errorf("help never exposed %q", text)
						}
					}
					updated, _ = m.Update(key(tea.KeyHome))
					m = updated.(tuiModel)
					if m.helpOffset != 0 || !strings.Contains(usabilityScreen(t, m), "NAVIGATE") {
						t.Error("Home did not return to the beginning of help")
					}
					updated, _ = m.Update(key(tea.KeyEnd))
					m = updated.(tuiModel)
					if !strings.Contains(usabilityScreen(t, m), "left off") {
						t.Error("End did not expose the final help instruction")
					}
					updated, command = m.Update(key(tea.KeyEscape))
					m = updated.(tuiModel)
					if m.help || command != nil || m.View().Content != before {
						t.Error("Esc from help did not restore the same underlying screen")
					}
				})
			}
		}
	}
}

func TestUsabilityNoticesExplainMeaningWithoutColor(t *testing.T) {
	for _, tc := range []struct {
		name, word, color string
		result            outcome
	}{
		{"error", "Error:", tuiDanger, outcome{input: "/maps/map.xmind", err: errors.New("destination is not writable")}},
		{"warning", "Warning:", tuiWarning, outcome{input: "/maps/map.xmind", result: export.Result{Output: "/out/map.md", Warnings: []string{"a link target could not be found"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := usabilityFixture("results", 80, 24)
			m.results = []outcome{tc.result}
			view := m.View().Content
			if !strings.Contains(view, tc.color+tc.word) || !strings.Contains(ansi.Strip(view), tc.word) {
				t.Errorf("notice lacks both color and explicit %q wording:\n%s", tc.word, view)
			}
		})
	}
}

func TestSelectAllHintMatchesCurrentFolderAction(t *testing.T) {
	m := viewFixture(2)
	m.selected["/elsewhere/selected.xmind"] = true
	for _, label := range []string{"Select all", "Deselect all", "Select all"} {
		if !strings.Contains(ansi.Strip(m.View().Content), "[a] "+label) {
			t.Fatalf("wrong hint for current folder; expected %q", label)
		}
		updated, _ := m.Update(key('a'))
		m = updated.(tuiModel)
		if !m.selected["/elsewhere/selected.xmind"] {
			t.Fatal("current-folder action cleared another folder's selection")
		}
	}
}

func usabilityFixture(mode string, width, height int) tuiModel {
	m := tuiModel{width: width, height: height, directory: "/maps", selected: make(map[string]bool)}
	m.entries = []browserEntry{{name: "map.xmind", path: "/maps/map.xmind"}}
	switch mode {
	case "selected":
		m.selected["/maps/map.xmind"] = true
	case "folder":
		m.entries = []browserEntry{{name: "nested", path: "/maps/nested", directory: true}}
	case "empty":
		m.entries = nil
	case "ready":
		m.ready = true
		m.selected["/maps/map.xmind"] = true
	case "picker":
		m.picker = true
		m.entries = []browserEntry{{name: "nested", path: "/maps/nested", directory: true}}
	case "results":
		m.done = true
		m.results = []outcome{{input: "/maps/map.xmind", result: export.Result{Output: "/out/map.md", Topics: 3, Images: 1}}}
	}
	return m
}

func usabilityScreen(t *testing.T, m tuiModel) string {
	t.Helper()
	view := m.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeNone {
		t.Error("screen must remain keyboard-only in the alternate terminal")
	}
	lines := strings.Split(view.Content, "\n")
	if len(lines) > m.height {
		t.Fatalf("screen occupies %d lines in a %d-line terminal", len(lines), m.height)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > m.width {
			t.Errorf("line occupies %d columns in a %d-column terminal: %q", width, m.width, ansi.Strip(line))
		}
	}
	return ansi.Strip(view.Content)
}
