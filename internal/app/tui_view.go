package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	tuiReset     = "\x1b[0m"
	tuiAccent    = "\x1b[1;36m"
	tuiSecondary = "\x1b[1;35m"
	tuiSuccess   = "\x1b[1;32m"
	tuiWarning   = "\x1b[1;33m"
	tuiDanger    = "\x1b[1;31m"
	tuiBold      = "\x1b[1m"
	tuiMuted     = "\x1b[39m"
	tuiHighlight = "\x1b[1;36;7m"
)

type viewRow struct {
	text, detail string
}

// Keep navigation and rendering on the same page size, including compact
// terminals. The reserved lines contain the destination and keyboard controls.
func (m tuiModel) visibleRows() int {
	if m.compactView() {
		return max(1, m.height-9)
	}
	return max(1, m.height-12)
}

func (m tuiModel) compactView() bool { return m.height < 18 || m.width < 60 }

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
		if m.help {
			lines[2] = "Esc: back · q: quit"
		}
		if m.running {
			lines[2] = "q / Esc / Ctrl+C: cancel"
		}
		return tuiScreen(lines[:min(height, len(lines))], width)
	}
	if m.help {
		return m.helpView()
	}

	mode, label, location, rows := m.viewRows()
	lines := []string{viewHeading("XMind → Markdown", mode, width)}
	if !m.compactView() {
		lines = append(lines, " "+m.viewSubtitle())
	}
	if !m.running && !m.done && !m.ready {
		location = "IN  " + shortPath(m.directory)
	}
	lines = append(lines, " "+tuiMuted+location+tuiReset)
	if !m.compactView() {
		lines = append(lines, "")
	}
	if !m.running && !m.done && !m.picker {
		label += fmt.Sprintf(" · %d selected", len(m.selected))
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
		} else if index < end {
			text = colorRow(text)
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
	lines = append(lines, " "+m.viewNotice(rows, cursor), " "+m.viewDestination(width))
	if !m.compactView() {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewFooter(width)...)
	return tuiScreen(lines, width)
}

func shortPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+string(filepath.Separator)) {
			path = "~" + strings.TrimPrefix(path, home)
		}
	}
	return safe(path)
}

func (m tuiModel) viewSubtitle() string {
	switch {
	case m.picker:
		return tuiSecondary + "Choose where to save your Markdown" + tuiReset
	case m.running:
		return tuiAccent + "Exporting Markdown and images…" + tuiReset
	case m.done:
		success, failed := 0, 0
		for _, result := range m.results {
			if result.err == nil {
				success++
			} else {
				failed++
			}
		}
		text := tuiSuccess + fmt.Sprintf("✓ %d exported", success) + tuiReset
		if failed > 0 {
			text += "   " + tuiDanger + fmt.Sprintf("✗ %d failed", failed) + tuiReset
		}
		return text
	case m.ready:
		return tuiBold + "Your files are ready to export" + tuiReset
	default:
		return tuiBold + "Choose your XMind files" + tuiReset
	}
}

func (m tuiModel) viewNotice(rows []viewRow, cursor int) string {
	if m.message != "" {
		color, prefix := tuiWarning, "! "
		if strings.HasPrefix(m.message, "Output folder selected.") {
			return tuiSuccess + "✓ Output folder updated" + tuiReset
		}
		return color + prefix + safe(m.message) + tuiReset
	}
	if m.running && m.canceled {
		return tuiWarning + "Canceling… finishing cleanup." + tuiReset
	}
	if (m.running || m.done) && len(rows) > 0 {
		detail := rows[cursor].detail
		if strings.HasPrefix(detail, "Saved:") {
			return "" // The destination already has a dedicated line below.
		}
		color := tuiMuted
		if strings.HasPrefix(detail, "Error:") {
			color = tuiDanger
		}
		if strings.HasPrefix(detail, "Warning:") {
			color = tuiWarning
		}
		return color + detail + tuiReset
	}
	// Avoid repeating the selected path. Reveal the full name only when its row
	// is too short to show it, and reserve this line for actionable notifications.
	if len(rows) > 0 && ansi.StringWidth(rows[cursor].text) > m.width-9 {
		return tuiMuted + rows[cursor].text + tuiReset
	}
	return ""
}

