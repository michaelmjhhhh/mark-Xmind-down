package xmind

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// This inventory intentionally uses the raw ZIP JSON, not the parser's topic
// type, so an accidentally ignored source field cannot hide behind topic counts.
func TestQASampleContentMatchesRawSource(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "assets", "*.xmind"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("sample archives are optional in source distributions")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			z, err := zip.OpenReader(file)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			var source []map[string]any
			for _, entry := range z.File {
				if entry.Name != "content.json" {
					continue
				}
				r, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(r)
				r.Close()
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &source); err != nil {
					t.Fatal(err)
				}
			}
			if len(source) == 0 {
				t.Fatal("sample did not contain modern JSON sheets")
			}
			a, err := Open(file)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if len(a.Document.Warnings) != 0 {
				t.Errorf("sample has unsupported semantic content: %v", a.Document.Warnings)
			}
			if len(a.Document.Sheets) != len(source) {
				t.Fatalf("sheet count: got %d, want %d", len(a.Document.Sheets), len(source))
			}
			for i, sheet := range source {
				got := a.Document.Sheets[i]
				if got.ID != qaString(sheet["id"]) || got.Title != qaString(sheet["title"]) {
					t.Errorf("sheet %d metadata changed: %+v", i, got)
				}
				qaCompareSourceTopic(t, sheet["rootTopic"].(map[string]any), got.Root, fmt.Sprintf("sheet[%d]", i))
			}
		})
	}
}

func qaCompareSourceTopic(t *testing.T, source map[string]any, got *Topic, path string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: missing topic", path)
	}
	wantTitle := qaString(source["title"])
	if wantTitle == "" {
		if spans, ok := source["attributedTitle"].([]any); ok {
			for _, span := range spans {
				wantTitle += qaString(span.(map[string]any)["text"])
			}
		}
	}
	if got.ID != qaString(source["id"]) || got.Title != wantTitle || got.Href != qaString(source["href"]) {
		t.Errorf("%s: identity/title/hyperlink mismatch: got %q %q %q; source %q %q %q", path, got.ID, got.Title, got.Href, source["id"], wantTitle, source["href"])
	}
	wantImage := ""
	if image, ok := source["image"].(map[string]any); ok {
		wantImage = qaString(image["src"])
	}
	if got.Image != wantImage {
		t.Errorf("%s: image = %q, want %q", path, got.Image, wantImage)
	}
	var wantLabels, wantMarkers []string
	if labels, ok := source["labels"].([]any); ok {
		for _, label := range labels {
			wantLabels = append(wantLabels, qaString(label))
		}
	}
	if markers, ok := source["markers"].([]any); ok {
		for _, marker := range markers {
			wantMarkers = append(wantMarkers, qaString(marker.(map[string]any)["markerId"]))
		}
	}
	if !reflect.DeepEqual(got.Labels, wantLabels) || !reflect.DeepEqual(got.Markers, wantMarkers) {
		t.Errorf("%s: labels/markers changed: got %v %v; want %v %v", path, got.Labels, got.Markers, wantLabels, wantMarkers)
	}
	// These source samples have no notes or annotation fields. Make any future
	// addition explicit so the source inventory must grow with its evidence.
	for _, key := range []string{"notes", "boundaries", "summaries", "extensions", "numbering"} {
		if value, present := source[key]; present && value != nil {
			t.Errorf("%s: sample now includes %s; extend the independent field comparison", path, key)
		}
	}
	children, _ := source["children"].(map[string]any)
	wantGroups := make([]string, 0, len(children))
	for kind, value := range children {
		if len(value.([]any)) > 0 {
			wantGroups = append(wantGroups, kind)
		}
	}
	sort.Strings(wantGroups)
	var gotGroups []string
	for _, group := range got.Children {
		gotGroups = append(gotGroups, group.Kind)
		want, exists := children[group.Kind]
		if !exists {
			t.Errorf("%s: invented group %q", path, group.Kind)
			continue
		}
		wantChildren := want.([]any)
		if len(group.Topics) != len(wantChildren) {
			t.Errorf("%s/%s: child count %d, want %d", path, group.Kind, len(group.Topics), len(wantChildren))
			continue
		}
		for i, child := range group.Topics {
			qaCompareSourceTopic(t, wantChildren[i].(map[string]any), child, fmt.Sprintf("%s/%s[%d]", path, group.Kind, i))
		}
	}
	sort.Strings(gotGroups)
	if strings.Join(gotGroups, "\x00") != strings.Join(wantGroups, "\x00") {
		t.Errorf("%s: child groups %v, want %v", path, gotGroups, wantGroups)
	}
}

