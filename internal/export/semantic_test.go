package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	exporter "github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	htmlrenderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

// These checks inspect a separately implemented CommonMark/GFM parser, rather
// than guessing rendered structure from the exporter's own Markdown syntax.
type semanticTopic struct {
	Title  string
	Depth  int
	Parent int
}

func semanticMarkdown() goldmark.Markdown {
	return goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(htmlrenderer.WithUnsafe()))
}

func semanticExport(t *testing.T, input string) ([]byte, ast.Node) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "result.md")
	if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output}); err != nil {
		t.Fatal(err)
	}
	source := readFile(t, output)
	return source, semanticMarkdown().Parser().Parse(text.NewReader(source))
}

var semanticHTMLTag = regexp.MustCompile(`<[^>]*>`)

func semanticText(t *testing.T, node ast.Node, source []byte) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := semanticMarkdown().Renderer().Render(&rendered, source, node); err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(rendered.String(), "<br>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	return strings.TrimSpace(html.UnescapeString(semanticHTMLTag.ReplaceAllString(s, "")))
}

func semanticTitle(t *testing.T, node ast.Node, source []byte) string {
	t.Helper()
	if img, ok := node.FirstChild().(*ast.Image); ok && img.NextSibling() == nil {
		name := filepath.Base(string(img.Destination))
		return "image:" + strings.TrimSuffix(name, filepath.Ext(name))
	}
	return semanticText(t, node, source)
}

func semanticTopics(t *testing.T, doc ast.Node, source []byte) []semanticTopic {
	t.Helper()
	var got []semanticTopic
	root, branch := -1, -1
	items := map[ast.Node]int{}
	if err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			if n.Level == 1 {
				root, branch = len(got), -1
				got = append(got, semanticTopic{semanticTitle(t, n, source), 0, -1})
			} else if n.Level == 2 {
				branch = len(got)
				got = append(got, semanticTopic{semanticTitle(t, n, source), 1, root})
			} else {
				t.Errorf("unexpected level %d heading: %q", n.Level, semanticText(t, n, source))
			}
		case *ast.ListItem:
			depth, parent := 2, branch
			for ancestor := n.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
				if _, ok := ancestor.(*ast.ListItem); ok {
					depth++
					if parent == branch {
						parent = items[ancestor]
					}
				}
			}
			items[n] = len(got)
			got = append(got, semanticTopic{semanticTitle(t, n.FirstChild(), source), depth, parent})
		case *ast.CodeBlock, *ast.FencedCodeBlock, *ast.CodeSpan, *ast.ThematicBreak, *ast.Emphasis:
			t.Errorf("source plain text became unintended %s", n.Kind())
		}
		return ast.WalkContinue, nil
	}); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSemanticSuppliedSamplesPreserveRenderedTopicHierarchy(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("..", "..", "assets", "*.xmind"))
	if err != nil || len(inputs) != 3 {
		t.Fatalf("sample corpus: %v, %v", inputs, err)
	}
	for _, input := range inputs {
		t.Run(filepath.Base(input), func(t *testing.T) {
			source, doc := semanticExport(t, input)
			want := semanticSampleOracle(t, input)
			got := semanticTopics(t, doc, source)
			if len(got) != len(want) {
				t.Fatalf("rendered %d topics, source has %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("topic %d rendered differently:\n got: %+v\nwant: %+v", i, got[i], want[i])
				}
			}
		})
	}
}

// Read the original ZIP/JSON independently of the production XMind parser.
func semanticSampleOracle(t *testing.T, input string) []semanticTopic {
	t.Helper()
	z, err := zip.OpenReader(input)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	read := func(name string) []byte {
		f, err := z.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	type rawTopic struct {
		Title    string                 `json:"title"`
		Image    struct{ Src string }   `json:"image"`
		Children map[string][]*rawTopic `json:"children"`
	}
	var sheets []struct {
		Root *rawTopic `json:"rootTopic"`
	}
	if err := json.Unmarshal(read("content.json"), &sheets); err != nil {
		t.Fatal(err)
	}
	var topics []semanticTopic
	var visit func(*rawTopic, int, int)
	visit = func(raw *rawTopic, depth, parent int) {
		title := strings.TrimSpace(strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(raw.Title))
		if title == "" {
			if raw.Image.Src != "" {
				title = fmt.Sprintf("image:%x", sha256.Sum256(read(strings.TrimPrefix(raw.Image.Src, "xap:"))))
			} else {
				title = "Untitled topic"
			}
		}
		index := len(topics)
		topics = append(topics, semanticTopic{title, depth, parent})
		for kind, children := range raw.Children {
			if kind != "attached" && len(children) != 0 {
				t.Fatalf("extend sample oracle for group %q", kind)
			}
		}
		for _, child := range raw.Children["attached"] {
			visit(child, depth+1, index)
		}
	}
	for _, sheet := range sheets {
		visit(sheet.Root, 0, -1)
	}
	return topics
}

func TestSemanticPlainTitlesRemainLiteralInGFM(t *testing.T) {
	titles := []string{
		"# heading", "## heading ##", "---", "===", "***", "___", "~~~", "```go", "`code`",
		"- item", "+ item", "* item", "1. item", "2) item", "[x] checked", "[ ] unchecked",
		"**bold** _italic_ ~~deleted~~ ~deleted~", "[link](https://example.org)", "![image](image.png)",
		"<script>alert('x')</script>", "<https://example.org>", "A &copy; &#42; & B", "|a|b|", "<!-- comment -->",
		"line one\nline two", "first\n\nthird", "literal <br>\nreal break", "中文 😃 é", `C:\Users\name\file`, "backslash \\\n* line\n\n1. line",
		"https://example.org/a_b?q=1&x=2", "www.example.org", "person@example.org",
	}
	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			for _, level := range []int{0, 1, 2} {
				node := map[string]any{"title": title}
				for i := level; i > 0; i-- {
					node = map[string]any{"title": fmt.Sprintf("parent %d", i), "children": map[string]any{"attached": []any{node}}}
				}
				source, doc := semanticExport(t, fixture(t, []any{map[string]any{"rootTopic": node}}, nil))
				got := semanticTopics(t, doc, source)
				if len(got) != level+1 || got[level].Title != title {
					t.Errorf("level %d: literal title changed in rendered GFM: got %+v, want %q\n%s", level, got, title, source)
				}
				_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
					if entering {
						switch n.Kind().String() {
						case "Strikethrough", "TaskCheckBox", "Link", "Image", "Table":
							t.Errorf("literal title became unintended %s", n.Kind())
						}
					}
					return ast.WalkContinue, nil
				})
			}
		})
	}
}

