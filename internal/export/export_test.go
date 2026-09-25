package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	exporter "github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
)

var assetLinks = regexp.MustCompile(`\]\((assets/[a-f0-9]+\.[a-z0-9]+)\)`)

func TestSuppliedSamplesPreserveEveryImageAndRepeatExactly(t *testing.T) {
	samples := []struct {
		name           string
		topics, images int
	}{
		{"10.1 Low unemployment.xmind", 104, 6},
		{"10.2 Low and stable rate of inflation.xmind", 286, 23},
		{"10.3 Exploring the relationship between unemployment and inflation.xmind", 21, 8},
	}
	for _, sample := range samples {
		t.Run(sample.name, func(t *testing.T) {
			input := filepath.Join("..", "..", "assets", sample.name)
			expected := sampleImages(t, input)
			if len(expected) != sample.images {
				t.Fatalf("fixture contains %d unique images; want %d", len(expected), sample.images)
			}
			output := filepath.Join(t.TempDir(), "export.md")
			result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
			if err != nil {
				t.Fatal(err)
			}
			if result.Topics != sample.topics || result.Images != sample.images || result.Sheets != 1 {
				t.Fatalf("unexpected counts: %+v; want %d topics / %d images / 1 sheet", result, sample.topics, sample.images)
			}
			if len(result.Warnings) != 0 {
				t.Fatalf("sample produced warnings: %v", result.Warnings)
			}
			markdown := readFile(t, output)
			assertSampleTopicSequence(t, input, string(markdown))
			if bytes.Contains(markdown, []byte("xap:")) || bytes.Contains(markdown, []byte("Warning")) {
				t.Fatal("output contains unconverted resource or legacy warning document")
			}
			if !bytes.HasSuffix(markdown, []byte("\n")) || bytes.HasSuffix(markdown, []byte("\n\n")) {
				t.Fatal("Markdown must have exactly one final newline")
			}
			links := assetLinks.FindAllSubmatch(markdown, -1)
			if len(links) != sample.images {
				t.Fatalf("got %d image links, want %d", len(links), sample.images)
			}
			seen := map[string]bool{}
			for _, link := range links {
				data := readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(string(link[1]))))
				hash := fmt.Sprintf("%x", sha256.Sum256(data))
				original, ok := expected[hash]
				if !ok || !bytes.Equal(original, data) {
					t.Fatalf("asset %s differs from every source image", link[1])
				}
				seen[hash] = true
			}
			if len(seen) != len(expected) {
				t.Fatalf("lost images: exported %d of %d", len(seen), len(expected))
			}
			entries, err := os.ReadDir(filepath.Join(filepath.Dir(output), "assets"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != sample.images {
				t.Fatalf("got %d extracted assets; want %d", len(entries), sample.images)
			}
			before := snapshot(t, filepath.Dir(output))
			if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output}); err == nil {
				t.Fatal("existing Markdown was overwritten without force")
			}
			assertSnapshot(t, before, snapshot(t, filepath.Dir(output)))
			if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output, Force: true}); err != nil {
				t.Fatal(err)
			}
			assertSnapshot(t, before, snapshot(t, filepath.Dir(output)))
		})
	}
}