func qaString(v any) string {
	s, _ := v.(string)
	return s
}

func TestQANotesPreservePlainAndRichContent(t *testing.T) {
	tests := []struct {
		name, content string
		parse         func([]byte) (*Document, error)
	}{
		{"JSON structured rich notes", `[{"rootTopic":{"id":"r","notes":{"plain":{"content":"Plain text"},"html":{"content":{"paragraphs":[{"spans":[{"text":"Rich-only text "},{"href":"https://example.com/reference","spans":[{"text":"source"}]},{"href":"xap:resources/report.pdf","spans":[{"text":"attachment"}]},{"image":"xap:resources/note.png"}]}]}}}}}]`, parseJSON},
		{"JSON HTML string notes", `[{"rootTopic":{"id":"r","notes":{"plain":{"content":"Plain text"},"html":{"content":"<p>Rich-only text <a href='https://example.com/reference'>source</a><a href='xap:resources/report.pdf'>attachment</a><img src='xap:resources/note.png'/></p>"}}}}]`, parseJSON},
		{"legacy XML rich notes", `<xmap-content><sheet><topic id="r"><notes><plain>Plain text</plain><html><p>Rich-only text <a href="https://example.com/reference">source</a><a href="xap:resources/report.pdf">attachment</a><img src="xap:resources/note.png"/></p></html></notes></topic></sheet></xmap-content>`, parseXML},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := tc.parse([]byte(tc.content))
			if err != nil {
				t.Fatal(err)
			}
			// Allow representations in separate model fields, but require all
			// semantic source information to survive parsing before rendering.
			serialized, err := json.Marshal(doc.Sheets[0].Root)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Plain text", "Rich-only text", "https://example.com/reference", "xap:resources/report.pdf", "xap:resources/note.png"} {
				if !strings.Contains(string(serialized), want) {
					t.Errorf("source note content lost: %q; model %s", want, serialized)
				}
			}
		})
	}
}

func TestQARichNoteTableCellsStaySeparated(t *testing.T) {
	for _, format := range []string{"HTML string", "legacy XML"} {
		t.Run(format, func(t *testing.T) {
			const table = `<table><tr><td>Alpha</td><td>Beta</td></tr><tr><td>Gamma</td><td>Delta</td></tr></table>`
			var notes string
			if format == "HTML string" {
				var err error
				notes, _, err = htmlText(table)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				doc, err := parseXML([]byte(`<xmap-content><sheet><topic><notes><html>` + table + `</html></notes></topic></sheet></xmap-content>`))
				if err != nil {
					t.Fatal(err)
				}
				notes = doc.Sheets[0].Root.Notes
			}
			if !reflect.DeepEqual(strings.Fields(notes), []string{"Alpha", "Beta", "Gamma", "Delta"}) {
				t.Errorf("table cell boundaries lost: %q", notes)
			}
		})
	}
}

func TestQANoteLinksPreservedWithoutDuplicatePlainText(t *testing.T) {
	tests := []struct {
		name, content string
		parse         func([]byte) (*Document, error)
	}{
		{"JSON", `[{"rootTopic":{"notes":{"plain":{"content":"Read source and other"},"html":{"content":{"paragraphs":[{"spans":[{"text":"Read "},{"href":"https://example.com","spans":[{"text":"source"}]},{"text":" and "},{"href":"xmind:#other","spans":[{"text":"other"}]}]}]}}}}}]`, parseJSON},
		{"legacy XML", `<xmap-content><sheet><topic><notes><plain>Read source and other</plain><html><p>Read <a href="https://example.com">source</a> and <a href="xmind:#other">other</a></p></html></notes></topic></sheet></xmap-content>`, parseXML},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := tc.parse([]byte(tc.content))
			if err != nil {
				t.Fatal(err)
			}
			topic := doc.Sheets[0].Root
			if topic.Notes != "Read source and other" {
				t.Errorf("equivalent plain/rich text duplicated: %q", topic.Notes)
			}
			if !reflect.DeepEqual(topic.NoteLinks, []string{"https://example.com", "xmind:#other"}) {
				t.Errorf("hyperlinks lost or reordered: %v", topic.NoteLinks)
			}
		})
	}
}

