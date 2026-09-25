package xmind

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

type jsonSheet struct {
	jsonUnsupported
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Root          *jsonTopic `json:"rootTopic"`
	Relationships []struct {
		Title string `json:"title"`
		From  string `json:"end1Id"`
		To    string `json:"end2Id"`
	} `json:"relationships"`
}
type jsonTopic struct {
	jsonUnsupported
	ID              string `json:"id"`
	Title           string `json:"title"`
	AttributedTitle []struct {
		Text string `json:"text"`
	} `json:"attributedTitle"`
	Notes struct {
		Plain *struct {
			Content string `json:"content"`
		} `json:"plain"`
		HTML *struct {
			Content json.RawMessage `json:"content"`
		} `json:"html"`
	} `json:"notes"`
	Href  string `json:"href"`
	Image *struct {
		Src string `json:"src"`
	} `json:"image"`
	Labels  []string `json:"labels"`
	Markers []struct {
		ID string `json:"markerId"`
	} `json:"markers"`
	Children   map[string][]*jsonTopic `json:"children"`
	Boundaries []jsonAnnotation        `json:"boundaries"`
	Summaries  []jsonAnnotation        `json:"summaries"`
}
type jsonAnnotation struct {
	Title   string `json:"title"`
	Range   string `json:"range"`
	TopicID string `json:"topicId"`
}

// Known non-visual fields are retained long enough to warn explicitly. Unknown
// presentation properties remain compatible without making a lossless promise.
type jsonUnsupported struct {
	Extensions []struct {
		Provider string `json:"provider"`
	} `json:"extensions"`
	Comments   json.RawMessage `json:"comments"`
	Numbering  json.RawMessage `json:"numbering"`
	TaskInfo   json.RawMessage `json:"taskInfo"`
	AudioNotes json.RawMessage `json:"audioNotes"`
	Legend     json.RawMessage `json:"legend"`
}

func parseJSON(data []byte) (*Document, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	var sheets []jsonSheet
	if err := decodeJSON(data, &sheets); err != nil {
		return nil, fmt.Errorf("invalid content.json: %w", err)
	}
	if len(sheets) == 0 {
		return nil, errors.New("content.json has no sheets")
	}
	d := &Document{Format: "json"}
	count := 0
	for i, s := range sheets {
		root, err := parseJSONTopic(s.Root, 1, &count)
		if err != nil {
			return nil, fmt.Errorf("sheet %d: %w", i+1, err)
		}
		sh := Sheet{ID: s.ID, Title: s.Title, Root: root}
		for _, r := range s.Relationships {
			sh.Relationships = append(sh.Relationships, Relationship{Title: r.Title, From: r.From, To: r.To})
		}
		d.Sheets = append(d.Sheets, sh)
		d.Warnings = append(d.Warnings, s.jsonUnsupported.warnings("sheet "+s.ID)...)
		var warnings func(*jsonTopic)
		warnings = func(t *jsonTopic) {
			d.Warnings = append(d.Warnings, t.jsonUnsupported.warnings("topic "+t.ID)...)
			keys := make([]string, 0, len(t.Children))
			for k := range t.Children {
				keys = append(keys, k)
			}
			orderGroups(keys)
			for _, k := range keys {
				for _, child := range t.Children[k] {
					warnings(child)
				}
			}
		}
		warnings(s.Root)
	}
	return d, nil
}

func decodeJSON(data []byte, value any) error {
	if !utf8.Valid(data) {
		return errors.New("JSON text is not valid UTF-8")
	}
	// Bound nesting before the recursive decoder allocates topic structures.
	// Scanning once also avoids quadratic copies of nested RawMessage topics.
	if err := checkJSONNesting(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func parseJSONTopic(raw *jsonTopic, depth int, count *int) (*Topic, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("topic depth exceeds %d", MaxDepth)
	}
	*count++
	if *count > MaxTopics {
		return nil, fmt.Errorf("topic count exceeds %d", MaxTopics)
	}
	if raw == nil {
		return nil, errors.New("missing topic")
	}
	t := &Topic{ID: raw.ID, Title: raw.Title, Href: raw.Href, Labels: raw.Labels}
	if t.Title == "" {
		var title strings.Builder
		for _, s := range raw.AttributedTitle {
			title.WriteString(s.Text)
		}
		t.Title = title.String()
	}
	if raw.Image != nil {
		t.Image = raw.Image.Src
	}
	if raw.Notes.Plain != nil {
		t.Notes = raw.Notes.Plain.Content
	}
	if raw.Notes.HTML != nil {
		notes, images, links, err := jsonHTMLContent(raw.Notes.HTML.Content)
		if err != nil {
			return nil, fmt.Errorf("topic %q notes: %w", raw.ID, err)
		}
		t.Notes = mergeNotes(t.Notes, notes, links)
		t.NoteImages = images
		t.NoteLinks = links
	}
	for _, m := range raw.Markers {
		t.Markers = append(t.Markers, m.ID)
	}
	for _, a := range raw.Boundaries {
		t.Boundaries = append(t.Boundaries, Annotation{Title: a.Title, Range: a.Range, TopicID: a.TopicID})
	}
	for _, a := range raw.Summaries {
		t.Summaries = append(t.Summaries, Annotation{Title: a.Title, Range: a.Range, TopicID: a.TopicID})
	}
	keys := make([]string, 0, len(raw.Children))
	for k := range raw.Children {
		keys = append(keys, k)
	}
	orderGroups(keys)
	for _, k := range keys {
		g := Group{Kind: k}
		for _, child := range raw.Children[k] {
			c, err := parseJSONTopic(child, depth+1, count)
			if err != nil {
				return nil, err
			}
			g.Topics = append(g.Topics, c)
		}
		if len(g.Topics) > 0 {
			t.Children = append(t.Children, g)
		}
	}
	return t, nil
}

func checkJSONNesting(data []byte) error {
	depth := 0
	quoted, escaped := false, false
	for _, c := range data {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '{', '[':
			depth++
			if depth > MaxDepth*4 {
				return errors.New("JSON nesting exceeds safety limit")
			}
		case '}', ']':
			depth--
		}
	}
	return nil
}

