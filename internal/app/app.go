// Package app implements the command-line and interactive XMind exporter.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/term"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

type options struct {
	output, outputDir                    string
	recursive, force, tui, help, version bool
}

type converter func(context.Context, string, export.Options) (export.Result, error)

type job struct{ input, output string }
type outcome struct {
	input  string
	result export.Result
	err    error
}

const usage = `xmind-md — export XMind maps to Markdown with local images

Usage:
  xmind-md                    Open the interactive file browser
  xmind-md --tui FOLDER        Browse a folder and choose files
  xmind-md [flags] INPUT...

In the browser: arrows move, Space selects, e exports, o chooses output.
No filenames or export commands need to be typed.

Examples:
  xmind-md --tui maps          Choose files in maps/
  xmind-md "My Map.xmind"      Convert one file
  xmind-md maps -d exports     Convert a folder into exports/
  xmind-md maps -r -d exports  Include subfolders
  xmind-md "My Map.xmind" -f   Replace an earlier export

Output: My Map.md beside the input, with images in assets/ beside it.
Keep the Markdown and assets/ together when moving or sharing them.

Flags:
  -o, --output FILE       Save one map to a specific .md file
  -d, --output-dir DIR    Save all Markdown files in this folder
  -r, --recursive         Include subfolders
  -f, --force             Replace existing Markdown output
      --tui               Choose files interactively (requires a terminal)
  -h, --help              Show this help
      --version           Show version
      --                  Treat all following arguments as paths

Inputs can be files, folders, or quoted globs ("maps/*.xmind").
Flags work before or after inputs. Existing files are kept unless -f is used.
`

// Run executes the CLI. It does not close streams or terminate the process.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	return run(ctx, args, stdin, stdout, stderr, version, export.Convert)
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, version string, convert converter) int {
	opts, inputs, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Error:", safe(err.Error()))
		fmt.Fprintln(stderr, "Run xmind-md --help for usage.")
		return 2
	}
	if opts.help {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if opts.version {
		fmt.Fprintln(stdout, "xmind-md", safe(version))
		return 0
	}
	if opts.output != "" && opts.outputDir != "" {
		fmt.Fprintln(stderr, "Error: --output and --output-dir cannot be combined.")
		return 2
	}
	interactive := opts.tui || len(inputs) == 0
	if interactive && (!isTerminal(stdin) || !isTerminal(stdout)) {
		if len(inputs) == 0 && !opts.tui {
			fmt.Fprint(stdout, usage)
		}
		fmt.Fprintln(stderr, "Error: interactive mode requires a terminal; pass an input path for batch conversion.")
		return 2
	}
	paths, directory, inputErrors := resolveInputs(inputs, opts.recursive, interactive)
	for _, err := range inputErrors {
		fmt.Fprintln(stderr, "Error:", safe(err.Error()))
	}
	if len(inputs) > 0 && len(paths) == 0 && directory == "" {
		fmt.Fprintln(stderr, "Error: no .xmind files found.")
		return 1
	}
	jobs, err := planJobs(paths, opts)
	if err != nil {
		fmt.Fprintln(stderr, "Error:", safe(err.Error()))
		return 2
	}
	var outcomes []outcome
	var canceled bool
	if interactive {
		outcomes, canceled, err = runTUI(ctx, paths, directory, opts, stdin, stdout, convert)
		if err != nil {
			fmt.Fprintln(stderr, "Error:", safe(err.Error()))
			if ctx.Err() != nil {
				return 130
			}
			return 1
		}
	} else {
		for _, j := range jobs {
			if ctx.Err() != nil {
				canceled = true
				break
			}
			result, err := convert(ctx, j.input, export.Options{Output: j.output, Force: opts.force})
			outcomes = append(outcomes, outcome{input: j.input, result: result, err: err})
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				canceled = true
				break
			}
		}
	}
	failures := len(inputErrors)
	successes := 0
	for _, o := range outcomes {
		if o.err != nil {
			failures++
			fmt.Fprintf(stderr, "Error: %s: %s\n", safe(o.input), safe(o.err.Error()))
			continue
		}
		successes++
		fmt.Fprintf(stdout, "Exported %s (%d sheets, %d topics, %d images)\n", safe(o.result.Output), o.result.Sheets, o.result.Topics, o.result.Images)
		for _, warning := range o.result.Warnings {
			fmt.Fprintf(stderr, "Warning: %s: %s\n", safe(o.input), safe(warning))
		}
	}
	if len(outcomes) > 1 || failures > 0 {
		fmt.Fprintf(stdout, "%d exported, %d failed.\n", successes, failures)
	}
	if canceled || ctx.Err() != nil {
		fmt.Fprintln(stderr, "Canceled.")
		return 130
	}
	if failures > 0 {
		return 1
	}
	return 0
}

