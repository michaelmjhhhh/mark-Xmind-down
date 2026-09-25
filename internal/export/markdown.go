package export

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/xmind"
)

type renderer struct {
	ctx      context.Context
	a        *xmind.Archive
	b        strings.Builder
	assets   map[string][]byte
	images   map[string]bool
	refs     map[string]string
	anchors  map[*xmind.Topic]string
	targets  map[string]bool
	byID     map[string]*xmind.Topic
	warnings []string
	topics   int
}

func newRenderer(ctx context.Context, a *xmind.Archive) *renderer {
	return &renderer{ctx: ctx, a: a, assets: map[string][]byte{}, images: map[string]bool{}, refs: map[string]string{}, anchors: map[*xmind.Topic]string{}, targets: map[string]bool{}, byID: map[string]*xmind.Topic{}}
}

func (r *renderer) render() (string, error) {
	var index func(*xmind.Topic)
	index = func(t *xmind.Topic) {
		if t == nil {
			return
		}
		r.topics++
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", r.topics, t.ID)))
		r.anchors[t] = fmt.Sprintf("topic-%x", sum[:8])
		if t.ID != "" {
			if _, exists := r.byID[t.ID]; !exists {
				r.byID[t.ID] = t
			}
		}
		if id, ok := internalID(t.Href); ok {
			r.targets[id] = true
		}
		for _, g := range t.Children {
			for _, child := range g.Topics {
				index(child)
			}
		}
	}
	for _, s := range r.a.Document.Sheets {
		index(s.Root)
		if s.ID != "" && s.Root != nil {
			r.byID[s.ID] = s.Root
		}
		for _, rel := range s.Relationships {
			r.targets[rel.From], r.targets[rel.To] = true, true
		}
	}
	for i, s := range r.a.Document.Sheets {
		if err := r.ctx.Err(); err != nil {
			return "", err
		}
		if i > 0 {
			r.b.WriteString("---\n\n")
		}
		if s.Root == nil {
			return "", fmt.Errorf("sheet %d has no root topic", i+1)
		}
		if len(r.a.Document.Sheets) > 1 && s.Title != "" && s.Title != s.Root.Title {
			r.b.WriteString("Sheet: " + inline(s.Title) + "\n\n")
		}
		if err := r.heading(s.Root, 1); err != nil {
			return "", err
		}
		for _, g := range s.Root.Children {
			if g.Kind == "attached" || g.Kind == "" {
				for _, child := range g.Topics {
					if err := r.heading(child, 2); err != nil {
						return "", err
					}
					if err := r.groups(child.Children, 0); err != nil {
						return "", err
					}
					r.b.WriteString("\n")
				}
			} else {
				r.b.WriteString("## " + groupName(g.Kind) + "\n\n")
				for _, t := range g.Topics {
					if err := r.list(t, 0); err != nil {
						return "", err
					}
				}
				r.b.WriteString("\n")
			}
		}
		if len(s.Relationships) > 0 {
			r.b.WriteString("## Relationships\n\n")
			for _, rel := range s.Relationships {
				r.b.WriteString("- " + r.topicLink(rel.From) + " → " + r.topicLink(rel.To))
				if rel.Title != "" {
					r.b.WriteString(": " + inline(rel.Title))
				}
				r.b.WriteString("\n")
			}
			r.b.WriteString("\n")
		}
	}
	return strings.TrimRight(r.b.String(), "\n") + "\n", nil
}

func groupName(kind string) string {
	switch kind {
	case "detached":
		return "Floating topics"
	case "summary":
		return "Summaries"
	case "callout":
		return "Callouts"
	default:
		return "Topics (" + inline(kind) + ")"
	}
}

func (r *renderer) heading(t *xmind.Topic, level int) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	r.anchor(t, "")
	title, err := r.title(t)
	if err != nil {
		return err
	}
	r.b.WriteString(strings.Repeat("#", level) + " " + title + "\n\n")
	return r.details(t, "")
}

func (r *renderer) list(t *xmind.Topic, depth int) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if r.b.Len() > 128<<20 {
		return fmt.Errorf("rendered Markdown exceeds 128 MiB limit")
	}
	prefix := strings.Repeat("  ", depth)
	title, err := r.title(t)
	if err != nil {
		return err
	}
	r.b.WriteString(prefix + "- ")
	if r.targets[t.ID] {
		r.b.WriteString(`<a id="` + r.anchors[t] + `"></a> `)
	}
	r.b.WriteString(title + "\n")
	if err := r.details(t, prefix+"  "); err != nil {
		return err
	}
	return r.groups(t.Children, depth+1)
}