func orderGroups(keys []string) {
	ranks := map[string]int{"attached": 0, "detached": 1, "summary": 2, "callout": 3}
	sort.SliceStable(keys, func(i, j int) bool {
		ri, oki := ranks[keys[i]]
		rj, okj := ranks[keys[j]]
		if oki && okj {
			return ri < rj
		}
		if oki != okj {
			return oki
		}
		return keys[i] < keys[j]
	})
}

type jsonNoteSpan struct {
	Text  string         `json:"text"`
	Href  string         `json:"href"`
	Image string         `json:"image"`
	Spans []jsonNoteSpan `json:"spans"`
}

func jsonHTMLText(data json.RawMessage) (string, []string, error) {
	text, images, _, err := jsonHTMLContent(data)
	return text, images, err
}

func jsonHTMLContent(data json.RawMessage) (string, []string, []string, error) {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return "", nil, nil, nil
	}
	var html string
	if json.Unmarshal(data, &html) == nil {
		return htmlContent(html)
	}
	var rich struct {
		Paragraphs []struct {
			Spans []jsonNoteSpan `json:"spans"`
		} `json:"paragraphs"`
	}
	if err := decodeJSON(data, &rich); err != nil {
		return "", nil, nil, err
	}
	if rich.Paragraphs == nil {
		return "", nil, nil, errors.New("unsupported rich note structure: expected paragraphs")
	}
	var images, links []string
	lines := make([]string, 0, len(rich.Paragraphs))
	for _, p := range rich.Paragraphs {
		var b strings.Builder
		for i := range p.Spans {
			if err := writeNoteSpan(&b, &images, &links, &p.Spans[i], 1); err != nil {
				return "", nil, nil, err
			}
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n"), images, links, nil
}

// Hyperlink spans contain further text, image, or hyperlink spans. Preserve all
// descendants before appending the URL, while bounding recursive note nesting.
func writeNoteSpan(b *strings.Builder, images, links *[]string, span *jsonNoteSpan, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("rich note span depth exceeds %d", MaxDepth)
	}
	start := b.Len()
	b.WriteString(span.Text)
	if span.Image != "" {
		*images = append(*images, span.Image)
	}
	if span.Href != "" {
		*links = append(*links, span.Href)
	}
	for i := range span.Spans {
		if err := writeNoteSpan(b, images, links, &span.Spans[i], depth+1); err != nil {
			return err
		}
	}
	if span.Href != "" {
		if b.Len() == start {
			b.WriteString(span.Href)
		} else if b.String()[start:] != span.Href {
			b.WriteString(" (" + span.Href + ")")
		}
	}
	return nil
}

func mergeNotes(plain, rich string, links []string) string {
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	if normalize(plain) == "" {
		return rich
	}
	if normalize(rich) == "" || normalize(plain) == normalize(rich) {
		return plain
	}
	// A plain alternative often has the same visible text but omits hyperlink
	// destinations. The destinations survive independently in NoteLinks.
	visible := rich
	replacements := make([]string, 0, len(links)*4)
	seen := make(map[string]bool, len(links))
	for _, link := range links {
		if !seen[link] {
			seen[link] = true
			replacements = append(replacements, " ("+link+")", "", "("+link+") ", "")
		}
	}
	if len(replacements) > 0 {
		// One pass avoids rescanning a long note once for every hyperlink.
		visible = strings.NewReplacer(replacements...).Replace(visible)
	}
	if normalize(plain) == normalize(visible) {
		return plain
	}
	return "Plain note:\n" + plain + "\n\nRich note:\n" + rich
}

func presentationExtension(provider string) bool {
	switch provider {
	case "org.xmind.ui.skeleton.structure.style", "org.xmind.ui.map.unbalanced":
		return true
	}
	return false
}

func (u jsonUnsupported) warnings(owner string) []string {
	var warnings []string
	for _, extension := range u.Extensions {
		if !presentationExtension(extension.Provider) {
			warnings = append(warnings, fmt.Sprintf("%s: unsupported extension %q; its content and resources are not exported", owner, extension.Provider))
		}
	}
	fields := []struct {
		name string
		data json.RawMessage
	}{{"comments", u.Comments}, {"numbering", u.Numbering}, {"taskInfo", u.TaskInfo}, {"audioNotes", u.AudioNotes}, {"legend", u.Legend}}
	for _, field := range fields {
		data := string(bytes.TrimSpace(field.data))
		if data != "" && data != "null" && data != "{}" && data != "[]" {
			warnings = append(warnings, fmt.Sprintf("%s: unsupported %s content is not exported", owner, field.name))
		}
	}
	return warnings
}
