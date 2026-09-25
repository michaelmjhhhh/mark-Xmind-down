package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	tea "charm.land/bubbletea/v2"

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
	request   uint64
	cursor    int
	offset    int
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
	cursor, offset, width, height           int
	loading, ready, running, done, canceled bool
	message                                 string
	jobs                                    []job
	results                                 []outcome
	history                                 []outcome
	picker                                  bool
	help                                    bool
	helpOffset                              int
	sourceDirectory                         string
	sourceEntries                           []browserEntry
	sourceCursor, browserCursor             int
	sourceOffset, browserOffset             int
	sourceReady                             bool
	listingRequest                          uint64
}

func runTUI(ctx context.Context, paths []string, directory string, opts options, stdin io.Reader, stdout io.Writer, convert converter) ([]outcome, bool, error) {
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return nil, false, fmt.Errorf("open file browser: %w", err)
		}
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := tuiModel{ctx: workerCtx, cancel: cancel, convert: convert, opts: opts, directory: directory, selected: make(map[string]bool), width: 80, height: 24, ready: len(paths) > 0, loading: len(paths) == 0}
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
	return finished.allResults(), finished.canceled, nil
}

func (m tuiModel) allResults() []outcome {
	return append(append([]outcome(nil), m.history...), m.results...)
}

func (m tuiModel) Init() tea.Cmd {
	watchCancel := func() tea.Msg { <-m.ctx.Done(); return cancelMsg{} }
	if m.ready {
		return watchCancel
	}
	return tea.Batch(readDirectory(m.directory), watchCancel)
}

func readDirectory(directory string) tea.Cmd {
	return readDirectoryEntries(directory, false, 0, 0, 0)
}

func readDirectoryEntries(directory string, foldersOnly bool, request uint64, cursor, offset int) tea.Cmd {
	return func() tea.Msg {
		listing, err := os.ReadDir(directory)
		if err != nil {
			return listingMsg{directory: directory, err: err, request: request}
		}
		var entries []browserEntry
		parent := filepath.Dir(directory)
		if parent != directory {
			entries = append(entries, browserEntry{name: "..", path: parent, directory: true})
		}
		for _, entry := range listing {
			// Symlinks are not followed in the browser, avoiding navigation loops.
			if entry.IsDir() || (!foldersOnly && entry.Type().IsRegular() && isXMind(entry.Name())) {
				entries = append(entries, browserEntry{name: entry.Name(), path: filepath.Join(directory, entry.Name()), directory: entry.IsDir()})
			}
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].directory != entries[j].directory {
				return entries[i].directory
			}
			return entries[i].name < entries[j].name
		})
		return listingMsg{directory: directory, entries: entries, request: request, cursor: cursor, offset: offset}
	}
}

