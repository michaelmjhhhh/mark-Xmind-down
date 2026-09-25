package xmind

import (
	"archive/zip"
	"bytes"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fixtureEntry struct {
	name    string
	data    []byte
	size    uint64
	mode    os.FileMode
	flags   uint16
	corrupt bool
}

func writeFixture(t *testing.T, entries ...fixtureEntry) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "test.xmind")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store, Flags: e.flags, UncompressedSize64: uint64(len(e.data)), CompressedSize64: uint64(len(e.data)), CRC32: crc32.ChecksumIEEE(e.data)}
		if e.size > 0 {
			h.UncompressedSize64 = e.size
		}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		if e.corrupt {
			h.CRC32++
		}
		dst, err := w.CreateRaw(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = dst.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}

const simpleJSON = `[{"id":"sheet","rootTopic":{"id":"root","title":"Root"}}]`

func openFixture(t *testing.T, entries ...fixtureEntry) *Archive {
	t.Helper()
	a, err := Open(writeFixture(t, entries...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func TestJSONXMLSemanticEquivalence(t *testing.T) {
	modern := `[{"id":"s","title":"Sheet","relationships":[{"title":"Connect","end1Id":"root","end2Id":"a"}],"rootTopic":{"id":"root","title":"Root 中文","href":"https://example.com","image":{"src":"xap:resources/test.png"},"notes":{"plain":{"content":"Line 1\nLine 2"}},"labels":["label"],"markers":[{"markerId":"priority-1"}],"boundaries":[{"title":"Boundary","range":"(0,1)"}],"summaries":[{"title":"Summary","range":"(0,1)","topicId":"sum"}],"children":{"zz":[{"id":"z","title":"Last"}],"callout":[{"title":"Callout"}],"summary":[{"id":"sum","title":"Sum"}],"detached":[{"title":"Floating"}],"attached":[{"id":"a","attributedTitle":[{"text":"First "},{"text":"child"}]}],"aa":[{"title":"Extension"}]}}},{"id":"s2","title":"Sheet 2","rootTopic":{"image":{"src":"xap:resources/other.png"}}}]`
	legacy := `<?xml version="1.0"?><xmap-content xmlns="urn:xmind:xmap:xmlns:content:2.0" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xhtml="http://www.w3.org/1999/xhtml"><sheet id="s"><title>Sheet</title><topic id="root" xlink:href="https://example.com"><title>Root 中文</title><xhtml:img xhtml:src="xap:resources/test.png"/><notes><plain>Line 1
Line 2</plain></notes><labels><label>label</label></labels><marker-refs><marker-ref marker-id="priority-1"/></marker-refs><boundaries><boundary range="(0,1)"><title>Boundary</title></boundary></boundaries><summaries><summary range="(0,1)" topic-id="sum"><title>Summary</title></summary></summaries><children><topics type="zz"><topic id="z"><title>Last</title></topic></topics><topics type="callout"><topic><title>Callout</title></topic></topics><topics type="summary"><topic id="sum"><title>Sum</title></topic></topics><topics type="detached"><topic><title>Floating</title></topic></topics><topics type="attached"><topic id="a"><title>First child</title></topic></topics><topics type="aa"><topic><title>Extension</title></topic></topics></children></topic><relationships><relationship end1="root" end2="a"><title>Connect</title></relationship></relationships></sheet><sheet id="s2"><title>Sheet 2</title><topic><xhtml:img xlink:href="xap:resources/other.png"/></topic></sheet></xmap-content>`
	j := openFixture(t, fixtureEntry{name: "content.json", data: []byte(modern)})
	x := openFixture(t, fixtureEntry{name: "content.xml", data: []byte(legacy)})
	if !reflect.DeepEqual(j.Document.Sheets, x.Document.Sheets) {
		t.Fatalf("modern and legacy differ:\nJSON %#v\nXML %#v", j.Document.Sheets, x.Document.Sheets)
	}
	var kinds []string
	for _, g := range j.Document.Sheets[0].Root.Children {
		kinds = append(kinds, g.Kind)
	}
	if want := []string{"attached", "detached", "summary", "callout", "aa", "zz"}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("groups=%v", kinds)
	}
	if j.Document.Sheets[1].Root.Title != "" || j.Document.Sheets[1].Root.Image == "" {
		t.Fatal("image-only root lost")
	}
}

func TestModernContentAuthoritative(t *testing.T) {
	file := writeFixture(t, fixtureEntry{name: "content.json", data: []byte(`broken`)}, fixtureEntry{name: "content.xml", data: []byte(`<xmap-content><sheet><topic><title>dummy</title></topic></sheet></xmap-content>`)})
	if _, err := Open(file); err == nil || !strings.Contains(err.Error(), "content.json") {
		t.Fatalf("expected JSON error, got %v", err)
	}
}

func TestReadAsset(t *testing.T) {
	a := openFixture(t, fixtureEntry{name: "content.json", data: []byte(simpleJSON)}, fixtureEntry{name: "resources/image name.png", data: []byte("image data")})
	for _, ref := range []string{"xap:resources/image%20name.png", "xap:/resources/image%20name.png", "resources/image name.png"} {
		data, name, err := a.ReadAsset(ref)
		if err != nil || name != "resources/image name.png" || string(data) != "image data" {
			t.Fatalf("%q: data=%q name=%q err=%v", ref, data, name, err)
		}
	}
	for _, ref := range []string{"xap:resources/missing.png", "xap:../outside", "xap:/%2e%2e/outside", "xap:resources/%2e%2e/outside", "xap://outside", "xap:resources%5cfile", "xap:resources/%00.png", "xap:bad%zz", "https://example.com/a.png", "/absolute"} {
		if _, _, err := a.ReadAsset(ref); err == nil {
			t.Errorf("accepted %q", ref)
		}
	}
}

func TestArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", "resources/../outside", "resources/./image", "C:/absolute", `resources\image`, "resources/\x00image"} {
		t.Run(name, func(t *testing.T) {
			_, err := Open(writeFixture(t, fixtureEntry{name: "content.json", data: []byte(simpleJSON)}, fixtureEntry{name: name, data: []byte("a")}))
			if err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	cases := map[string][]fixtureEntry{
		"duplicate":            {{name: "content.json", data: []byte(simpleJSON)}, {name: "content.json", data: []byte(simpleJSON)}},
		"normalized duplicate": {{name: "resources/a", data: []byte("a")}, {name: "resources//a", data: []byte("a")}},
		"symlink":              {{name: "content.json", data: []byte(simpleJSON)}, {name: "resources/link", mode: os.ModeSymlink | 0777, data: []byte("target")}},
		"encrypted":            {{name: "content.json", data: []byte(simpleJSON), flags: 1}},
		"content limit":        {{name: "content.json", size: MaxContentBytes + 1}},
		"total limit":          {{name: "content.json", data: []byte(simpleJSON)}, {name: "huge", size: MaxTotalBytes}},
		"missing content":      {{name: "metadata.json", data: []byte(`{}`)}},
		"corrupt content":      {{name: "content.json", data: []byte(simpleJSON), corrupt: true}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Open(writeFixture(t, entries...)); err == nil {
				t.Fatal("bad archive accepted")
			}
		})
	}
	entries := make([]fixtureEntry, MaxEntries+1)
	for i := range entries {
		entries[i].name = string(rune(0x1000 + i))
	}
	if _, err := Open(writeFixture(t, entries...)); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("entry limit: %v", err)
	}
	file := filepath.Join(t.TempDir(), "notzip.xmind")
	os.WriteFile(file, []byte("not zip"), 0600)
	if _, err := Open(file); err == nil {
		t.Fatal("nonzip accepted")
	}
}

func TestAssetSizeAndCorruption(t *testing.T) {
	for _, e := range []fixtureEntry{{name: "resources/a", size: MaxAssetBytes + 1}, {name: "resources/a", data: []byte("wrong crc"), corrupt: true}, {name: "resources/a"}, {name: "resources/a/", mode: os.ModeDir | 0755}} {
		a := openFixture(t, fixtureEntry{name: "content.json", data: []byte(simpleJSON)}, e)
		if _, _, err := a.ReadAsset("xap:resources/a"); err == nil {
			t.Fatal("invalid asset accepted")
		}
	}
}

func TestMalformedContent(t *testing.T) {
	if _, err := parseJSON([]byte("[{\"rootTopic\":{\"title\":\"\xff\"}}]")); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	for _, content := range []string{`[]`, `null`, `{}`, `[{"rootTopic":null}]`, `[{"rootTopic":{"children":{"attached":[null]}}}]`, simpleJSON + `{}`, `[{"rootTopic":{"title":4}}]`} {
		if _, err := parseJSON([]byte(content)); err == nil {
			t.Errorf("JSON accepted: %s", content)
		}
	}
	for _, content := range []string{``, `<xmap-content/>`, `<wrong/>`, `<xmap-content><sheet/></xmap-content>`, `<xmap-content><sheet><topic/><topic/></sheet></xmap-content>`, `<xmap-content><sheet><topic></sheet></xmap-content>`, `<!DOCTYPE xmap-content [<!ENTITY test SYSTEM "file:///etc/passwd">]><xmap-content/>`, `<xmap-content/><xmap-content/>`} {
		if _, err := parseXML([]byte(content)); err == nil {
			t.Errorf("XML accepted: %s", content)
		}
	}
}

func TestTopicDepthAndCountLimits(t *testing.T) {
	makeJSON := func(depth int) []byte {
		topic := `{"title":"leaf"}`
		for i := 1; i < depth; i++ {
			topic = `{"children":{"attached":[` + topic + `]}}`
		}
		return []byte(`[{"rootTopic":` + topic + `}]`)
	}
	if _, err := parseJSON(makeJSON(MaxDepth)); err != nil {
		t.Fatalf("maximum legal depth failed: %v", err)
	}
	if _, err := parseJSON(makeJSON(MaxDepth + 1)); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("deep JSON: %v", err)
	}
	makeXML := func(depth int) []byte {
		return []byte(`<xmap-content><sheet>` + strings.Repeat(`<topic><children><topics>`, depth) + strings.Repeat(`</topics></children></topic>`, depth) + `</sheet></xmap-content>`)
	}
	if _, err := parseXML(makeXML(MaxDepth)); err != nil {
		t.Fatalf("maximum legal XML depth failed: %v", err)
	}
	if _, err := parseXML(makeXML(MaxDepth + 1)); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("deep XML: %v", err)
	}
	count := MaxTopics
	if _, err := parseJSONTopic(&jsonTopic{}, 1, &count); err == nil {
		t.Fatal("topic count accepted")
	}
	count = MaxTopics
	if _, err := parseXMLTopic(&xmlElement{}, 1, &count); err == nil {
		t.Fatal("XML topic count accepted")
	}
}