func (m tuiModel) viewDestination(width int) string {
	output := "beside each source"
	if m.opts.output != "" {
		output = shortPath(m.opts.output)
	} else if m.opts.outputDir != "" {
		output = shortPath(m.opts.outputDir)
	}
	if m.picker {
		return tuiSecondary + "SAVE TO  " + tuiReset + shortPath(m.directory)
	}
	left := tuiSecondary + "SAVE TO  " + tuiReset + output
	if width < 60 && m.opts.force && !m.running && !m.done {
		right := tuiWarning + "Replace ON" + tuiReset
		return viewPad(left, width-3-ansi.StringWidth(right)) + " " + right
	}
	if width < 60 || m.running || m.done {
		return left
	}
	state, color := "off", tuiMuted
	if m.opts.force {
		state, color = "ON", tuiWarning
	}
	right := viewKey("f", tuiSecondary) + " Replace " + color + state + tuiReset
	available := width - 2 - ansi.StringWidth(right) - 3
	return viewPad(left, available) + "   " + right
}

func colorRow(text string) string {
	for _, marker := range []struct{ token, color string }{{"[x]", tuiSuccess}, {"▸", tuiSecondary}, {"✓", tuiSuccess}, {"✗", tuiDanger}, {"…", tuiAccent}} {
		if i := strings.Index(text, marker.token); i >= 0 && i < 6 {
			return text[:i] + marker.color + marker.token + tuiReset + text[i+len(marker.token):]
		}
	}
	return text
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

func viewKey(key, color string) string {
	return color + "[" + key + "]" + tuiReset
}

func viewAction(key, label, color string, primary bool) string {
	style := color
	if primary {
		style += "\x1b[7m"
	}
	return viewKey(key, style) + " " + label
}

func (m tuiModel) viewFooter(width int) []string {
	gap := "   "
	if width < 60 {
		gap = " "
	}
	helpLabel, outputLabel := "All keys", "Output folder"
	if width < 60 {
		helpLabel, outputLabel = "Keys", "Output"
	}
	help := viewAction("?", helpLabel, tuiSecondary, false)
	quit := viewAction("q", "Quit", tuiMuted, false)
	move := viewAction("↑↓", "Move", tuiAccent, false)
	var primary, navigation, more string
	switch {
	case m.running:
		primary = viewAction("Ctrl+C", "Cancel export", tuiWarning, true)
	case m.done:
		primary = viewAction("b", "Browse more", tuiAccent, true)
		navigation = viewAction("↑↓", "Inspect results", tuiAccent, false)
		more = help + gap + viewAction("q", "Close", tuiMuted, false)
	case m.picker:
		primary = viewAction("Space", "Use this folder", tuiSecondary, true)
		navigation = move + gap + viewAction("Enter", "Open", tuiAccent, false)
		more = viewAction("Esc", "Back", tuiMuted, false) + gap + help
		if width >= 60 {
			navigation += gap + viewAction("←", "Parent", tuiAccent, false)
		}
	default:
		key, label := "e", "Export file"
		if len(m.selected) > 0 {
			label = fmt.Sprintf("Export %d files", len(m.selected))
			if len(m.selected) == 1 {
				label = "Export 1 file"
			}
		}
		if width < 60 {
			label = "Export"
		}
		if m.ready {
			key = "Enter"
		}
		folder := !m.ready && m.cursor >= 0 && m.cursor < len(m.entries) && m.entries[m.cursor].directory
		if folder && len(m.selected) == 0 {
			key, label = "Enter", "Open folder"
		}
		if m.loading {
			primary = tuiMuted + "Loading files…" + tuiReset
		} else if len(m.entries) == 0 && len(m.selected) == 0 {
			primary = tuiMuted + "Choose a file to export" + tuiReset
		} else {
			primary = viewAction(key, label, tuiAccent, true)
		}
		output := viewAction("o", outputLabel, tuiSecondary, false)
		if ansi.StringWidth(primary+gap+output) < width-1 {
			primary += gap + output
		} else {
			// A long primary action never hides destination selection in a small terminal.
			more = output + gap
		}
		navigation = move
		if !m.ready {
			if len(m.entries) == 0 {
				navigation = viewAction("r", "Refresh", tuiAccent, false)
			} else if folder {
				navigation += gap + viewAction("←", "Parent", tuiAccent, false)
			} else {
				verb := "Select"
				if m.cursor >= 0 && m.cursor < len(m.entries) && m.selected[m.entries[m.cursor].path] {
					verb = "Deselect"
				}
				navigation += gap + viewAction("Space", verb, tuiAccent, false)
			}
			if width >= 60 {
				files, selected := 0, 0
				for _, entry := range m.entries {
					if !entry.directory {
						files++
						if m.selected[entry.path] {
							selected++
						}
					}
				}
				if files > 0 {
					label := "Select all"
					if selected == files {
						label = "Deselect all"
					}
					navigation += gap + viewAction("a", label, tuiAccent, false)
				}
			}
		}
		more += help + gap + quit
	}
	return []string{" " + primary, " " + navigation, " " + more}
}

// Full help is grouped by task. Compact terminals wrap and scroll the same
// content; no shortcut disappears just because the terminal is small.
func (m tuiModel) helpVisibleRows() int { return max(1, m.height-6) }

func (m tuiModel) helpContent() []string {
	width := max(20, m.width-4)
	var lines []string
	section := func(title string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, tuiSecondary+title+tuiReset)
	}
	binding := func(key, label string) {
		keyText := viewKey(key, tuiAccent)
		if width >= 76 {
			lines = append(lines, viewPad(keyText, 23)+label)
		} else {
			lines = append(lines, keyText)
			words := strings.Fields(label)
			line := "  "
			for _, word := range words {
				if ansi.StringWidth(line+word) > width {
					lines = append(lines, strings.TrimRight(line, " "))
					line = "  "
				}
				line += word + " "
			}
			lines = append(lines, strings.TrimRight(line, " "))
		}
	}
	section("NAVIGATE")
	binding("↑ ↓ / k j", "Move through the list")
	binding("PgUp PgDn / Home End", "Page through files / jump to ends")
	switch {
	case m.done:
		section("RESULTS")
		binding("b", "Browse more files")
		binding("Enter / q / Esc", "Close; print full results and warnings")
	case m.picker:
		binding("Enter / → / l", "Open a folder")
		binding("← / Backspace / h", "Go to the parent folder")
		section("DESTINATION")
		binding("Space", "Use the current folder")
		binding("Esc", "Back without changing the destination")
		binding("r", "Refresh folders")
	case m.ready:
		section("EXPORT")
		binding("Enter / e", "Export the prepared selection")
		section("OUTPUT")
		binding("o", "Choose an output folder")
		binding("f", "Toggle replacing existing Markdown")
	default:
		binding("Enter / → / l", "Open a folder; Enter also selects files")
		binding("← / Backspace / h", "Go to the parent folder")
		section("SELECT & EXPORT")
		binding("Space / Enter", "Select or deselect a file")
		binding("a", "Select or deselect all files here")
		binding("e", "Export selected files, or the highlighted file")
		section("OUTPUT & TOOLS")
		binding("o", "Choose an output folder")
		binding("f", "Toggle replacing existing Markdown")
		binding("r", "Refresh files")
	}
	section("SESSION")
	if m.picker {
		binding("q / Ctrl+C", "Quit")
	} else if !m.done {
		binding("q / Esc / Ctrl+C", "Quit")
	} else {
		binding("Ctrl+C", "Quit")
	}
	binding("? / Esc", "Close this help; resume where you left off")
	return lines
}

