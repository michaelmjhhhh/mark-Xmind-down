package export_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	exporter "github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

func TestLinkDestinationsPreserveEntityLikeQueries(t *testing.T) {
	input := fixture(t, []any{map[string]any{"rootTopic": map[string]any{
		"title": "Root", "href": "https://example.com/?x=&copy;&a=1&b=2",
		"image": map[string]any{"src": "https://example.com/a.png?x=&copy;&a=1&b=2"},
	}}}, nil)
	output := filepath.Join(t.TempDir(), "urls.md")
	if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output}); err != nil {
		t.Fatal(err)
	}
	markdown := string(readFile(t, output))
	for _, expected := range []string{
		"[Root](https://example.com/?x=&amp;copy;&amp;a=1&amp;b=2)",
		"![Root](https://example.com/a.png?x=&amp;copy;&amp;a=1&amp;b=2)",
	} {
		if !strings.Contains(markdown, expected) {
			t.Errorf("destination entity will alter URL; missing %s", expected)
		}
	}
}

func TestPlainTextBlockMarkersRemainLiteral(t *testing.T) {
	values := []string{"- literal", "+ literal", "---", "===", "1. numbered", "2) numbered", "10.2 decimal", "ordinary-dash"}
	var children []any
	for _, title := range values {
		children = append(children, map[string]any{"title": title})
	}
	input := fixture(t, []any{map[string]any{"rootTopic": map[string]any{
		"title": "Root", "notes": map[string]any{"plain": map[string]any{"content": strings.Join(values, "\n")}},
		"children": map[string]any{"attached": []any{map[string]any{"title": "Branch", "children": map[string]any{"attached": children}}}},
	}}}, nil)
	output := filepath.Join(t.TempDir(), "markers.md")
	if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output}); err != nil {
		t.Fatal(err)
	}
	markdown := string(readFile(t, output))
	for _, line := range []string{`\- literal`, `\+ literal`, `\---`, `\===`, `1\. numbered`, `2\) numbered`, `10.2 decimal`, `ordinary-dash`} {
		for _, prefix := range []string{"- ", "> "} {
			if !strings.Contains(markdown, prefix+line+"\n") {
				t.Errorf("plain title/note marker not preserved: %q in %s", prefix+line, markdown)
			}
		}
	}
}
