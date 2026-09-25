package xmind

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// xmlElement keeps ordered mixed content so note paragraphs and inline spans
// retain their text order without relying on a particular namespace prefix.
type xmlElement struct {
	name  string
	attrs map[string]string
	parts []any
}

func parseXML(data []byte) (*Document, error) {
	root, err := readXML(data)
	if err != nil {
		return nil, fmt.Errorf("invalid content.xml: %w", err)
	}
	if root.name != "xmap-content" {
		return nil, fmt.Errorf("unsupported content.xml root %q", root.name)
	}
	d := &Document{Format: "xml"}
	count := 0
	for i, s := range root.children("sheet") {
		nodes := s.children("topic")
		if len(nodes) != 1 {
			return nil, fmt.Errorf("sheet %d must have one root topic", i+1)
		}
		t, err := parseXMLTopic(nodes[0], 1, &count)
		if err != nil {
			return nil, fmt.Errorf("sheet %d: %w", i+1, err)
		}
		sh := Sheet{ID: s.attrs["id"], Title: s.childText("title"), Root: t}
		if rel := s.child("relationships"); rel != nil {
			for _, r := range rel.children("relationship") {
				sh.Relationships = append(sh.Relationships, Relationship{Title: r.childText("title"), From: r.attrs["end1"], To: r.attrs["end2"]})
			}
		}
		d.Sheets = append(d.Sheets, sh)
	}
	if len(d.Sheets) == 0 {
		return nil, errors.New("content.xml has no sheets")
	}
	return d, nil
}

func readXML(data []byte) (*xmlElement, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []*xmlElement
	var root *xmlElement
	elements := 0
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch v := token.(type) {
		case xml.StartElement:
			elements++
			if elements > 1000000 {
				return nil, errors.New("XML exceeds 1000000 elements")
			}
			if len(stack) > MaxDepth*4 {
				return nil, errors.New("XML nesting exceeds safety limit")
			}
			n := &xmlElement{name: v.Name.Local, attrs: make(map[string]string)}
			for _, a := range v.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, errors.New("multiple XML root elements")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.parts = append(p.parts, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.parts = append(p.parts, string(v))
			} else if strings.TrimSpace(string(v)) != "" {
				return nil, errors.New("text outside XML root")
			}
		case xml.Directive:
			return nil, errors.New("XML directives and DTDs are unsupported")
		}
	}
	if root == nil {
		return nil, errors.New("empty XML document")
	}
	return root, nil
}

func parseXMLTopic(raw *xmlElement, depth int, count *int) (*Topic, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("topic depth exceeds %d", MaxDepth)
	}
	*count++
	if *count > MaxTopics {
		return nil, fmt.Errorf("topic count exceeds %d", MaxTopics)
	}
	t := &Topic{ID: raw.attrs["id"], Title: raw.childText("title"), Href: raw.attrs["href"]}
	for _, name := range []string{"img", "image"} {
		if img := raw.child(name); img != nil {
			t.Image = img.attrs["src"]
			if t.Image == "" {
				t.Image = img.attrs["href"]
			}
			break
		}
	}
	if notes := raw.child("notes"); notes != nil {
		if plain := notes.child("plain"); plain != nil {
			t.Notes = plain.text()
		}
		if rich := notes.child("html"); rich != nil {
			if t.Notes == "" {
				t.Notes = strings.TrimSpace(rich.blockText())
			}
			t.NoteImages = rich.images()
		}
	}
	if labels := raw.child("labels"); labels != nil {
		for _, l := range labels.children("label") {
			t.Labels = append(t.Labels, l.text())
		}
	}
	if markers := raw.child("marker-refs"); markers != nil {
		for _, m := range markers.children("marker-ref") {
			t.Markers = append(t.Markers, m.attrs["marker-id"])
		}
	}
	if bounds := raw.child("boundaries"); bounds != nil {
		for _, a := range bounds.children("boundary") {
			t.Boundaries = append(t.Boundaries, Annotation{Title: a.childText("title"), Range: a.attrs["range"], TopicID: a.attrs["topic-id"]})
		}
	}
	if sums := raw.child("summaries"); sums != nil {
		for _, a := range sums.children("summary") {
			t.Summaries = append(t.Summaries, Annotation{Title: a.childText("title"), Range: a.attrs["range"], TopicID: a.attrs["topic-id"]})
		}
	}
	if children := raw.child("children"); children != nil {
		groups := make(map[string][]*xmlElement)
		for _, group := range children.children("topics") {
			kind := group.attrs["type"]
			if kind == "" {
				kind = "attached"
			}
			groups[kind] = append(groups[kind], group.children("topic")...)
		}
		keys := make([]string, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		orderGroups(keys)
		for _, kind := range keys {
			g := Group{Kind: kind}
			for _, child := range groups[kind] {
				c, err := parseXMLTopic(child, depth+1, count)
				if err != nil {
					return nil, err
				}
				g.Topics = append(g.Topics, c)
			}
			if len(g.Topics) > 0 {
				t.Children = append(t.Children, g)
			}
		}
	}
	return t, nil
}

