package export_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	exporter "github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func TestContentQASheetSummaryAndUnresolvedReferencesSurvive(t *testing.T) {
	input := fixture(t, []any{map[string]any{"title": "Single sheet 中文", "rootTopic": map[string]any{
		"id": "root", "title": "Root", "href": "xmind:#unavailable",
		"summaries": []any{map[string]any{"range": "(0,1)", "topicId": "summary"}},
		"children":  map[string]any{"summary": []any{map[string]any{"id": "summary", "title": "Summary content"}}},
	}}}, nil)
	output := filepath.Join(t.TempDir(), "summary.md")
	result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	source := readFile(t, output)
	for _, piece := range []string{"Sheet: Single sheet 中文", "Link: xmind:\\#unavailable", "Summary ((0,1)) → [Summary content](#topic-"} {
		if !strings.Contains(string(source), piece) {
			t.Errorf("missing preserved field %q in %s", piece, source)
		}
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "unresolved topic link") {
		t.Fatalf("missing warning: %v", result.Warnings)
	}
	var rendered bytes.Buffer
	if err := semanticMarkdown().Convert(source, &rendered); err != nil {
		t.Fatal(err)
	}
	doc := semanticMarkdown().Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if link, ok := n.(*ast.Link); ok && entering && strings.HasPrefix(string(link.Destination), "#") {
			id := strings.TrimPrefix(string(link.Destination), "#")
			if strings.Count(rendered.String(), "id=\""+id+"\"") != 1 {
				t.Errorf("summary link has no unique target %s", id)
			}
		}
		return ast.WalkContinue, nil
	})
}

func TestContentQARichNoteLinksExtractAndResolveWithPlainAlternative(t *testing.T) {
	attachment := []byte("%PDF-1.4\noriginal note attachment")
	root := map[string]any{"id": "root", "title": "Root", "notes": map[string]any{
		"plain": map[string]any{"content": "Attachment and target"},
		"html": map[string]any{"content": map[string]any{"paragraphs": []any{map[string]any{"spans": []any{
			map[string]any{"href": "xap:resources/report.pdf", "spans": []any{map[string]any{"text": "Attachment"}}},
			map[string]any{"text": " and "}, map[string]any{"href": "xmind:#target", "spans": []any{map[string]any{"text": "target"}}},
		}}}}},
	}, "children": map[string]any{"attached": []any{map[string]any{"id": "target", "title": "Destination"}}}}
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "present", true: "missing"}[missing], func(t *testing.T) {
			resources := map[string][]byte{}
			if !missing {
				resources["resources/report.pdf"] = attachment
			}
			input := fixture(t, []any{map[string]any{"rootTopic": root}}, resources)
			output := filepath.Join(t.TempDir(), "notes.md")
			_, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
			if missing {
				if err == nil {
					t.Fatal("missing rich-note attachment accepted")
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("failed note attachment export published Markdown")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			source := readFile(t, output)
			doc := semanticMarkdown().Parser().Parse(text.NewReader(source))
			var rendered bytes.Buffer
			if err := semanticMarkdown().Renderer().Render(&rendered, source, doc); err != nil {
				t.Fatal(err)
			}
			links := 0
			_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if link, ok := n.(*ast.Link); ok && entering {
					links++
					dest := string(link.Destination)
					if strings.HasPrefix(dest, "assets/") {
						if !bytes.Equal(readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(dest))), attachment) {
							t.Error("note attachment bytes changed")
						}
					} else if strings.HasPrefix(dest, "#topic-") {
						if !strings.Contains(rendered.String(), "id=\""+dest[1:]+"\"") {
							t.Error("note target missing anchor")
						}
					} else {
						t.Errorf("unconverted note hyperlink: %s", dest)
					}
				}
				return ast.WalkContinue, nil
			})
			if links != 2 {
				t.Errorf("expected 2 converted note links, got %d", links)
			}
		})
	}
}