func TestSemanticDetailsStayInsideCorrectListItem(t *testing.T) {
	leaf := map[string]any{"id": "leaf", "title": "Leaf", "notes": map[string]any{"plain": map[string]any{"content": "Leaf note"}}, "image": map[string]any{"src": "xap:resources/image.png"}}
	parent := map[string]any{"id": "parent", "title": "Parent", "notes": map[string]any{"plain": map[string]any{"content": "Parent note"}}, "labels": []any{"label"}, "markers": []any{map[string]any{"markerId": "priority-1"}}, "image": map[string]any{"src": "xap:resources/image.png"}, "children": map[string]any{"attached": []any{leaf}}}
	branch := map[string]any{"title": "Branch", "children": map[string]any{"attached": []any{parent, map[string]any{"title": "Sibling", "href": "xmind:#leaf"}}}}
	root := map[string]any{"title": "Root", "children": map[string]any{"attached": []any{branch}}}
	source, doc := semanticExport(t, fixture(t, []any{map[string]any{"rootTopic": root}}, map[string][]byte{"resources/image.png": pngImage(t)}))
	want := []semanticTopic{{"Root", 0, -1}, {"Branch", 1, 0}, {"Parent", 2, 1}, {"Leaf", 3, 2}, {"Sibling", 2, 1}}
	if got := semanticTopics(t, doc, source); !reflect.DeepEqual(got, want) {
		t.Fatalf("details broke hierarchy: got %+v, want %+v\n%s", got, want, source)
	}
	var images, notes, links int
	anchors := map[string]bool{}
	var internalLinks []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		owner := func() string {
			for parent := n.Parent(); parent != nil; parent = parent.Parent() {
				if _, ok := parent.(*ast.ListItem); ok {
					return semanticTitle(t, parent.FirstChild(), source)
				}
			}
			return ""
		}
		switch n := n.(type) {
		case *ast.Image:
			images++
			if parent := owner(); parent != "Parent" && parent != "Leaf" {
				t.Errorf("image escaped its topic: owner %q", parent)
			}
		case *ast.Blockquote:
			notes++
			if got, parent := semanticText(t, n, source), owner(); got != parent+" note" {
				t.Errorf("note belongs to %q but says %q", parent, got)
			}
		case *ast.Link:
			links++
			internalLinks = append(internalLinks, strings.TrimPrefix(string(n.Destination), "#"))
		case *ast.RawHTML:
			var buf bytes.Buffer
			for i := 0; i < n.Segments.Len(); i++ {
				segment := n.Segments.At(i)
				buf.Write(segment.Value(source))
			}
			for _, match := range regexp.MustCompile(`id="([^"]+)"`).FindAllStringSubmatch(buf.String(), -1) {
				anchors[match[1]] = true
			}
		}
		return ast.WalkContinue, nil
	})
	if images != 2 || notes != 2 || links != 1 {
		t.Errorf("details lost: images=%d notes=%d links=%d", images, notes, links)
	}
	for _, target := range internalLinks {
		if !anchors[target] {
			t.Errorf("rendered link #%s has no rendered target anchor", target)
		}
	}
}

func TestSemanticMaximumSupportedDepth(t *testing.T) {
	const depth = 256
	node := map[string]any{"title": fmt.Sprintf("topic %d", depth-1)}
	for i := depth - 2; i >= 0; i-- {
		node = map[string]any{"title": fmt.Sprintf("topic %d", i), "children": map[string]any{"attached": []any{node}}}
	}
	source, doc := semanticExport(t, fixture(t, []any{map[string]any{"rootTopic": node}}, nil))
	got := semanticTopics(t, doc, source)
	if len(got) != depth {
		t.Fatalf("%d-level input rendered %d topics", depth, len(got))
	}
	for i, topic := range got {
		if topic != (semanticTopic{fmt.Sprintf("topic %d", i), i, i - 1}) {
			t.Errorf("topic %d changed hierarchy: %+v", i, topic)
		}
	}
}

