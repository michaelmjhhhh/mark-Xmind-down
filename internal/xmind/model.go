// Package xmind reads modern and legacy XMind ZIP documents.
package xmind

type Document struct {
	Sheets   []Sheet
	Warnings []string
	Format   string
}

type Sheet struct {
	ID            string
	Title         string
	Root          *Topic
	Relationships []Relationship
}

type Topic struct {
	ID         string
	Title      string
	Notes      string
	NoteImages []string
	NoteLinks  []string
	Href       string
	Image      string
	Labels     []string
	Markers    []string
	Children   []Group
	Boundaries []Annotation
	Summaries  []Annotation
}

type Group struct {
	Kind   string
	Topics []*Topic
}

type Annotation struct {
	Title   string
	Range   string
	TopicID string
}

type Relationship struct {
	Title string
	From  string
	To    string
}
