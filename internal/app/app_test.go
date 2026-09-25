package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

func fixture(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseInterspersedFlagsAndLiteralPaths(t *testing.T) {
	o, inputs, err := parseArgs([]string{"first.xmind", "--recursive", "--output-dir=out", "second.xmind", "-f", "--", "-special.xmind"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.recursive || !o.force || o.outputDir != "out" {
		t.Fatalf("unexpected options: %+v", o)
	}
	if want := []string{"first.xmind", "second.xmind", "-special.xmind"}; !reflect.DeepEqual(inputs, want) {
		t.Fatalf("got %v, want %v", inputs, want)
	}
	for _, args := range [][]string{{"--unknown"}, {"--output"}, {"--force=invalid"}} {
		if _, _, err := parseArgs(args); err == nil {
			t.Fatalf("expected error for %v", args)
		}
	}
}

func TestDiscoverDeterministicRecursiveAndDeduplicated(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, dir, "a.XMIND")
	b := fixture(t, dir, "b.xmind")
	nested := fixture(t, dir, "nested/c.xmind")
	fixture(t, dir, "notes.txt")
	files, errs := discover([]string{dir, b, a}, false)
	if len(errs) != 0 || !reflect.DeepEqual(files, []string{a, b}) {
		t.Fatalf("files=%v errors=%v", files, errs)
	}
	files, errs = discover([]string{dir}, true)
	if len(errs) != 0 || !reflect.DeepEqual(files, []string{a, b, nested}) {
		t.Fatalf("files=%v errors=%v", files, errs)
	}
	files, errs = discover([]string{filepath.Join(dir, "*.xmind")}, false)
	if len(errs) != 0 || !reflect.DeepEqual(files, []string{b}) {
		t.Fatalf("glob files=%v errors=%v", files, errs)
	}
}

func TestPlanRejectsPortableOutputCollisionBeforeConversion(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, dir, "one/Map.xmind")
	b := fixture(t, dir, "two/map.XMIND")
	var out, stderr bytes.Buffer
	called := false
	code := run(context.Background(), []string{a, b, "-d", filepath.Join(dir, "out")}, strings.NewReader(""), &out, &stderr, "test", func(context.Context, string, export.Options) (export.Result, error) {
		called = true
		return export.Result{}, nil
	})
	if code != 2 || called || !strings.Contains(stderr.String(), "collision") {
		t.Fatalf("code=%d called=%v stderr=%s", code, called, stderr.String())
	}
}

func TestBatchContinuesErrorsAndPassesOptions(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, dir, "a.xmind")
	b := fixture(t, dir, "b.xmind")
	var out, stderr bytes.Buffer
	var visited []string
	convert := func(ctx context.Context, input string, opts export.Options) (export.Result, error) {
		visited = append(visited, input)
		if !opts.Force || filepath.Dir(opts.Output) != filepath.Join(dir, "out") {
			t.Fatalf("bad options: %+v", opts)
		}
		if input == a {
			return export.Result{}, errors.New("malformed archive")
		}
		return export.Result{Output: opts.Output, Topics: 5, Sheets: 1, Images: 2, Warnings: []string{"unsupported marker"}}, nil
	}
	code := run(context.Background(), []string{b, a, "-f", "-d", filepath.Join(dir, "out")}, strings.NewReader(""), &out, &stderr, "test", convert)
	if code != 1 || !reflect.DeepEqual(visited, []string{a, b}) {
		t.Fatalf("code=%d visited=%v", code, visited)
	}
	if !strings.Contains(out.String(), "1 exported, 1 failed") || !strings.Contains(stderr.String(), "malformed archive") || !strings.Contains(stderr.String(), "Warning:") {
		t.Fatalf("stdout=%s stderr=%s", out.String(), stderr.String())
	}
}

func TestCLIUsageAndCancellation(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"help", []string{"--help"}, 0, "Usage:"},
		{"version", []string{"--version"}, 0, "xmind-md test"},
		{"no terminal", nil, 2, "requires a terminal"},
		{"explicit tui", []string{"--tui"}, 2, "requires a terminal"},
		{"incompatible destinations", []string{"-o", "a.md", "-d", "out"}, 2, "cannot be combined"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := run(context.Background(), tt.args, strings.NewReader(""), &out, &stderr, "test", nil)
			if code != tt.code || !strings.Contains(out.String()+stderr.String(), tt.want) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	file := fixture(t, t.TempDir(), "map.xmind")
	var out, stderr bytes.Buffer
	if code := run(ctx, []string{file}, strings.NewReader(""), &out, &stderr, "test", nil); code != 130 {
		t.Fatalf("cancellation code=%d", code)
	}
}

func TestMissingInputStillExportsValidFile(t *testing.T) {
	dir := t.TempDir()
	file := fixture(t, dir, "good.xmind")
	var out, stderr bytes.Buffer
	called := 0
	code := run(context.Background(), []string{filepath.Join(dir, "missing.xmind"), file}, strings.NewReader(""), &out, &stderr, "test", func(_ context.Context, _ string, opts export.Options) (export.Result, error) {
		called++
		return export.Result{Output: opts.Output}, nil
	})
	if code != 1 || called != 1 {
		t.Fatalf("code=%d called=%d errors=%s", code, called, stderr.String())
	}
}

func TestSafeTerminalText(t *testing.T) {
	got := safe("map\x1b[2J\n\u202efile")
	if strings.ContainsAny(got, "\x1b\n\u202e") {
		t.Fatalf("unsafe output: %q", got)
	}
	if safe("中文 map.xmind") != "中文 map.xmind" {
		t.Fatal("printable Unicode changed")
	}
}