func TestExportMultipleSheetsTextLinksAndDeduplicatedAssets(t *testing.T) {
	image := pngImage(t)
	document := []any{
		map[string]any{"id": "sheet-one", "title": "工作表 α", "rootTopic": map[string]any{
			"id": "root", "title": "Root *literal* <script>alert(1)</script>",
			"children": map[string]any{"attached": []any{
				map[string]any{"id": "first", "title": "中文 [brackets] & detail\r\nsecond line", "notes": map[string]any{"plain": map[string]any{"content": "first <tag>\nsecond *literal*"}}, "children": map[string]any{"attached": []any{
					map[string]any{"id": "image-one", "image": map[string]any{"src": "xap:resources/one.png"}},
					map[string]any{"id": "image-two", "title": "Repeated diagram", "image": map[string]any{"src": "xap:resources/two.png"}},
					map[string]any{"id": "file", "title": "Download PDF", "href": "xap:resources/report.pdf"},
					map[string]any{"id": "website", "title": "Official page", "href": "https://example.com/docs?q=a b(c)"},
					map[string]any{"id": "internal", "title": "Go to other sheet", "href": "xmind:#sheet-two"},
				}},
				},
			}},
		}},
		map[string]any{"id": "sheet-two", "title": "Second", "rootTopic": map[string]any{"id": "target", "title": "Second root 😃"}},
	}
	input := fixture(t, document, map[string][]byte{"resources/one.png": image, "resources/two.png": image, "resources/report.pdf": []byte("%PDF-1.4\nsource attachment\n")})
	output := filepath.Join(t.TempDir(), "unicode output.md")
	result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Topics != 8 || result.Images != 1 || result.Sheets != 2 {
		t.Fatalf("wrong counts: %+v", result)
	}
	markdown := string(readFile(t, output))
	for _, text := range []string{
		`# Root \*literal\* &lt;script&gt;alert(1)&lt;/script&gt;`,
		`中文 \[brackets\] &amp; detail<br>second line`,
		`> first &lt;tag&gt;`, `> second \*literal\*`,
		`工作表 α`, `Second root 😃`,
		`[Official page](https://example.com/docs?q=a%20b%28c%29)`,
		`[Download PDF](assets/`, `[Go to other sheet](#topic-`,
	} {
		if !strings.Contains(markdown, text) {
			t.Errorf("Markdown missing %q:\n%s", text, markdown)
		}
	}
	if strings.Contains(markdown, "<script>") || strings.Contains(markdown, "\r") {
		t.Fatal("unsafe HTML or CR characters in output")
	}
	assertInOrder(t, markdown, "Root ", "中文", "Repeated diagram", "Download PDF", "Official page", "Go to other sheet", "# Second root")
	links := assetLinks.FindAllStringSubmatch(markdown, -1)
	if len(links) != 3 {
		t.Fatalf("got %d asset references, want two images and attachment", len(links))
	}
	if links[0][1] != links[1][1] {
		t.Fatal("identical image bytes under different ZIP paths must share the same asset")
	}
	if data := readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(links[0][1]))); !bytes.Equal(data, image) {
		t.Fatal("image bytes changed")
	}
	if data := readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(links[2][1]))); string(data) != "%PDF-1.4\nsource attachment\n" {
		t.Fatal("attachment bytes changed")
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(output), "assets"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("deduplication wrote %d files, want 2", len(entries))
	}
	anchorRefs := regexp.MustCompile(`\]\(#(topic-[a-z0-9]+)\)`).FindAllStringSubmatch(markdown, -1)
	for _, ref := range anchorRefs {
		if !strings.Contains(markdown, `id="`+ref[1]+`"`) {
			t.Fatalf("internal link %q has no target anchor", ref[1])
		}
	}
}

func TestDeepHierarchyRetainsOrderAndFloatingTopics(t *testing.T) {
	var descendant map[string]any
	for i := 39; i >= 0; i-- {
		node := map[string]any{"id": fmt.Sprintf("node-%02d", i), "title": fmt.Sprintf("Node %02d", i)}
		if descendant != nil {
			node["children"] = map[string]any{"attached": []any{descendant}}
		}
		descendant = node
	}
	root := map[string]any{"id": "root", "title": "Deep root", "children": map[string]any{
		"attached": []any{descendant},
		"detached": []any{map[string]any{"id": "floating", "title": "Floating branch"}},
		"summary":  []any{map[string]any{"id": "summary", "title": "A summary"}},
		"callout":  []any{map[string]any{"id": "callout", "title": "A callout"}},
	}}
	input := fixture(t, []any{map[string]any{"rootTopic": root}}, nil)
	output := filepath.Join(t.TempDir(), "deep.md")
	result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Topics != 44 {
		t.Fatalf("got %d topics; want 44", result.Topics)
	}
	markdown := string(readFile(t, output))
	ordered := []string{"Deep root"}
	for i := 0; i < 40; i++ {
		ordered = append(ordered, fmt.Sprintf("Node %02d", i))
	}
	assertInOrder(t, markdown, ordered...)
	for _, title := range []string{"Floating branch", "A summary", "A callout"} {
		if strings.Count(markdown, title) != 1 {
			t.Errorf("topic %q missing or duplicated", title)
		}
	}
	before := readFile(t, output)
	if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output, Force: true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readFile(t, output)) {
		t.Fatal("child-group traversal order is nondeterministic")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(output), "assets")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("text-only export should not create assets directory")
	}
}

