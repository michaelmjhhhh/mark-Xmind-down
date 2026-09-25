package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	tuiReset     = "\x1b[0m"
	tuiAccent    = "\x1b[1;36m"
	tuiMuted     = "\x1b[37m"
	tuiHighlight = "\x1b[1;7m"
)

type viewRow struct {
	text, detail string
}

// Keep navigation and rendering on the same page size, including compact
// terminals. The reserved lines contain the destination and keyboard controls.
func (m tuiModel) visibleRows() int {
	if m.height < 18 {
		return max(1, m.height-10)
	}
	return max(1, m.height-11)
}

func (m tuiModel) listBounds(total int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	rows := m.visibleRows()
	cursor := min(max(m.cursor, 0), total-1)
	start := min(max(0, m.offset), max(0, total-rows))
	if cursor < start {
		start = cursor
	} else if cursor >= start+rows {
		start = cursor - rows + 1
	}
	return start, min(total, start+rows)
}

func (m tuiModel) View() tea.View {
	width, height := max(1, m.width), max(1, m.height)
	if width < 30 || height < 12 {
		lines := []string{"XMind → Markdown", "Resize to at least 30 × 12", "q / Esc / Ctrl+C: quit"}
		if m.picker {
			lines[2] = "Esc: back · Ctrl+C: quit"
		}
		if m.running {
			lines[2] = "q / Esc / Ctrl+C: cancel"
		}
		return tuiScreen(lines[:min(height, len(lines))], width)
	}

	mode, label, location, rows := m.viewRows()
	output := "beside each input"
	if m.opts.output != "" {
		output = safe(m.opts.output)
	} else if m.opts.outputDir != "" {
		output = safe(m.opts.outputDir)
	}
	force := "off"
	if m.opts.force {
		force = "on"
	}
	status := fmt.Sprintf("%d selected  ·  Overwrite: %s", len(m.selected), force)
	if m.running || m.done {
		succeeded := 0
		for _, result := range m.results {
			if result.err == nil {
				succeeded++
			}
		}
		status = fmt.Sprintf("%d exported  ·  %d failed  ·  Overwrite: %s", succeeded, len(m.results)-succeeded, force)
	}
	lines := []string{
		viewHeading("XMind → Markdown", mode, width),
		" Output: " + output,
		" " + status,
		" " + location,
	}

	// The frame leaves one column of margin on both sides. Its rightmost inner
	// column is reserved for a proportional scrollbar, independent of filenames.
	paneWidth := width - 2
	textWidth := paneWidth - 5
	lines = append(lines, " "+viewBorder("╭", "╮", label, paneWidth))
	start, end := m.listBounds(len(rows))
	cursor := min(max(0, m.cursor), max(0, len(rows)-1))
	if m.running && len(rows) > 0 {
		// Follow the current conversion while keyboard input is reserved for cancel.
		follow := m
		follow.cursor = len(rows) - 1
		start, end = follow.listBounds(len(rows))
		cursor = len(rows) - 1
	}
	for row := 0; row < m.visibleRows(); row++ {
		index := start + row
		text, highlighted := "", false
		if index < end {
			highlighted = index == cursor
			prefix := "  "
			if highlighted {
				prefix = "> "
			}
			text = prefix + rows[index].text
		} else if len(rows) == 0 && row == 0 {
			text = m.emptyMessage()
		}
		text = viewPad(text, textWidth)
		if highlighted {
			text = tuiHighlight + text + tuiReset
		}
		scroll := viewScrollbar(row, m.visibleRows(), len(rows), start)
		lines = append(lines, " "+tuiMuted+"│"+tuiReset+" "+text+" "+scroll+tuiMuted+"│"+tuiReset)
	}
	position := "0 items"
	if len(rows) > 0 {
		position = fmt.Sprintf("%d / %d", cursor+1, len(rows))
		if len(rows) > m.visibleRows() {
			position += fmt.Sprintf("  ·  showing %d–%d", start+1, end)
		}
	}
	lines = append(lines, " "+viewBorder("╰", "╯", position, paneWidth))
	detail := "Images are saved in assets/ beside the Markdown."
	if len(rows) > 0 {
		detail = rows[cursor].detail
	}
	if m.picker {
		detail = "Choose current folder: " + safe(m.directory)
	}
	message := m.message
	if message == "" {
		switch {
		case m.picker:
			message = "Space chooses this folder; Esc keeps your current output location."
		case m.done:
			message = "b opens the browser again. Full results and warnings print on exit."
		case m.running && m.canceled:
			message = "Canceling… waiting for the current export to finish cleanup."
		case m.running:
			message = "Exporting Markdown and images…"
		case m.ready:
			message = "Enter exports these files. Choose another destination with o."
		default:
			message = "e exports the highlighted file, or all selected files."
		}
	}
	if height >= 18 {
		lines = append(lines, " "+tuiMuted+detail+tuiReset, " "+safe(message))
	} else if m.message != "" || m.running {
		lines = append(lines, " "+safe(message))
	} else {
		lines = append(lines, " "+tuiMuted+detail+tuiReset)
	}
	lines = append(lines, m.viewFooter(width)...)
	return tuiScreen(lines, width)
}

