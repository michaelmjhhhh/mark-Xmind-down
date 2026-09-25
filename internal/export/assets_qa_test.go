package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	exporter "github.com/michaelmjhhhh/mark-Xmind-down/internal/export"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Check actual CommonMark image nodes, not just Markdown-looking strings: a
// syntactically malformed image inside a code block must fail this oracle.
func qaImageDestinations(t *testing.T, markdown []byte) []string {
	t.Helper()
	var destinations []string
	doc := goldmark.New().Parser().Parse(text.NewReader(markdown))
	err := ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if img, ok := node.(*ast.Image); ok && entering {
			destinations = append(destinations, string(img.Destination))
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return destinations
}

func qaCheckImageBytes(t *testing.T, output string, expected [][]byte) {
	t.Helper()
	destinations := qaImageDestinations(t, readFile(t, output))
	if len(destinations) != len(expected) {
		t.Fatalf("CommonMark parsed %d images; source contains %d", len(destinations), len(expected))
	}
	for i, destination := range destinations {
		u, err := url.Parse(destination)
		if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "assets/") || strings.Contains(u.Path, "\\") {
			t.Fatalf("image %d has a non-portable destination: %q", i, destination)
		}
		got := readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(u.Path)))
		if !bytes.Equal(got, expected[i]) {
			t.Fatalf("image %d does not match its own source bytes: %s", i, destination)
		}
		decoded, format, err := image.Decode(bytes.NewReader(got))
		if err != nil {
			t.Fatalf("image %d is not decodable: %s: %v", i, destination, err)
		}
		if decoded.Bounds().Dx() < 1 || decoded.Bounds().Dy() < 1 {
			t.Fatalf("image %d is empty: %s", i, destination)
		}
		extension := "." + format
		if format == "jpeg" {
			extension = ".jpg"
		}
		if filepath.Ext(u.Path) != extension {
			t.Errorf("image %d has extension %q but is %s", i, filepath.Ext(u.Path), format)
		}
		if want := fmt.Sprintf("%x%s", sha256.Sum256(got), extension); filepath.Base(u.Path) != want {
			t.Errorf("image %d filename does not identify its content/format", i)
		}
	}
}

// The source oracle reads ZIP+JSON directly rather than the production parser.
// Comparing in topic order detects image swaps even when every hash is present.
func qaSampleImageSequence(t *testing.T, input string) [][]byte {
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
		Image *struct {
			Src string `json:"src"`
		} `json:"image"`
		Children map[string][]*sourceTopic `json:"children"`
		Notes    json.RawMessage           `json:"notes"`
	}
	var sheets []struct {
		Root *sourceTopic `json:"rootTopic"`
	}
	if err := json.Unmarshal(read("content.json"), &sheets); err != nil {
		t.Fatal(err)
	}
	var images [][]byte
	var walk func(*sourceTopic)
	walk = func(topic *sourceTopic) {
		if topic == nil {
			t.Fatal("nil source topic")
		}
		if topic.Image != nil && topic.Image.Src != "" {
			ref, err := url.PathUnescape(strings.TrimPrefix(strings.TrimPrefix(topic.Image.Src, "xap:"), "/"))
			if err != nil {
				t.Fatal(err)
			}
			images = append(images, read(ref))
		}
		if len(topic.Notes) != 0 && string(topic.Notes) != "null" && string(topic.Notes) != "{}" {
			t.Fatal("sample gained notes; expand independent note-image oracle")
		}
		for kind, children := range topic.Children {
			if kind != "attached" && len(children) != 0 {
				t.Fatalf("sample gained %s topics; expand independent image-order oracle", kind)
			}
		}
		for _, child := range topic.Children["attached"] {
			walk(child)
		}
	}
	for _, sheet := range sheets {
		walk(sheet.Root)
	}
	return images
}

func TestQARealSampleImagesDecodeAfterRelocationAndRepeatedExports(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("..", "..", "assets", "*.xmind"))
	if err != nil || len(inputs) != 3 {
		t.Fatalf("expected three supplied archives: %v %v", inputs, err)
	}
	outputDir := filepath.Join(t.TempDir(), "图表 #100% [portable]", "export tree")
	expected := make(map[string][][]byte)
	total := 0
	for i, input := range inputs {
		name := fmt.Sprintf("%d 图表 [final] #100%%.md", i)
		expected[name] = qaSampleImageSequence(t, input)
		total += len(expected[name])
		output := filepath.Join(outputDir, name)
		if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output}); err != nil {
			t.Fatal(err)
		}
		qaCheckImageBytes(t, output, expected[name])
	}
	if total != 37 {
		t.Fatalf("source image occurrence count changed: %d", total)
	}
	before := snapshot(t, outputDir)
	for repetition := 0; repetition < 3; repetition++ {
		for i, input := range inputs {
			output := filepath.Join(outputDir, fmt.Sprintf("%d 图表 [final] #100%%.md", i))
			if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output, Force: true}); err != nil {
				t.Fatal(err)
			}
		}
		assertSnapshot(t, before, snapshot(t, outputDir))
	}
	moved := filepath.Join(t.TempDir(), "moved elsewhere + 汉字")
	if err := os.Rename(outputDir, moved); err != nil {
		t.Fatal(err)
	}
	assertSnapshot(t, before, snapshot(t, moved))
	for name, images := range expected {
		qaCheckImageBytes(t, filepath.Join(moved, name), images)
	}
	t.Logf("all %d source image occurrences parsed as Markdown images, decoded, matched byte-for-byte, and survived relocation", total)
}