func TestFailureDoesNotPublishOutput(t *testing.T) {
	for _, test := range []struct {
		name      string
		root      map[string]any
		resources map[string][]byte
		cancel    bool
	}{
		{"missing-image", map[string]any{"title": "Missing", "image": map[string]any{"src": "xap:resources/missing.png"}}, nil, false},
		{"missing-second-image", map[string]any{"title": "Good first", "image": map[string]any{"src": "xap:resources/one.png"}, "children": map[string]any{"attached": []any{map[string]any{"title": "Bad later", "image": map[string]any{"src": "xap:resources/missing.png"}}}}}, map[string][]byte{"resources/one.png": pngImage(t)}, false},
		{"cancelled", map[string]any{"title": "Cancelled", "image": map[string]any{"src": "xap:resources/one.png"}}, map[string][]byte{"resources/one.png": pngImage(t)}, true},
		{"archive-traversal", map[string]any{"title": "Safe topic"}, map[string][]byte{"../outside.png": pngImage(t)}, false},
		{"encoded-resource-traversal", map[string]any{"title": "Bad reference", "image": map[string]any{"src": "xap:resources/%2e%2e/outside.png"}}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := fixture(t, []any{map[string]any{"rootTopic": test.root}}, test.resources)
			outputDir := t.TempDir()
			ctx := context.Background()
			if test.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := exporter.Convert(ctx, input, exporter.Options{Output: filepath.Join(outputDir, "result.md")})
			if err == nil {
				t.Fatal("invalid export unexpectedly succeeded")
			}
			if test.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want cancellation", err)
			}
			if got := snapshot(t, outputDir); len(got) != 0 {
				t.Fatalf("failed export left output: %v", got)
			}
			entries, err := os.ReadDir(outputDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("failed export created an output directory or file")
			}
		})
	}
}