func parseArgs(args []string) (options, []string, error) {
	var o options
	fs := flag.NewFlagSet("xmind-md", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.output, "output", "", "")
	fs.StringVar(&o.output, "o", "", "")
	fs.StringVar(&o.outputDir, "output-dir", "", "")
	fs.StringVar(&o.outputDir, "d", "", "")
	fs.BoolVar(&o.recursive, "recursive", false, "")
	fs.BoolVar(&o.recursive, "r", false, "")
	fs.BoolVar(&o.force, "force", false, "")
	fs.BoolVar(&o.force, "f", false, "")
	fs.BoolVar(&o.tui, "tui", false, "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	// Keep standard flag parsing, but allow flags after positional arguments.
	var flags, paths []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			paths = append(paths, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			paths = append(paths, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		name, _, hasValue := strings.Cut(name, "=")
		f := fs.Lookup(name)
		if f == nil {
			return o, nil, fmt.Errorf("unknown flag %q", a)
		}
		flags = append(flags, a)
		boolFlag, isBool := f.Value.(interface{ IsBoolFlag() bool })
		if !hasValue && (!isBool || !boolFlag.IsBoolFlag()) {
			i++
			if i >= len(args) {
				return o, nil, fmt.Errorf("flag %s requires a value", a)
			}
			flags = append(flags, args[i])
		}
	}
	if err := fs.Parse(flags); err != nil {
		return o, nil, err
	}
	return o, paths, nil
}

func discover(inputs []string, recursive bool) ([]string, []error) {
	seen := make(map[string]bool)
	var files []string
	var errs []error
	add := func(path string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			errs = append(errs, err)
			return
		}
		identity, err := filepath.EvalSymlinks(abs)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			return
		}
		if !seen[identity] {
			seen[identity] = true
			files = append(files, abs)
		}
	}
	for _, input := range inputs {
		matches := []string{input}
		if _, err := os.Stat(input); err != nil && strings.ContainsAny(input, "*?[") {
			matches, err = filepath.Glob(input)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: invalid glob: %w", input, err))
				continue
			}
			if len(matches) == 0 {
				errs = append(errs, fmt.Errorf("%s: no matching files", input))
				continue
			}
		}
		for _, path := range matches {
			info, err := os.Stat(path)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				continue
			}
			if !info.IsDir() {
				if !info.Mode().IsRegular() || !isXMind(path) {
					errs = append(errs, fmt.Errorf("%s: expected a regular .xmind file", path))
					continue
				}
				add(path)
				continue
			}
			// Resolve a directory explicitly supplied by the user; never follow
			// directory symlinks encountered during recursive traversal.
			root := filepath.Clean(path)
			linkInfo, err := os.Lstat(path)
			if err == nil && linkInfo.Mode()&os.ModeSymlink != 0 {
				root, err = filepath.EvalSymlinks(path)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				continue
			}
			err = filepath.WalkDir(root, func(p string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					errs = append(errs, fmt.Errorf("%s: %w", p, walkErr))
					if entry != nil && entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.IsDir() {
					if p != root && !recursive {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.Type().IsRegular() && isXMind(p) {
					add(p)
				}
				return nil
			})
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	sort.Strings(files)
	return files, errs
}

// A single interactive directory opens the picker, including when it is empty.
// Explicit files/globs retain the prepared selection; batch discovery is unchanged.
func resolveInputs(inputs []string, recursive, interactive bool) ([]string, string, []error) {
	if interactive && len(inputs) == 1 {
		if info, err := os.Stat(inputs[0]); err == nil && info.IsDir() {
			directory, err := filepath.Abs(inputs[0])
			if err == nil {
				directory, err = filepath.EvalSymlinks(directory)
			}
			if err != nil {
				return nil, "", []error{fmt.Errorf("%s: %w", inputs[0], err)}
			}
			return nil, directory, nil
		}
	}
	paths, errs := discover(inputs, recursive)
	return paths, "", errs
}

func planJobs(paths []string, opts options) ([]job, error) {
	if opts.output != "" && len(paths) > 1 {
		return nil, fmt.Errorf("--output requires exactly one input; use --output-dir for multiple inputs")
	}
	if opts.output != "" && !strings.EqualFold(filepath.Ext(opts.output), ".md") {
		return nil, fmt.Errorf("--output must have a .md extension")
	}
	seen := make(map[string]string)
	jobs := make([]job, 0, len(paths))
	for _, input := range paths {
		output := strings.TrimSuffix(input, filepath.Ext(input)) + ".md"
		if opts.outputDir != "" {
			output = filepath.Join(opts.outputDir, filepath.Base(output))
		}
		if opts.output != "" {
			output = opts.output
		}
		abs, err := filepath.Abs(output)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(filepath.Clean(abs))
		if previous, exists := seen[key]; exists {
			return nil, fmt.Errorf("output collision: %s and %s both target %s; choose separate destinations", previous, input, abs)
		}
		seen[key] = input
		jobs = append(jobs, job{input: input, output: abs})
	}
	return jobs, nil
}

func isXMind(path string) bool { return strings.EqualFold(filepath.Ext(path), ".xmind") }
func isTerminal(stream any) bool {
	f, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(f.Fd())
}

// Filenames and archive warnings are untrusted terminal text. Strip all controls
// and bidi formatting characters so they cannot inject terminal escape codes.
func safe(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return '\uFFFD'
		}
		return r
	}, text)
}