func qaPNG(t *testing.T, seed uint32, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			seed = seed*1664525 + 1013904223
			img.SetRGBA(x, y, color.RGBA{R: byte(seed >> 24), G: byte(seed >> 16), B: byte(seed >> 8), A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestQAImageAliasesNotesAndAttachmentsSharePortableAssets(t *testing.T) {
	imageBytes := qaPNG(t, 1, 3)
	resourceName := "resources/图 [A]#100% (1).PNG"
	ref := "xap:" + url.PathEscape(resourceName)
	attachment := []byte("%PDF-1.7\nattachment bytes preserved\n")
	root := map[string]any{
		"title": "Image aliases",
		"children": map[string]any{"attached": []any{
			map[string]any{"image": map[string]any{"src": ref}, "href": "xap:/resources/Guide.PDF"},
			map[string]any{"title": "Same resource with leading slash", "image": map[string]any{"src": "xap:/" + url.PathEscape(resourceName)}},
			map[string]any{"title": "Repeated identical image under another ZIP name", "image": map[string]any{"src": "xap:resources/copy.jpg"}},
			map[string]any{"title": "Notes only", "notes": map[string]any{"html": map[string]any{"content": map[string]any{"paragraphs": []any{map[string]any{"spans": []any{map[string]any{"spans": []any{map[string]any{"image": ref}}}}}}}}}},
		}},
	}
	input := fixture(t, []any{map[string]any{"rootTopic": root}}, map[string][]byte{resourceName: imageBytes, "resources/copy.jpg": imageBytes, "resources/Guide.PDF": attachment})
	output := filepath.Join(t.TempDir(), "nested exports", "汉字 [test] #100%.md")
	result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Images != 1 {
		t.Fatalf("image aliases not deduplicated: %+v", result)
	}
	qaCheckImageBytes(t, output, [][]byte{imageBytes, imageBytes, imageBytes, imageBytes})
	files, err := os.ReadDir(filepath.Join(filepath.Dir(output), "assets"))
	if err != nil || len(files) != 2 {
		t.Fatalf("want one image plus one attachment, got %v: %v", files, err)
	}
	attachmentPath := fmt.Sprintf("assets/%x.pdf", sha256.Sum256(attachment))
	if !bytes.Contains(readFile(t, output), []byte("[Link]("+attachmentPath+")")) || !bytes.Equal(attachment, readFile(t, filepath.Join(filepath.Dir(output), filepath.FromSlash(attachmentPath)))) {
		t.Fatal("image-only topic lost its attachment or attachment bytes changed")
	}
}

func TestQASharedArchivePathWorksAsBothImageAndAttachmentInEitherOrder(t *testing.T) {
	data := qaPNG(t, 7, 4)
	var destinations [][]string
	for _, attachmentFirst := range []bool{true, false} {
		attachment := map[string]any{"title": "Download the source image", "href": "xap:resources/image.txt"}
		img := map[string]any{"title": "See the image", "image": map[string]any{"src": "xap:resources/image.txt"}}
		children := []any{attachment, img}
		if !attachmentFirst {
			children = []any{img, attachment}
		}
		input := fixture(t, []any{map[string]any{"rootTopic": map[string]any{"title": "Both uses", "children": map[string]any{"attached": children}}}}, map[string][]byte{"resources/image.txt": data})
		output := filepath.Join(t.TempDir(), "result.md")
		if _, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output}); err != nil {
			t.Fatal(err)
		}
		qaCheckImageBytes(t, output, [][]byte{data})
		destinations = append(destinations, qaImageDestinations(t, readFile(t, output)))
	}
	if !reflect.DeepEqual(destinations[0], destinations[1]) {
		t.Errorf("image filename depends on whether its attachment was encountered first: %v", destinations)
	}
}

func TestQAConcurrentMapsShareAssetsWithoutPartialFiles(t *testing.T) {
	data := qaPNG(t, 49, 768)
	input := fixture(t, []any{map[string]any{"rootTopic": map[string]any{"title": "Concurrent shared image", "image": map[string]any{"src": "xap:resources/shared.png"}}}}, map[string][]byte{"resources/shared.png": data})
	dir := t.TempDir()
	const workers = 12
	start := make(chan struct{})
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := exporter.Convert(context.Background(), input, exporter.Options{Output: filepath.Join(dir, fmt.Sprintf("map-%d.md", i))})
			errors <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errors)
	failed := false
	for err := range errors {
		if err != nil {
			t.Errorf("simultaneous independent output failed: %v", err)
			failed = true
		}
	}
	if failed {
		return
	}
	for i := 0; i < workers; i++ {
		qaCheckImageBytes(t, filepath.Join(dir, fmt.Sprintf("map-%d.md", i)), [][]byte{data})
	}
	entries, err := os.ReadDir(filepath.Join(dir, "assets"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("concurrent maps should share one complete image: %v %v", entries, err)
	}
}

func TestQASeparateMapsWithIdenticalResourcePathsDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	data := [][]byte{qaPNG(t, 19, 8), qaPNG(t, 97, 8)}
	inputs := make([]string, len(data))
	for i, imageBytes := range data {
		inputs[i] = fixture(t, []any{map[string]any{"rootTopic": map[string]any{"title": fmt.Sprintf("Map %d", i), "image": map[string]any{"src": "xap:resources/image.png"}}}}, map[string][]byte{"resources/image.png": imageBytes})
		output := filepath.Join(dir, fmt.Sprintf("map-%d.md", i))
		if _, err := exporter.Convert(context.Background(), inputs[i], exporter.Options{Output: output}); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, dir)
	for i := len(data) - 1; i >= 0; i-- {
		output := filepath.Join(dir, fmt.Sprintf("map-%d.md", i))
		if _, err := exporter.Convert(context.Background(), inputs[i], exporter.Options{Output: output, Force: true}); err != nil {
			t.Fatal(err)
		}
		qaCheckImageBytes(t, output, [][]byte{data[i]})
	}
	assertSnapshot(t, before, snapshot(t, dir))
	entries, err := os.ReadDir(filepath.Join(dir, "assets"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("different bytes must remain separate despite identical resource paths: %v %v", entries, err)
	}
}

func TestQALegacyAndModernHTMLNoteImageOccurrencesAreRetained(t *testing.T) {
	first, second := qaPNG(t, 73, 3), qaPNG(t, 27, 3)
	resources := map[string][]byte{"resources/first.png": first, "resources/second.png": second}
	const htmlNotes = `<p>Before <img src="xap:resources/second.png"/> between <img src="xap:/resources/second.png"/> after.</p>`
	modern := fixture(t, []any{map[string]any{"rootTopic": map[string]any{"image": map[string]any{"src": "xap:resources/first.png"}, "notes": map[string]any{"html": map[string]any{"content": htmlNotes}}}}}, resources)
	legacy := zipFixture(t, map[string][]byte{
		"resources/first.png":  first,
		"resources/second.png": second,
		"content.xml":          []byte(`<xmap-content xmlns:xhtml="http://www.w3.org/1999/xhtml"><sheet><topic><xhtml:img src="xap:resources/first.png"/><notes><html>` + htmlNotes + `</html></notes></topic></sheet></xmap-content>`),
	})
	for _, input := range []string{modern, legacy} {
		output := filepath.Join(t.TempDir(), "notes.md")
		result, err := exporter.Convert(context.Background(), input, exporter.Options{Output: output})
		if err != nil {
			t.Fatal(err)
		}
		if result.Images != 2 {
			t.Fatalf("repeated note image should use one asset, in addition to topic image: %+v", result)
		}
		qaCheckImageBytes(t, output, [][]byte{first, second, second})
	}
}

// This context cancels exactly when an asset has been staged. Using an explicit
// boundary avoids timing-dependent sleeps and verifies cleanup after a write.
type qaCancelAtStagedAsset struct {
	context.Context
	cancel context.CancelFunc
	dir    string
	staged bool
}

func (c *qaCancelAtStagedAsset) Err() error {
	staged, _ := filepath.Glob(filepath.Join(c.dir, "assets", ".xmind-md-asset-*.tmp"))
	if len(staged) > 0 {
		c.staged = true
		c.cancel()
	}
	return c.Context.Err()
}

func TestQACancelAfterAssetStagingPreservesPreviousDocument(t *testing.T) {
	data := qaPNG(t, 1, 3)
	input := fixture(t, []any{map[string]any{"rootTopic": map[string]any{"title": "Canceled replacement", "image": map[string]any{"src": "xap:resources/image.png"}}}}, map[string][]byte{"resources/image.png": data})
	dir := t.TempDir()
	output := filepath.Join(dir, "existing.md")
	if err := os.WriteFile(output, []byte("# Previous complete export\n"), 0644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boundary := &qaCancelAtStagedAsset{Context: ctx, cancel: cancel, dir: dir}
	_, err := exporter.Convert(boundary, input, exporter.Options{Output: output, Force: true})
	if !boundary.staged || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation after writing staging asset: staged=%v err=%v", boundary.staged, err)
	}
	assertSnapshot(t, before, snapshot(t, dir))
}