func TestLegacyXMLExportPreservesNotesImagesAndRelationships(t *testing.T) {
	content := `<?xml version="1.0" encoding="UTF-8"?>
<xmap-content xmlns="urn:xmind:xmap:xmlns:content:2.0" xmlns:h="http://www.w3.org/1999/xhtml" xmlns:link="http://www.w3.org/1999/xlink">
<sheet id="legacy-sheet"><title>Legacy sheet</title><topic id="legacy-root"><title>Legacy 中文</title>
<children><topics type="attached"><topic id="legacy-child"><title>Old image &amp; notes</title>
<h:img h:src="xap:attachments/diagram.png"/>
<notes><html><h:p>Visible <h:span>rich text</h:span></h:p><h:p>Second paragraph<h:br/>next line<h:img h:src="xap:attachments/note.png"/></h:p></html></notes>
<labels><label>important</label></labels><marker-refs><marker-ref marker-id="priority-1"/></marker-refs>
<children><topics type="attached"><topic id="legacy-file" link:href="xap:attachments/document.txt"><title>Legacy attachment</title></topic></topics></children>
</topic></topics><topics type="detached"><topic id="floating"><title>Loose topic</title></topic></topics></children>
</topic><relationships><relationship end1="legacy-child" end2="floating"><title>Related ideas</title></relationship></relationships></sheet>
</xmap-content>`
	image := pngImage(t)
	input := zipFixture(t, map[string][]byte{"content.xml": []byte(content), "attachments/diagram.png": image, "attachments/note.png": image, "attachments/document.txt": []byte("legacy attachment bytes")})
	output := filepath.Join(t.TempDir(), "legacy.md")
	result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Topics != 4 || result.Images != 1 || result.Sheets != 1 {
		t.Fatalf("wrong legacy counts: %+v", result)
	}
	markdown := string(readFile(t, output))
	for _, text := range []string{"# Legacy 中文", "Old image &amp; notes", "Visible rich text", "Second paragraph", "next line", "Labels: important", "Markers: priority-1", "Legacy attachment", "Loose topic", "Related ideas"} {
		if !strings.Contains(markdown, text) {
			t.Errorf("legacy export missing %q:\n%s", text, markdown)
		}
	}
	links := assetLinks.FindAllStringSubmatch(markdown, -1)
	if len(links) != 3 {
		t.Fatalf("legacy output has %d assets, want topic image, note image and attachment", len(links))
	}
	if links[0][1] != links[1][1] {
		t.Fatal("legacy topic and note image were not deduplicated")
	}
	if !bytes.Equal(readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(links[0][1]))), image) {
		t.Fatal("legacy image bytes changed")
	}
	if string(readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(links[2][1])))) != "legacy attachment bytes" {
		t.Fatal("legacy attachment bytes changed")
	}
	refs := regexp.MustCompile(`\]\(#(topic-[a-z0-9]+)\)`).FindAllStringSubmatch(markdown, -1)
	if len(refs) != 2 {
		t.Fatalf("expected two relationship links; got %d", len(refs))
	}
	for _, ref := range refs {
		if !strings.Contains(markdown, `id="`+ref[1]+`"`) {
			t.Fatalf("relationship target %q has no anchor", ref[1])
		}
	}
}

func TestModernRichNotesExportEmbeddedImages(t *testing.T) {
	root := map[string]any{"title": "Rich notes", "notes": map[string]any{
		"plain": map[string]any{"content": "Plain text has priority"},
		"html": map[string]any{"content": map[string]any{"paragraphs": []any{map[string]any{"spans": []any{
			map[string]any{"text": "Rich text fallback"}, map[string]any{"image": "xap:resources/note.png"},
		}}}}},
	}}
	input := fixture(t, []any{map[string]any{"rootTopic": root}}, map[string][]byte{"resources/note.png": pngImage(t)})
	output := filepath.Join(t.TempDir(), "notes.md")
	result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Images != 1 {
		t.Fatalf("rich-note image missing from export: %+v", result)
	}
	markdown := string(readFile(t, output))
	if !strings.Contains(markdown, "> Plain text has priority") {
		t.Fatal("plain-note text was lost")
	}
	if strings.Contains(markdown, "Rich text fallback") {
		t.Fatal("equivalent rich-note text was duplicated alongside plain notes")
	}
	links := assetLinks.FindAllStringSubmatch(markdown, -1)
	if len(links) != 1 {
		t.Fatalf("got %d note image references; want 1", len(links))
	}
	if !bytes.Equal(readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(links[0][1]))), pngImage(t)) {
		t.Fatal("rich-note image changed")
	}
}

func TestBrokenModernContentNeverFallsBackToWarningXML(t *testing.T) {
	input := zipFixture(t, map[string][]byte{
		"content.json": []byte(`[{broken`),
		"content.xml":  []byte(`<xmap-content><sheet><topic><title>Compatibility warning</title></topic></sheet></xmap-content>`),
	})
	dir := t.TempDir()
	if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: filepath.Join(dir, "bad.md")}); err == nil {
		t.Fatal("invalid modern content was silently replaced with legacy content")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatal("malformed modern document left output")
	}
}

func TestForceFailurePreservesExistingMarkdown(t *testing.T) {
	input := fixture(t, []any{map[string]any{"rootTopic": map[string]any{"title": "Missing", "image": map[string]any{"src": "xap:resources/missing.png"}}}}, nil)
	dir := t.TempDir()
	output := filepath.Join(dir, "important.md")
	if err := os.WriteFile(output, []byte("# Previously exported document\n"), 0644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output, Force: true}); err == nil {
		t.Fatal("missing image accepted")
	}
	assertSnapshot(t, before, snapshot(t, dir))
}

