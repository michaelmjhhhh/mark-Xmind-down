package xmind

import "testing"

// The fuzz targets exercise public-format parsers without touching the filesystem.
// The archive layer separately limits source bytes before either parser is called.
func FuzzParseJSON(f *testing.F) {
	for _, seed := range []string{simpleJSON, `[{"rootTopic":{"image":{"src":"xap:resources/a.png"},"children":{"attached":[{"title":"child"}]}}}]`, `[{"rootTopic":{"notes":{"html":{"content":{"paragraphs":[{"spans":[{"text":"hello"}]}]}}}}}]`, `[]`, `null`, `{`, `[{"rootTopic":null}]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		doc, err := parseJSON(data)
		if err == nil {
			if doc == nil || len(doc.Sheets) == 0 {
				t.Fatal("successful parse lacks sheets")
			}
			for _, s := range doc.Sheets {
				if s.Root == nil {
					t.Fatal("successful parse lacks root")
				}
			}
		}
	})
}

func FuzzParseXML(f *testing.F) {
	for _, seed := range []string{`<xmap-content><sheet><topic><title>Root</title></topic></sheet></xmap-content>`, `<xmap-content><sheet><topic><notes><html><p>A &amp; B<br/>next</p></html></notes><children><topics type="attached"><topic/></topics></children></topic></sheet></xmap-content>`, `<xmap-content/>`, `<!DOCTYPE x [<!ENTITY a SYSTEM "file:///tmp/a">]><x>&a;</x>`, `<xmap-content><sheet>`, `</topic>`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		doc, err := parseXML(data)
		if err == nil {
			if doc == nil || len(doc.Sheets) == 0 {
				t.Fatal("successful parse lacks sheets")
			}
			for _, s := range doc.Sheets {
				if s.Root == nil {
					t.Fatal("successful parse lacks root")
				}
			}
		}
	})
}

func FuzzRichHTML(f *testing.F) {
	for _, seed := range []string{`<p>A &amp; B<br/>next</p>`, `<a href="https://example.com">link</a><img src="xap:resources/a.png"/>`, `<script>ignore</script>`, `</script>`, `<`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _, _ = htmlText(data)
	})
}
