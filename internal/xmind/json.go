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
		notes, images, err := jsonHTMLText(raw.Notes.HTML.Content)
		if err != nil {
			return nil, fmt.Errorf("topic %q notes: %w", raw.ID, err)
		}
		if t.Notes == "" {
			t.Notes = notes
		}
		t.NoteImages = images
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
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return "", nil, nil
	}
	var html string
	if json.Unmarshal(data, &html) == nil {
		return htmlText(html)
	}
	var rich struct {
		Paragraphs []struct {
			Spans []jsonNoteSpan `json:"spans"`
		} `json:"paragraphs"`
	}
	if err := decodeJSON(data, &rich); err != nil {
		return "", nil, err
	}
	if rich.Paragraphs == nil {
		return "", nil, errors.New("unsupported rich note structure: expected paragraphs")
	}
	var images []string
	lines := make([]string, 0, len(rich.Paragraphs))
	for _, p := range rich.Paragraphs {
		var b strings.Builder
		for i := range p.Spans {
			if err := writeNoteSpan(&b, &images, &p.Spans[i], 1); err != nil {
				return "", nil, err
			}
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n"), images, nil
}

// Hyperlink spans contain further text, image, or hyperlink spans. Preserve all
// descendants before appending the URL, while bounding recursive note nesting.
func writeNoteSpan(b *strings.Builder, images *[]string, span *jsonNoteSpan, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("rich note span depth exceeds %d", MaxDepth)
	}
	start := b.Len()
	b.WriteString(span.Text)
	if span.Image != "" {
		*images = append(*images, span.Image)
	}
	for i := range span.Spans {
		if err := writeNoteSpan(b, images, &span.Spans[i], depth+1); err != nil {
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