func TestExistingAssetCorruptionIsNeverOverwritten(t *testing.T) {
	input := imageFixture(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first.md")
	if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: first}); err != nil {
		t.Fatal(err)
	}
	links := assetLinks.FindAllSubmatch(readFile(t, first), -1)
	if len(links) != 1 {
		t.Fatal("fixture did not export one image")
	}
	asset := filepath.Join(dir, filepath.FromSlash(string(links[0][1])))
	data := readFile(t, asset)
	data[len(data)-1] ^= 1 // Same size, different bytes: length checks alone cannot detect corruption.
	if err := os.WriteFile(asset, data, 0644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: filepath.Join(dir, "second.md"), Force: true}); err == nil {
		t.Fatal("corrupt content-addressed asset was accepted")
	}
	assertSnapshot(t, before, snapshot(t, dir))
}

func TestSymlinkOutputAndAssetsAreRejected(t *testing.T) {
	input := imageFixture(t)
	for _, kind := range []string{"markdown", "assets-directory", "asset-file"} {
		t.Run(kind, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			output := filepath.Join(dir, "result.md")
			sentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(sentinel, []byte("keep me"), 0644); err != nil {
				t.Fatal(err)
			}
			var target, link string
			switch kind {
			case "markdown":
				target, link = sentinel, output
			case "assets-directory":
				target, link = outside, filepath.Join(dir, "assets")
			case "asset-file":
				if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output}); err != nil {
					t.Fatal(err)
				}
				links := assetLinks.FindAllSubmatch(readFile(t, output), -1)
				link = filepath.Join(dir, filepath.FromSlash(string(links[0][1])))
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(output); err != nil {
					t.Fatal(err)
				}
				target = sentinel
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			outsideBefore := snapshot(t, outside)
			if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output, Force: true}); err == nil {
				t.Fatal("symlink output accepted")
			}
			assertSnapshot(t, outsideBefore, snapshot(t, outside))
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("symlink was removed or replaced")
			}
			if kind != "markdown" {
				if _, err := os.Stat(output); !errors.Is(err, fs.ErrNotExist) {
					t.Fatal("failed symlink check published Markdown")
				}
			}
		})
	}
}

func TestExternalImageIsKeptWithoutNetworkAccess(t *testing.T) {
	root := map[string]any{"title": "Remote", "image": map[string]any{"src": "https://example.invalid/image with (spaces).png"}}
	input := fixture(t, []any{map[string]any{"rootTopic": root}}, nil)
	output := filepath.Join(t.TempDir(), "remote.md")
	result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Images != 0 || len(result.Warnings) == 0 {
		t.Fatalf("remote-image result must distinguish external images: %+v", result)
	}
	if !strings.Contains(string(readFile(t, output)), `![Remote](https://example.invalid/image%20with%20%28spaces%29.png)`) {
		t.Fatal("external image URL was not retained and escaped")
	}
}

func imageFixture(t *testing.T) string {
	t.Helper()
	return fixture(t, []any{map[string]any{"rootTopic": map[string]any{"title": "Image", "image": map[string]any{"src": "xap:resources/image.png"}}}}, map[string][]byte{"resources/image.png": pngImage(t)})
}

func fixture(t *testing.T, document any, resources map[string][]byte) string {
	t.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{"content.json": data}
	for name, data := range resources {
		entries[name] = data
	}
	return zipFixture(t, entries)
}