func (m tuiModel) openDirectory(directory string, cursor, offset int) (tea.Model, tea.Cmd) {
	m.listingRequest++
	m.loading, m.message = true, ""
	return m, readDirectoryEntries(directory, m.picker, m.listingRequest, cursor, offset)
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
		m.syncViewport(m.rowCount())
		if m.help {
			m.scrollHelp("")
		}
	case listingMsg:
		// A pending folder read may finish after Esc cancels the output picker.
		if msg.request != m.listingRequest {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.message = "Cannot open directory: " + safe(msg.err.Error())
			return m, nil
		}
		m.directory, m.entries, m.cursor, m.offset, m.message = msg.directory, msg.entries, msg.cursor, msg.offset, ""
		m.syncViewport(len(m.entries))
	case convertedMsg:
		m.results = append(m.results, outcome(msg))
		m.cursor = len(m.results) - 1
		m.syncViewport(len(m.results))
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
		if m.help {
			switch key {
			case "?", "esc":
				m.help = false
				return m, nil
			case "q", "ctrl+c":
				// The usual quit behavior remains available from help.
			default:
				m.scrollHelp(key)
				return m, nil
			}
		}
		if key == "esc" && m.picker {
			return m.closePicker(false), nil
		}
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
		if key == "?" {
			m.help, m.helpOffset = true, 0
			return m, nil
		}
		if m.done {
			switch key {
			case "enter":
				return m, tea.Quit
			case "b":
				m.history = append(m.history, m.results...)
				m.results, m.jobs = nil, nil
				m.done, m.ready = false, false
				m.selected = make(map[string]bool)
				return m.openDirectory(m.directory, m.browserCursor, m.browserOffset)
			}
			m.moveCursor(key, len(m.results))
			return m, nil
		}
		if m.loading {
			return m, nil
		}
		if m.picker {
			m.moveCursor(key, len(m.entries))
			switch key {
			case "space":
				return m.closePicker(true), nil
			case "enter", "right", "l":
				if m.cursor >= 0 && m.cursor < len(m.entries) {
					return m.openDirectory(m.entries[m.cursor].path, 0, 0)
				}
			case "backspace", "left", "h":
				return m.openDirectory(filepath.Dir(m.directory), 0, 0)
			case "r":
				return m.openDirectory(m.directory, m.cursor, m.offset)
			}
			return m, nil
		}
		if key == "f" {
			m.opts.force = !m.opts.force
			m.message = ""
			return m, nil
		}
		if key == "o" {
			m.picker = true
			m.sourceDirectory, m.sourceEntries = m.directory, m.entries
			m.sourceCursor, m.sourceReady = m.cursor, m.ready
			m.sourceOffset = m.offset
			m.ready, m.entries = false, nil
			directory := m.directory
			if m.opts.outputDir != "" {
				if absolute, err := filepath.Abs(m.opts.outputDir); err == nil {
					if info, err := os.Stat(absolute); err == nil && info.IsDir() {
						directory = absolute
					}
				}
			}
			return m.openDirectory(directory, 0, 0)
		}
		if m.ready {
			switch key {
			case "enter", "e":
				return m.start()
			}
			m.moveCursor(key, len(m.selected))
			return m, nil
		}
		m.moveCursor(key, len(m.entries))
		switch key {
		case "backspace", "left", "h":
			return m.openDirectory(filepath.Dir(m.directory), 0, 0)
		case "r":
			return m.openDirectory(m.directory, m.cursor, m.offset)
		case "enter", "right", "l":
			if len(m.entries) > 0 {
				entry := m.entries[m.cursor]
				if entry.directory {
					return m.openDirectory(entry.path, 0, 0)
				}
				if key == "enter" {
					m.toggle(entry.path)
				}
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

func (m *tuiModel) scrollHelp(key string) {
	rows := m.helpVisibleRows()
	switch key {
	case "up", "k":
		m.helpOffset--
	case "down", "j":
		m.helpOffset++
	case "pgup":
		m.helpOffset -= rows
	case "pgdown":
		m.helpOffset += rows
	case "home":
		m.helpOffset = 0
	case "end":
		m.helpOffset = len(m.helpContent())
	}
	m.helpOffset = min(max(0, m.helpOffset), max(0, len(m.helpContent())-rows))
}

func (m *tuiModel) moveCursor(key string, count int) {
	switch key {
	case "up", "k":
		m.cursor--
	case "down", "j":
		m.cursor++
	case "pgup":
		m.cursor -= m.visibleRows()
		m.offset -= m.visibleRows()
	case "pgdown":
		m.cursor += m.visibleRows()
		m.offset += m.visibleRows()
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = count - 1
	}
	m.syncViewport(count)
}

func (m *tuiModel) syncViewport(count int) {
	m.cursor = min(max(0, m.cursor), max(0, count-1))
	m.offset = min(max(0, m.offset), max(0, count-m.visibleRows()))
	if m.cursor < m.offset {
		m.offset = m.cursor
	} else if m.cursor >= m.offset+m.visibleRows() {
		m.offset = m.cursor - m.visibleRows() + 1
	}
}

func (m tuiModel) rowCount() int {
	if m.running || m.done {
		return len(m.results)
	}
	if m.ready {
		return len(m.selected)
	}
	return len(m.entries)
}

func (m tuiModel) closePicker(choose bool) tuiModel {
	if choose {
		m.opts.output, m.opts.outputDir = "", m.directory
	}
	m.directory, m.entries, m.cursor = m.sourceDirectory, m.sourceEntries, m.sourceCursor
	m.offset = m.sourceOffset
	m.ready, m.picker, m.loading = m.sourceReady, false, false
	m.syncViewport(m.rowCount())
	m.sourceDirectory, m.sourceEntries = "", nil
	m.listingRequest++ // Discard a directory read still in flight when Esc was pressed.
	m.message = ""
	if choose {
		m.message = "Output folder selected. Press e to export."
	}
	return m
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
	m.browserCursor = m.cursor
	m.browserOffset = m.offset
	m.jobs, m.running, m.done, m.message = jobs, true, false, ""
	m.results, m.cursor, m.offset = nil, 0, 0
	return m, m.convertNext()
}

func (m tuiModel) convertNext() tea.Cmd {
	j := m.jobs[len(m.results)]
	return func() tea.Msg {
		result, err := m.convert(m.ctx, j.input, export.Options{Output: j.output, Force: m.opts.force})
		return convertedMsg{input: j.input, result: result, err: err}
	}
}