func (r *renderer) groups(groups []xmind.Group, depth int) error {
	for _, g := range groups {
		childDepth := depth
		if g.Kind != "attached" && g.Kind != "" {
			r.b.WriteString(strings.Repeat("  ", depth) + "- **" + groupName(g.Kind) + "**\n")
			childDepth++
		}
		for _, t := range g.Topics {
			if err := r.list(t, childDepth); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *renderer) anchor(t *xmind.Topic, prefix string) {
	needed := r.targets[t.ID]
	if !needed {
		for id, target := range r.byID {
			if target == t && r.targets[id] {
				needed = true
				break
			}
		}
	}
	if needed {
		r.b.WriteString(prefix + `<a id="` + r.anchors[t] + `"></a>` + "\n\n")
	}
}

func (r *renderer) title(t *xmind.Topic) (string, error) {
	title := inline(t.Title)
	if title == "" {
		if t.Image != "" {
			return r.image(t.Image, "Image")
		}
		title = "Untitled topic"
	}
	if t.Href != "" {
		dest, err := r.link(t.Href)
		if err != nil {
			return "", err
		}
		if dest != "" {
			title = "[" + title + "](" + dest + ")"
		}
	}
	return title, nil
}

func (r *renderer) details(t *xmind.Topic, prefix string) error {
	block := func(s string) { r.b.WriteString("\n" + prefix + s + "\n\n") }
	if t.Image != "" && strings.TrimSpace(t.Title) != "" {
		img, err := r.image(t.Image, t.Title)
		if err != nil {
			return err
		}
		block(img)
	}
	if t.Href != "" && strings.TrimSpace(t.Title) == "" && t.Image != "" {
		dest, err := r.link(t.Href)
		if err != nil {
			return err
		}
		if dest != "" {
			block("[Link](" + dest + ")")
		}
	}
	if len(t.Labels) > 0 {
		labels := make([]string, len(t.Labels))
		for i, l := range t.Labels {
			labels[i] = inline(l)
		}
		block("Labels: " + strings.Join(labels, ", "))
	}
	if len(t.Markers) > 0 {
		markers := make([]string, len(t.Markers))
		for i, m := range t.Markers {
			markers[i] = inline(m)
		}
		block("Markers: " + strings.Join(markers, ", "))
	}
	if notes := strings.TrimSpace(clean(t.Notes)); notes != "" {
		r.b.WriteString("\n")
		for _, line := range strings.Split(notes, "\n") {
			r.b.WriteString(prefix + "> " + escape(line) + "\n")
		}
		r.b.WriteString("\n")
	}
	for _, ref := range t.NoteImages {
		img, err := r.image(ref, "Note image")
		if err != nil {
			return err
		}
		block(img)
	}
	if t.Href != "" {
		u, err := url.Parse(t.Href)
		_, internal := internalID(t.Href)
		if !internal && (err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto" && u.Scheme != "xap")) {
			block("Link: " + inline(t.Href))
		}
	}
	for _, b := range t.Boundaries {
		block(annotation("Boundary", b))
	}
	for _, s := range t.Summaries {
		block(annotation("Summary", s))
	}
	return nil
}

func annotation(kind string, a xmind.Annotation) string {
	text := kind
	if a.Range != "" {
		text += " (" + inline(a.Range) + ")"
	}
	if a.Title != "" {
		text += ": " + inline(a.Title)
	}
	return text
}

func internalID(ref string) (string, bool) {
	if strings.HasPrefix(ref, "xmind:#") {
		id, err := url.PathUnescape(strings.TrimPrefix(ref, "xmind:#"))
		return id, err == nil
	}
	if strings.HasPrefix(ref, "#") {
		return strings.TrimPrefix(ref, "#"), true
	}
	return "", false
}

func (r *renderer) topicLink(id string) string {
	if t, ok := r.byID[id]; ok {
		title := inline(t.Title)
		if title == "" {
			title = "Untitled topic"
		}
		return "[" + title + "](#" + r.anchors[t] + ")"
	}
	r.warnings = append(r.warnings, "unresolved topic reference: "+clean(id))
	return inline(id)
}

func (r *renderer) link(ref string) (string, error) {
	if id, ok := internalID(ref); ok {
		if t, exists := r.byID[id]; exists {
			return "#" + r.anchors[t], nil
		}
		r.warnings = append(r.warnings, "unresolved topic link: "+clean(ref))
		return "", nil
	}
	if strings.HasPrefix(ref, "xap:") {
		return r.embedded(ref, false)
	}
	u, err := url.Parse(ref)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto") {
		return markdownURL(u.String()), nil
	}
	r.warnings = append(r.warnings, "unsupported hyperlink retained as text: "+clean(ref))
	return "", nil
}

func markdownURL(s string) string {
	r := strings.NewReplacer("&", "&amp;", " ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "\"", "%22", "\\", "%5C", "\n", "%0A", "\r", "%0D", "\t", "%09")
	return r.Replace(s)
}

func (r *renderer) image(ref, alt string) (string, error) {
	var dest string
	u, err := url.Parse(ref)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		dest = markdownURL(u.String())
		r.warnings = append(r.warnings, "external image left as URL (not downloaded): "+clean(ref))
	} else {
		dest, err = r.embedded(ref, true)
		if err != nil {
			return "", err
		}
	}
	return "![" + inline(alt) + "](" + dest + ")", nil
}

func (r *renderer) embedded(ref string, isImage bool) (string, error) {
	if dest, ok := r.refs[ref]; ok {
		if isImage {
			r.images[dest] = true
		}
		return dest, nil
	}
	if err := r.ctx.Err(); err != nil {
		return "", err
	}
	data, name, err := r.a.ReadAsset(ref)
	if err != nil {
		return "", fmt.Errorf("read embedded resource %q: %w", ref, err)
	}
	name = assetName(name, data, isImage)
	r.assets[name] = data
	dest := "assets/" + name
	r.refs[ref] = dest
	if isImage {
		r.images[dest] = true
	}
	return dest, nil
}

func clean(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, s)
}

func escape(s string) string {
	s = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "#", "\\#", "|", "\\|", "~", "\\~").Replace(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// Keep list, thematic-break and setext markers literal in plain text.
		start := len(line) - len(strings.TrimLeft(line, " "))
		if start == len(line) {
			continue
		}
		switch line[start] {
		case '-', '+', '=':
			lines[i] = line[:start] + "\\" + line[start:]
		default:
			end := start
			for end < len(line) && line[end] >= '0' && line[end] <= '9' {
				end++
			}
			if end > start && end < len(line) && (line[end] == '.' || line[end] == ')') && (end+1 == len(line) || line[end+1] == ' ') {
				lines[i] = line[:end] + "\\" + line[end:]
			}
		}
	}
	return strings.Join(lines, "\n")
}

func inline(s string) string {
	return strings.ReplaceAll(escape(strings.TrimSpace(clean(s))), "\n", "<br>")
}