func zipFixture(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	input := filepath.Join(t.TempDir(), "fixture.xmind")
	f, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entries[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return input
}

func pngImage(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6uAAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sampleImages(t *testing.T, input string) map[string][]byte {
	t.Helper()
	z, err := zip.OpenReader(input)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	entries := map[string]*zip.File{}
	for _, f := range z.File {
		entries[f.Name] = f
	}
	read := func(name string) []byte {
		f, ok := entries[name]
		if !ok {
			t.Fatalf("source references missing archive entry %s", name)
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var sheets []struct {
		Root map[string]any `json:"rootTopic"`
	}
	if err := json.Unmarshal(read("content.json"), &sheets); err != nil {
		t.Fatal(err)
	}
	images := map[string][]byte{}
	var walk func(map[string]any)
	walk = func(topic map[string]any) {
		if image, ok := topic["image"].(map[string]any); ok {
			ref := strings.TrimPrefix(image["src"].(string), "xap:")
			data := read(ref)
			images[fmt.Sprintf("%x", sha256.Sum256(data))] = data
		}
		if groups, ok := topic["children"].(map[string]any); ok {
			for _, children := range groups {
				for _, child := range children.([]any) {
					walk(child.(map[string]any))
				}
			}
		}
	}
	for _, sheet := range sheets {
		walk(sheet.Root)
	}
	return images
}

// Compare every emitted topic against the source order, independently of the
// production parser and renderer. Decoding the Markdown back to plain titles
// detects omissions, duplicated titles, reordering, and escaping corruption.
func assertSampleTopicSequence(t *testing.T, input, markdown string) {
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
		data, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	type sourceTopic struct {
		Title string `json:"title"`
		Image *struct {
			Src string `json:"src"`
		} `json:"image"`
		Children map[string][]*sourceTopic `json:"children"`
	}
	var sheets []struct {
		Root *sourceTopic `json:"rootTopic"`
	}
	if err := json.Unmarshal(read("content.json"), &sheets); err != nil {
		t.Fatal(err)
	}
	var expected []string
	var walk func(*sourceTopic)
	walk = func(topic *sourceTopic) {
		title := strings.TrimSpace(strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(topic.Title))
		if title == "" {
			if topic.Image != nil {
				data := read(strings.TrimPrefix(topic.Image.Src, "xap:"))
				title = fmt.Sprintf("image:%x", sha256.Sum256(data))
			} else {
				title = "Untitled topic"
			}
		}
		expected = append(expected, title)
		// The supplied corpus only uses attached children. Make a changed
		// corpus explicit instead of silently ignoring newly added groups.
		for kind, children := range topic.Children {
			if kind != "attached" && len(children) > 0 {
				t.Fatalf("sample introduced child group %q; expand sequence oracle", kind)
			}
		}
		for _, child := range topic.Children["attached"] {
			walk(child)
		}
	}
	for _, sheet := range sheets {
		walk(sheet.Root)
	}

	var actual []string
	topicLine := regexp.MustCompile(`^(?:#{1,2} | *- )(.*)$`)
	imageOnly := regexp.MustCompile(`^!\[Image\]\(assets/([a-f0-9]{64})\.[a-z0-9]+\)$`)
	// CommonMark allows escaping ASCII punctuation. Decode escapes before
	// HTML entities so encoded literal punctuation remains literal text.
	escapedPunctuation := regexp.MustCompile(`\\([[:punct:]])`)
	for _, line := range strings.Split(markdown, "\n") {
		match := topicLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		title := match[1]
		if image := imageOnly.FindStringSubmatch(title); image != nil {
			title = "image:" + image[1]
		} else {
			title = strings.ReplaceAll(title, "<br>", "\n")
			title = escapedPunctuation.ReplaceAllString(title, "$1")
			title = html.UnescapeString(title)
		}
		actual = append(actual, strings.TrimSpace(title))
	}
	if len(actual) != len(expected) {
		t.Fatalf("Markdown has %d topic entries; source has %d", len(actual), len(expected))
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("topic %d changed or reordered:\nsource: %q\noutput: %q", i+1, expected[i], actual[i])
		}
	}
}

func readFile(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertSnapshot(t *testing.T, want, got map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatal("export modified existing output bytes or file set")
	}
}

func assertInOrder(t *testing.T, text string, pieces ...string) {
	t.Helper()
	position := 0
	for _, piece := range pieces {
		next := strings.Index(text[position:], piece)
		if next < 0 {
			t.Fatalf("missing or out-of-order text %q", piece)
		}
		position += next + len(piece)
	}
}