func TestQAUnsupportedSemanticContentWarns(t *testing.T) {
	tests := []struct {
		name, content string
		parse         func([]byte) (*Document, error)
	}{
		{"JSON", `[{"id":"s","legend":{"markers":{"flag":{"name":"Needs approval"}}},"extensions":[{"provider":"org.xmind.ui.skeleton.structure.style","content":{"centralTopic":"org.xmind.ui.map.clockwise"}}],"rootTopic":{"id":"r","comments":[{"content":"Review comment"}],"numbering":{"prefix":"Step"},"extensions":[{"provider":"org.xmind.ui.taskInfo","content":"Assignee"},{"provider":"org.xmind.ui.audioNotes","resourceRefs":["xap:resources/audio.wav"]}]}}]`, parseJSON},
		{"legacy XML", `<xmap-content><sheet id="s"><legend><marker name="Needs approval"/></legend><extensions><extension provider="org.xmind.ui.skeleton.structure.style"/></extensions><topic id="r"><comments><comment>Review comment</comment></comments><numbering prefix="Step"/><extensions><extension provider="org.xmind.ui.taskInfo">Assignee</extension><extension provider="org.xmind.ui.audioNotes"><resource-ref>xap:resources/audio.wav</resource-ref></extension></extensions></topic></sheet></xmap-content>`, parseXML},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := tc.parse([]byte(tc.content))
			if err != nil {
				t.Fatal(err)
			}
			warnings := strings.Join(doc.Warnings, "\n")
			for _, field := range []string{"comments", "numbering", "legend", "org.xmind.ui.taskInfo", "org.xmind.ui.audioNotes"} {
				if !strings.Contains(warnings, field) {
					t.Errorf("unsupported %s silently discarded; warnings %q", field, warnings)
				}
			}
			if strings.Contains(warnings, "org.xmind.ui.skeleton.structure.style") {
				t.Errorf("presentation-only extension needlessly warned: %q", warnings)
			}
			for i := 0; i < 5; i++ {
				again, err := tc.parse([]byte(tc.content))
				if err != nil || !reflect.DeepEqual(doc.Warnings, again.Warnings) {
					t.Fatalf("warnings not deterministic: %v %v %v", doc.Warnings, again, err)
				}
			}
		})
	}
}

func TestQAHTMLNotesPreserveBlockBoundariesAndCaseInsensitiveImages(t *testing.T) {
	text, images, links, err := htmlContent(`<PRE>Alpha</PRE><BLOCKQUOTE>Beta</BLOCKQUOTE><p>Gamma</p><IMG SRC="xap:resources/upper.png"><A HREF="https://example.com">Source</A><SCRIPT>hidden</SCRIPT>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "Alpha\nBeta\nGamma\n") || strings.Contains(text, "hidden") {
		t.Errorf("HTML blocks merged or script shown: %q", text)
	}
	if !reflect.DeepEqual(images, []string{"xap:resources/upper.png"}) || !reflect.DeepEqual(links, []string{"https://example.com"}) {
		t.Errorf("case-insensitive HTML resources lost: images=%v links=%v", images, links)
	}
}

func TestQARichNotesSeparateInlineTextFromBlockStarts(t *testing.T) {
	cases := []struct{ name, rich, want string }{
		{"mixed blocks", `<span>Before</span><p>Paragraph</p><span>after</span><section>block</section>`, "Before\nParagraph\nafter\nblock"},
		{"inline flow", `<span>Before</span><span>After</span>`, "BeforeAfter"},
		{"table boundaries", `<span>Before</span><table><tr><td>Alpha</td><td>Beta</td></tr></table><span>After</span>`, "Before\nAlpha Beta \n\nAfter"},
		{"empty link", `<a href="">source</a>`, "source"},
	}
	for _, tc := range cases {
		for _, format := range []string{"HTML string", "legacy XML"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				var doc *Document
				var err error
				if format == "HTML string" {
					content, marshalErr := json.Marshal(tc.rich)
					if marshalErr != nil {
						t.Fatal(marshalErr)
					}
					doc, err = parseJSON([]byte(`[{"rootTopic":{"notes":{"html":{"content":` + string(content) + `}}}}]`))
				} else {
					doc, err = parseXML([]byte(`<xmap-content><sheet><topic><notes><html>` + tc.rich + `</html></notes></topic></sheet></xmap-content>`))
				}
				if err != nil {
					t.Fatal(err)
				}
				topic := doc.Sheets[0].Root
				if topic.Notes != tc.want {
					t.Errorf("rich note text = %q, want %q", topic.Notes, tc.want)
				}
				if len(topic.NoteLinks) != 0 || len(doc.Warnings) != 0 {
					t.Errorf("invented hyperlinks or warnings: links=%v warnings=%v", topic.NoteLinks, doc.Warnings)
				}
			})
		}
	}
}
