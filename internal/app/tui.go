package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

type browserEntry struct {
	name, path string
	directory  bool
}
type listingMsg struct {
	directory string
	entries   []browserEntry
	err       error
}
type convertedMsg outcome
type cancelMsg struct{}

type tuiModel struct {
	ctx                                     context.Context
	cancel                                  context.CancelFunc
	convert                                 converter
	opts                                    options
	directory                               string
	entries                                 []browserEntry
	selected                                map[string]bool
	cursor, width, height                   int
	loading, ready, running, done, canceled bool
	message                                 string
	jobs                                    []job
	results                                 []outcome
}

func runTUI(ctx context.Context, paths []string, opts options, stdin io.Reader, stdout io.Writer, convert converter) ([]outcome, bool, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, false, fmt.Errorf("open file browser: %w", err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := tuiModel{ctx: workerCtx, cancel: cancel, convert: convert, opts: opts, directory: directory, selected: make(map[string]bool), width: 80, height: 24, ready: len(paths) > 0}
	for _, path := range paths {
		m.selected[path] = true
	}
	// Feed cancellation through Update so an active converter finishes cleanup
	// before the terminal closes. Main owns process signal handling.
	final, err := tea.NewProgram(m, tea.WithContext(context.WithoutCancel(ctx)), tea.WithoutSignalHandler(), tea.WithInput(stdin), tea.WithOutput(stdout)).Run()
	if err != nil {
		return nil, ctx.Err() != nil, err
	}
	finished := final.(tuiModel)
	return finished.results, finished.canceled, nil
}

func (m tuiModel) Init() tea.Cmd {
	watchCancel := func() tea.Msg { <-m.ctx.Done(); return cancelMsg{} }
	if m.ready {
		return watchCancel
	}
	return tea.Batch(readDirectory(m.directory), watchCancel)
}

func readDirectory(directory string) tea.Cmd {
	return func() tea.Msg {
		listing, err := os.ReadDir(directory)
		if err != nil {
			return listingMsg{directory: directory, err: err}
		}
		var entries []browserEntry
		parent := filepath.Dir(directory)
		if parent != directory {
			entries = append(entries, browserEntry{name: "..", path: parent, directory: true})
		}
		for _, entry := range listing {
			// Symlinks are not followed in the browser, avoiding navigation loops.
			if entry.IsDir() || (entry.Type().IsRegular() && isXMind(entry.Name())) {
				entries = append(entries, browserEntry{name: entry.Name(), path: filepath.Join(directory, entry.Name()), directory: entry.IsDir()})
			}
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].directory != entries[j].directory {
				return entries[i].directory
			}
			return entries[i].name < entries[j].name
		})
		return listingMsg{directory: directory, entries: entries}
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case cancelMsg:
		m.canceled = true
		if !m.running {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case listingMsg:
		m.loading = false
		if msg.err != nil {
			m.message = "Cannot open directory: " + safe(msg.err.Error())
			return m, nil
		}
		m.directory, m.entries, m.cursor, m.message = msg.directory, msg.entries, 0, ""
	case convertedMsg:
		m.results = append(m.results, outcome(msg))
		if m.canceled || m.ctx.Err() != nil {
			m.canceled = true
			m.running = false
			return m, tea.Quit
		}
		if len(m.results) < len(m.jobs) {
			return m, m.convertNext()
		}
		m.running, m.done = false, true
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" || key == "q" || key == "esc" {
			if m.running {
				// Wait for the worker result before leaving; conversion owns its cleanup.
				m.canceled = true
				m.cancel()
				return m, nil
			}
			m.canceled = key == "ctrl+c"
			return m, tea.Quit
		}
		if m.running {
			return m, nil
		}
		if m.done {
			if key == "enter" {
				return m, tea.Quit
			}
			return m, nil
		}
		if key == "f" {
			m.opts.force = !m.opts.force
			m.message = ""
			return m, nil
		}
		if m.ready {
			switch key {
			case "enter", "e":
				return m.start()
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.selected)-1 {
					m.cursor++
				}
			}
			return m, nil
		}
		if m.loading {
			return m, nil
		}
		switch key {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "backspace", "left", "h":
			m.loading = true
			return m, readDirectory(filepath.Dir(m.directory))
		case "enter", "right", "l":
			if len(m.entries) > 0 {
				entry := m.entries[m.cursor]
				if entry.directory {
					m.loading = true
					return m, readDirectory(entry.path)
				}
				m.toggle(entry.path)
			}
		case "space":
			if len(m.entries) > 0 && !m.entries[m.cursor].directory {
				m.toggle(m.entries[m.cursor].path)
			}
		case "a":
			allSelected := true
			for _, entry := range m.entries {
				if !entry.directory && !m.selected[entry.path] {
					allSelected = false
				}
			}
			for _, entry := range m.entries {
				if !entry.directory {
					if allSelected {
						delete(m.selected, entry.path)
					} else {
						m.selected[entry.path] = true
					}
				}
			}
		case "e":
			return m.start()
		}
	}
	return m, nil
}