func TestRichNotes(t *testing.T) {
	content := `[{"rootTopic":{"notes":{"html":{"content":{"paragraphs":[{"spans":[{"text":"hello "},{"text":"link","href":"https://example.com"},{"image":"xap:resources/note.png"}]},{"spans":[{"text":"next"}]}]}}}}}]`
	d, err := parseJSON([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	topic := d.Sheets[0].Root
	if topic.Notes != "hello link (https://example.com)\nnext" || !reflect.DeepEqual(topic.NoteImages, []string{"xap:resources/note.png"}) {
		t.Fatalf("notes=%q images=%v", topic.Notes, topic.NoteImages)
	}
	content = `[{"rootTopic":{"notes":{"plain":{"content":"Plain wins"},"html":{"content":{"paragraphs":[{"spans":[{"image":"xap:resources/note.png"}]}]}}}}}]`
	d, err = parseJSON([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if d.Sheets[0].Root.Notes != "Plain wins" || len(d.Sheets[0].Root.NoteImages) != 1 {
		t.Fatal("plain notes lost images")
	}
	notes, images, err := htmlText(`<p>A &amp; B &lt;tag&gt;</p><p><a href="https://example.com">Link</a><br/>next<img src="xap:resources/n.png"/></p><script>ignored</script>`)
	if err != nil || notes != "A & B <tag>\n(https://example.com) Link\nnext" || len(images) != 1 {
		t.Fatalf("HTML notes=%q images=%v error=%v", notes, images, err)
	}
	xmlNotes := `<xmap-content><sheet><topic><notes><html><p>A &amp; B</p><p><a href="https://example.com">Link</a><br/>next<img src="xap:resources/n.png"/></p></html></notes></topic></sheet></xmap-content>`
	d, err = parseXML([]byte(xmlNotes))
	if err != nil {
		t.Fatal(err)
	}
	if d.Sheets[0].Root.Notes != "A & B\nLink (https://example.com)\nnext" || len(d.Sheets[0].Root.NoteImages) != 1 {
		t.Fatalf("legacy notes: %+v", d.Sheets[0].Root)
	}
}

func TestSampleArchiveCounts(t *testing.T) {
	cases := []struct {
		name           string
		topics, images int
	}{
		{"10.1 Low unemployment.xmind", 104, 6},
		{"10.2 Low and stable rate of inflation.xmind", 286, 23},
		{"10.3 Exploring the relationship between unemployment and inflation.xmind", 21, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join("..", "..", "assets", tc.name)
			if _, err := os.Stat(file); os.IsNotExist(err) {
				t.Skip("sample assets are optional in source distributions")
			}
			a, err := Open(file)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			topics, images := 0, 0
			var walk func(*Topic)
			walk = func(n *Topic) {
				topics++
				if n.Image != "" {
					images++
					data, _, err := a.ReadAsset(n.Image)
					if err != nil || !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
						t.Errorf("image %q: %v", n.Image, err)
					}
				}
				for _, g := range n.Children {
					for _, c := range g.Topics {
						walk(c)
					}
				}
			}
			for _, s := range a.Document.Sheets {
				walk(s.Root)
			}
			if topics != tc.topics || images != tc.images {
				t.Fatalf("topics=%d images=%d", topics, images)
			}
		})
	}
}

func TestNestedRichNoteSpans(t *testing.T) {
	const rich = `"html":{"content":{"paragraphs":[{"spans":[{"href":"https://example.com","spans":[{"text":"Hidden label"},{"image":"xap:resources/nested.png"},{"spans":[{"image":"xap:resources/deeper.png"}]}]}]}]}}`
	for _, plain := range []bool{false, true} {
		t.Run(map[bool]string{false: "rich fallback", true: "plain text with rich images"}[plain], func(t *testing.T) {
			notes := rich
			want := "Hidden label (https://example.com)"
			if plain {
				notes = `"plain":{"content":"Plain note"},` + notes
				want = "Plain note"
			}
			doc, err := parseJSON([]byte(`[{"rootTopic":{"title":"Notes root","notes":{` + notes + `}}}]`))
			if err != nil {
				t.Fatal(err)
			}
			topic := doc.Sheets[0].Root
			if topic.Notes != want {
				t.Fatalf("notes=%q, want %q", topic.Notes, want)
			}
			if !reflect.DeepEqual(topic.NoteImages, []string{"xap:resources/nested.png", "xap:resources/deeper.png"}) {
				t.Fatalf("nested note images lost: %v", topic.NoteImages)
			}
		})
	}
}

func TestRichNoteSpanDepthLimit(t *testing.T) {
	for _, depth := range []int{MaxDepth, MaxDepth + 1} {
		span := strings.Repeat(`{"spans":[`, depth-1) + `{"text":"leaf"}` + strings.Repeat(`]}`, depth-1)
		notes, _, err := jsonHTMLText([]byte(`{"paragraphs":[{"spans":[` + span + `]}]}`))
		if depth == MaxDepth {
			if err != nil || notes != "leaf" {
				t.Fatalf("maximum valid note depth: notes=%q err=%v", notes, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "span depth") {
			t.Fatalf("expected note depth error, got %v", err)
		}
	}
}

func TestLegacyUTF8BOM(t *testing.T) {
	data := []byte("\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?><xmap-content><sheet><topic><title>BOM 中文</title></topic></sheet></xmap-content>`)
	archive := openFixture(t, fixtureEntry{name: "content.xml", data: data})
	if archive.Document.Sheets[0].Root.Title != "BOM 中文" {
		t.Fatalf("unexpected BOM archive title: %q", archive.Document.Sheets[0].Root.Title)
	}
}