func (m tuiModel) helpView() tea.View {
	lines := []string{viewHeading("Keyboard shortcuts", "HELP", m.width), " " + tuiMuted + "Keys for the current screen" + tuiReset, ""}
	content := m.helpContent()
	count := m.helpVisibleRows()
	start := min(max(0, m.helpOffset), max(0, len(content)-count))
	end := min(len(content), start+count)
	for i := start; i < end; i++ {
		lines = append(lines, " "+content[i])
	}
	for len(lines) < 3+count {
		lines = append(lines, "")
	}
	position := ""
	if len(content) > count {
		position = fmt.Sprintf(" %d–%d of %d", start+1, end, len(content))
	}
	lines = append(lines, tuiMuted+position+tuiReset, " "+viewAction("↑↓", "Scroll", tuiAccent, false)+"  "+viewAction("Esc", "Back", tuiSecondary, true), " "+viewAction("?", "Back", tuiSecondary, false)+"  "+viewAction("q", "Quit", tuiMuted, false))
	return tuiScreen(lines, m.width)
}

func viewHeading(title, mode string, width int) string {
	available := width - 2
	color := tuiAccent
	switch mode {
	case "OUTPUT FOLDER", "HELP":
		color = tuiSecondary
	case "COMPLETE":
		color = tuiSuccess
	}
	badge := color + "\x1b[7m " + mode + " " + tuiReset
	if width < 50 && title == "XMind → Markdown" {
		title = "XMind → MD"
	}
	if ansi.StringWidth(title)+ansi.StringWidth(badge)+2 > available {
		return " " + tuiAccent + title + tuiReset
	}
	return " " + tuiAccent + title + tuiReset + strings.Repeat(" ", available-ansi.StringWidth(title)-ansi.StringWidth(badge)) + badge
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