func (n *xmlElement) children(name string) []*xmlElement {
	var out []*xmlElement
	for _, p := range n.parts {
		if c, ok := p.(*xmlElement); ok && c.name == name {
			out = append(out, c)
		}
	}
	return out
}
func (n *xmlElement) child(name string) *xmlElement {
	for _, p := range n.parts {
		if c, ok := p.(*xmlElement); ok && c.name == name {
			return c
		}
	}
	return nil
}
func (n *xmlElement) childText(name string) string {
	if c := n.child(name); c != nil {
		return c.text()
	}
	return ""
}
func (n *xmlElement) text() string {
	var b strings.Builder
	n.writeText(&b)
	return b.String()
}
func (n *xmlElement) writeText(b *strings.Builder) {
	for _, p := range n.parts {
		switch v := p.(type) {
		case string:
			b.WriteString(v)
		case *xmlElement:
			v.writeText(b)
		}
	}
}
func (n *xmlElement) blockText() string {
	var b strings.Builder
	n.writeBlockText(&b)
	return b.String()
}
func (n *xmlElement) writeBlockText(b *strings.Builder) {
	for _, p := range n.parts {
		switch v := p.(type) {
		case string:
			b.WriteString(v)
		case *xmlElement:
			if v.name == "br" {
				b.WriteByte('\n')
				continue
			}
			if v.name == "script" || v.name == "style" {
				continue
			}
			v.writeBlockText(b)
			if v.name == "a" && v.attrs["href"] != "" && v.attrs["href"] != v.text() {
				b.WriteString(" (" + v.attrs["href"] + ")")
			}
			switch v.name {
			case "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteByte('\n')
			}
		}
	}
}

func (n *xmlElement) images() []string {
	var out []string
	for _, p := range n.parts {
		if c, ok := p.(*xmlElement); ok {
			if c.name == "script" || c.name == "style" {
				continue
			}
			if c.name == "img" || c.name == "image" {
				src := c.attrs["src"]
				if src == "" {
					src = c.attrs["href"]
				}
				if src != "" {
					out = append(out, src)
				}
			}
			out = append(out, c.images()...)
		}
	}
	return out
}

// htmlText reads XHTML notes; malformed HTML still preserves its visible text.
// It never evaluates markup, entities, or external resources.
func htmlText(value string) (string, []string, error) {
	dec := xml.NewDecoder(strings.NewReader("<notes>" + value + "</notes>"))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	var b strings.Builder
	var images []string
	suppressed := 0
	depth := 0
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("invalid HTML notes: %w", err)
		}
		switch v := token.(type) {
		case xml.StartElement:
			depth++
			if depth > MaxDepth*4 {
				return "", nil, errors.New("HTML notes nesting exceeds safety limit")
			}
			if v.Name.Local == "script" || v.Name.Local == "style" {
				suppressed++
			}
			if suppressed == 0 && v.Name.Local == "br" {
				b.WriteByte('\n')
			}
			if suppressed == 0 && v.Name.Local == "img" {
				for _, a := range v.Attr {
					if a.Name.Local == "src" {
						images = append(images, a.Value)
					}
				}
			}
			if suppressed == 0 && v.Name.Local == "a" {
				for _, a := range v.Attr {
					if a.Name.Local == "href" {
						b.WriteString("(" + a.Value + ") ")
					}
				}
			}
		case xml.EndElement:
			depth--
			if v.Name.Local == "script" || v.Name.Local == "style" {
				suppressed--
			}
			if suppressed == 0 {
				switch v.Name.Local {
				case "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6":
					b.WriteByte('\n')
				}
			}
		case xml.CharData:
			if suppressed == 0 {
				b.Write(v)
			}
		case xml.Directive:
			return "", nil, errors.New("HTML directives and DTDs are unsupported")
		}
	}
	return strings.TrimSpace(b.String()), images, nil
}