func TestSemanticConsecutiveSheetsHaveIndependentRoots(t *testing.T) {
	var sheets []any
	for i := range 3 {
		sheets = append(sheets, map[string]any{"rootTopic": map[string]any{"title": fmt.Sprintf("Sheet %d", i)}})
	}
	source, doc := semanticExport(t, fixture(t, sheets, nil))
	var roots []string
	var breaks int
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		switch n := n.(type) {
		case *ast.Heading:
			if n.Level != 1 {
				t.Errorf("sheet became heading level %d", n.Level)
			}
			roots = append(roots, semanticText(t, n, source))
		case *ast.ThematicBreak:
			breaks++
		default:
			t.Errorf("unexpected sheet-separator node: %s", n.Kind())
		}
	}
	if !reflect.DeepEqual(roots, []string{"Sheet 0", "Sheet 1", "Sheet 2"}) || breaks != 2 {
		t.Errorf("sheets merged: roots=%q separators=%d\n%s", roots, breaks, source)
	}
}

func TestSemanticRenderedURLsAndCrossSheetAnchors(t *testing.T) {
	const href = "https://example.org/a (b)?x=&copy;&a=1#frag"
	const src = "https://example.org/image (1).png?x=&copy;&a=1"
	root := map[string]any{
		"id": "root", "title": "Root", "href": href, "image": map[string]any{"src": src},
		"children": map[string]any{"attached": []any{map[string]any{
			"id": "branch", "title": "Branch", "children": map[string]any{"attached": []any{
				map[string]any{"id": "leaf", "title": "Leaf"},
				map[string]any{"title": "To leaf", "href": "xmind:#leaf"},
				map[string]any{"title": "To root", "href": "xmind:#root"},
				map[string]any{"title": "To other sheet", "href": "xmind:#other-sheet"},
			}},
		}}},
	}
	input := fixture(t, []any{
		map[string]any{"rootTopic": root, "relationships": []any{map[string]any{"end1Id": "branch", "end2Id": "leaf", "title": "Relation"}}},
		map[string]any{"id": "other-sheet", "rootTopic": map[string]any{"id": "other-root", "title": "Other root"}},
	}, nil)
	source, doc := semanticExport(t, input)
	var rendered bytes.Buffer
	if err := semanticMarkdown().Renderer().Render(&rendered, source, doc); err != nil {
		t.Fatal(err)
	}
	page := rendered.String()
	anchors := map[string]int{}
	for _, match := range regexp.MustCompile(`<a id="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		anchors[match[1]]++
	}
	internal, external := 0, 0
	for _, match := range regexp.MustCompile(`<a href="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		dest := html.UnescapeString(match[1])
		if strings.HasPrefix(dest, "#") {
			internal++
			if count := anchors[strings.TrimPrefix(dest, "#")]; count != 1 {
				t.Errorf("rendered link %q resolves to %d anchors", dest, count)
			}
		} else {
			external++
			if dest != "https://example.org/a%20%28b%29?x=&copy;&a=1#frag" {
				t.Errorf("rendered hyperlink changed destination: %q", dest)
			}
		}
	}
	if internal != 5 || external != 1 {
		t.Errorf("missing rendered hyperlinks: internal=%d external=%d\n%s", internal, external, page)
	}
	images := regexp.MustCompile(`<img src="([^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(images) != 1 || html.UnescapeString(images[0][1]) != "https://example.org/image%20%281%29.png?x=&copy;&a=1" {
		t.Errorf("rendered image destination changed: %q", images)
	}
}

func TestSemanticPlainNotesDoNotBecomeCode(t *testing.T) {
	note := "Intro\n\n    **indented** & <tag>\n    second line\n\n# literal heading\n- literal item\n1. literal number\n~~~\n[ ] task\n~~literal~~"
	root := map[string]any{"title": "Root", "notes": map[string]any{"plain": map[string]any{"content": note}}}
	source, doc := semanticExport(t, fixture(t, []any{map[string]any{"rootTopic": root}}, nil))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch n := n.(type) {
			case *ast.CodeBlock, *ast.FencedCodeBlock, *ast.CodeSpan, *ast.List, *ast.ThematicBreak, *ast.Emphasis:
				t.Errorf("plain note became unintended %s\n%s", n.Kind(), source)
			case *ast.Heading:
				if n.Level != 1 || semanticText(t, n, source) != "Root" {
					t.Errorf("plain note became heading")
				}
			case *ast.Blockquote:
				if got := semanticText(t, n, source); !reflect.DeepEqual(strings.Fields(got), strings.Fields(note)) {
					t.Errorf("visible note text changed:\n got: %q\nwant: %q", got, note)
				}
			}
		}
		return ast.WalkContinue, nil
	})
}