func (m tuiModel) viewRows() (mode, label, location string, rows []viewRow) {
	switch {
	case m.running || m.done:
		mode, label = "EXPORTING", "Export progress"
		location = fmt.Sprintf("Exporting %d / %d", min(len(m.results)+1, len(m.jobs)), len(m.jobs))
		if m.done {
			mode, label = "COMPLETE", "Export results"
			location = fmt.Sprintf("Finished %d file(s)", len(m.results))
		}
		for _, result := range m.results {
			if result.err != nil {
				rows = append(rows, viewRow{
					text:   "✗ " + safe(filepath.Base(result.input)) + " · " + safe(result.err.Error()),
					detail: "Error: " + safe(result.err.Error()),
				})
				continue
			}
			text := fmt.Sprintf("✓ %s · %d topics, %d images", safe(filepath.Base(result.result.Output)), result.result.Topics, result.result.Images)
			detail := "Saved: " + safe(result.result.Output)
			if len(result.result.Warnings) > 0 {
				text += fmt.Sprintf(" · %d warnings", len(result.result.Warnings))
				detail = "Warning: " + safe(result.result.Warnings[0])
			}
			rows = append(rows, viewRow{text: text, detail: detail})
		}
		if m.running && len(m.results) < len(m.jobs) {
			current := m.jobs[len(m.results)]
			rows = append(rows, viewRow{text: "… " + safe(filepath.Base(current.input)), detail: "Writing: " + safe(current.output)})
		}
	case m.picker:
		mode, label, location = "OUTPUT FOLDER", "Folders", "Folder: "+safe(m.directory)
		for _, entry := range m.entries {
			rows = append(rows, viewRow{text: "▸ " + safe(entry.name) + "/", detail: "Folder: " + safe(entry.path)})
		}
	case m.ready:
		mode, label, location = "READY", "Selected files", fmt.Sprintf("%d file(s) ready to export", len(m.selected))
		for _, path := range m.selectedPaths() {
			rows = append(rows, viewRow{text: "[x] " + safe(filepath.Base(path)), detail: "File: " + safe(path)})
		}
	default:
		mode, label, location = "BROWSE", "XMind files", "Folder: "+safe(m.directory)
		for _, entry := range m.entries {
			text, kind := "[ ] "+safe(entry.name), "File: "
			if entry.directory {
				text, kind = " ▸  "+safe(entry.name)+"/", "Folder: "
			} else if m.selected[entry.path] {
				text = "[x] " + safe(entry.name)
			}
			rows = append(rows, viewRow{text: text, detail: kind + safe(entry.path)})
		}
	}
	return
}

func (m tuiModel) emptyMessage() string {
	if m.loading {
		return "Loading…"
	}
	if m.picker {
		return "No subfolders. Space chooses this folder."
	}
	if m.running || m.done {
		return "Waiting for export…"
	}
	return "No .xmind files or subfolders here."
}

func (m tuiModel) viewFooter(width int) []string {
	var lines []string
	switch {
	case m.running:
		lines = []string{"q / Esc / Ctrl+C: cancel", "", ""}
	case m.done:
		lines = []string{"↑/↓: inspect · PgUp/PgDn: page · Home/End: jump", "b: browse more files", "Enter/q/Esc: close"}
	case m.picker:
		lines = []string{"↑/↓: move · PgUp/PgDn: page · Home/End: jump", "Enter/→: open folder · ←: parent", "Space: use this folder · Esc: back · Ctrl+C: quit"}
	case m.ready:
		lines = []string{"↑/↓: move · PgUp/PgDn: page · Home/End: jump", "Enter/e: export · o: output folder", "f: toggle overwrite · q/Esc: quit"}
	default:
		lines = []string{"↑/↓: move · PgUp/PgDn: page · Home/End: jump · ←: parent", "Enter: open/select · Space: select · a: select all · e: export", "o: output folder · f: toggle overwrite · q/Esc: quit"}
		if width >= 75 {
			lines[0] += " · r: refresh"
		}
	}
	if width < 65 {
		switch {
		case m.running:
			lines = []string{"q / Esc / Ctrl+C: cancel", "", ""}
		case m.done:
			lines = []string{"↑↓: inspect · PgUp/PgDn: page", "b: browse more files", "Enter/q/Esc: close"}
		case m.picker:
			lines = []string{"↑↓: move · Enter: open", "Space: use current folder", "←: parent · Esc: back"}
		case m.ready:
			lines = []string{"↑↓: move · Enter/e: export", "o: output · f: overwrite", "q/Esc: quit"}
		default:
			lines = []string{"↑↓ move · Enter open/select", "Space select · e export · o output", "← parent · f overwrite · q quit"}
			if width < 35 {
				lines = []string{"↑↓ move · Enter open/select", "Space select · e export", "o output · f force · q quit"}
			}
		}
	}
	for i := range lines {
		lines[i] = " " + tuiMuted + lines[i] + tuiReset
	}
	return lines
}

func viewHeading(title, mode string, width int) string {
	available := width - 2
	if ansi.StringWidth(title)+ansi.StringWidth(mode)+2 > available {
		return " " + tuiAccent + title + tuiReset
	}
	return " " + tuiAccent + title + tuiReset + strings.Repeat(" ", available-ansi.StringWidth(title)-ansi.StringWidth(mode)) + tuiMuted + mode + tuiReset
}

func viewBorder(left, right, label string, width int) string {
	label = ansi.Truncate(" "+label+" ", max(0, width-4), "…")
	return tuiMuted + left + "─" + label + strings.Repeat("─", max(0, width-3-ansi.StringWidth(label))) + right + tuiReset
}

func viewPad(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

func viewScrollbar(row, height, total, start int) string {
	if total <= height {
		return " "
	}
	thumb := max(1, height*height/total)
	offset := start * (height - thumb) / (total - height)
	if row >= offset && row < offset+thumb {
		return tuiAccent + "┃" + tuiReset
	}
	return tuiMuted + "│" + tuiReset
}

func tuiScreen(lines []string, width int) tea.View {
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeNone
	return view
}