func (m *tuiModel) toggle(path string) {
	if m.selected[path] {
		delete(m.selected, path)
	} else {
		m.selected[path] = true
	}
}
func (m tuiModel) selectedPaths() []string {
	paths := make([]string, 0, len(m.selected))
	for path := range m.selected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (m tuiModel) start() (tea.Model, tea.Cmd) {
	paths := m.selectedPaths()
	if len(paths) == 0 && !m.ready && m.cursor >= 0 && m.cursor < len(m.entries) {
		entry := m.entries[m.cursor]
		if !entry.directory && isXMind(entry.path) {
			paths = []string{entry.path}
		}
	}
	if len(paths) == 0 {
		m.message = "Highlight a .xmind file or select files with Space."
		return m, nil
	}
	jobs, err := planJobs(paths, m.opts)
	if err != nil {
		m.message = safe(err.Error())
		return m, nil
	}
	m.jobs, m.running, m.message = jobs, true, ""
	return m, m.convertNext()
}

func (m tuiModel) convertNext() tea.Cmd {
	j := m.jobs[len(m.results)]
	return func() tea.Msg {
		result, err := m.convert(m.ctx, j.input, export.Options{Output: j.output, Force: m.opts.force})
		return convertedMsg{input: j.input, result: result, err: err}
	}
}

func (m tuiModel) View() tea.View {
	var b strings.Builder
	b.WriteString("XMind → Markdown\n")
	if m.opts.output != "" {
		fmt.Fprintf(&b, "Output: %s\n", safe(m.opts.output))
	} else if m.opts.outputDir != "" {
		fmt.Fprintf(&b, "Output: %s  ·  images: assets/\n", safe(m.opts.outputDir))
	} else {
		b.WriteString("Output: beside each input  ·  images: assets/\n")
	}
	if m.opts.force {
		b.WriteString("Overwrite: on (replace existing Markdown)\n")
	} else {
		b.WriteString("Overwrite: off (keep existing files)\n")
	}
	b.WriteString("\n")
	rows := max(1, m.height-13)
	switch {
	case m.running || m.done:
		if m.done {
			fmt.Fprintf(&b, "Finished %d file(s)\n\n", len(m.results))
		} else if m.canceled {
			b.WriteString("Canceling…\n\n")
		} else {
			fmt.Fprintf(&b, "Exporting %d/%d: %s\n\n", len(m.results)+1, len(m.jobs), safe(filepath.Base(m.jobs[len(m.results)].input)))
		}
		start := max(0, len(m.results)-rows)
		for _, result := range m.results[start:] {
			if result.err != nil {
				fmt.Fprintf(&b, "✗ %s: %s\n", safe(filepath.Base(result.input)), safe(result.err.Error()))
			} else {
				fmt.Fprintf(&b, "✓ %s  ·  %d topics, %d images", safe(filepath.Base(result.result.Output)), result.result.Topics, result.result.Images)
				if len(result.result.Warnings) > 0 {
					fmt.Fprintf(&b, "  ·  %d warnings", len(result.result.Warnings))
				}
				b.WriteString("\n")
			}
		}
		if m.done {
			b.WriteString("\nEnter/q: close · full results and warnings print on exit")
		} else {
			b.WriteString("\nq / Esc / Ctrl+C: cancel")
		}
	case m.ready:
		fmt.Fprintf(&b, "%d file(s) ready to export\n\n", len(m.selected))
		paths := m.selectedPaths()
		start := max(0, m.cursor-rows+1)
		for _, path := range paths[start:min(len(paths), start+rows)] {
			fmt.Fprintf(&b, "  %s\n", safe(path))
		}
		b.WriteString("\nEnter/e: export · ↑/↓: scroll\nf: toggle overwrite · q/Esc: cancel")
	default:
		fmt.Fprintf(&b, "%s\n\n", safe(m.directory))
		if m.loading {
			b.WriteString("Loading…\n")
		} else if len(m.entries) == 0 {
			b.WriteString("No subdirectories or .xmind files here.\n")
		}
		start := max(0, m.cursor-rows+1)
		for i := start; i < min(len(m.entries), start+rows); i++ {
			entry := m.entries[i]
			cursor, mark, suffix := " ", " ", ""
			if i == m.cursor {
				cursor = ">"
			}
			if m.selected[entry.path] {
				mark = "x"
			}
			if entry.directory {
				suffix = "/"
				mark = "·"
			}
			fmt.Fprintf(&b, "%s [%s] %s%s\n", cursor, mark, safe(entry.name), suffix)
		}
		fmt.Fprintf(&b, "\n%d selected · ↑/↓ move · Enter open/select · Space select\n", len(m.selected))
		b.WriteString("e: export selected or highlighted · a: select all · ←: parent\n")
		b.WriteString("f: toggle overwrite · q/Esc: quit")
	}
	if m.message != "" {
		fmt.Fprintf(&b, "\n\n%s", safe(m.message))
	}
	lines := strings.Split(b.String(), "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(1, m.width-1), "…")
	}
	view := tea.NewView(strings.Join(lines, "\n") + "\n")
	view.AltScreen = true
	return view
}
