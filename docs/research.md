# Format research and verification

The converter uses Go's ZIP reader and native JSON/XML decoders. It reads the
archive directly instead of unpacking arbitrary archive paths to disk. Exported
resources retain their exact source bytes, with SHA-256 filenames in `assets/`.
Content-derived names avoid collisions across documents and make repeated exports
stable.

## Primary open-source references

Bubble Tea was resolved and queried through the Context7 CLI. Its indexed
package examples described v1; the [official v2 README](https://github.com/charmbracelet/bubbletea)
and installed v2 source were used to confirm the current module, `tea.View`,
key messages, and asynchronous commands. The UI uses `charm.land/bubbletea/v2`.

Go's [security guidance](https://go.dev/doc/security/best-practices) informed
the vulnerability scan. The initial installed Go 1.26.1 toolchain was affected
by standard-library issues including [XML decoding](https://pkg.go.dev/vuln/GO-2026-6088)
and [os.Root path handling](https://pkg.go.dev/vuln/GO-2026-4970). The minimum
toolchain was raised to the patched Go 1.26.7; the subsequent `govulncheck`
reported no vulnerabilities.

- [XMind's official JSON interfaces](https://github.com/xmindltd/xmind-sdk-js/blob/d35e820a2d09995c69bbb452d4edc42b09a13946/src/common/model.ts)
  document sheets, root topics, image references, notes, relationships, summaries,
  and boundaries.
- [XMind's official viewer loader](https://github.com/xmindltd/xmind-viewer/blob/272b324bd82ea8567739c6617154d8d2556f6b50/src/xmindLoader.ts)
  chooses `content.json` when present and `content.xml` otherwise. Its XML viewer
  implementation is incomplete, so it is a format reference rather than a parser
  to copy wholesale.
- [XMind's topic model](https://github.com/xmindltd/xmind-model/blob/c448b0fcfc8bee51f68124aae87a57e3947c6b44/src/models/topic.ts)
  supports attached, detached, callout, and summary children. Topic hyperlinks can
  refer to embedded attachments (`xap:resources/...`), another topic (`xmind:#ID`),
  a web page, or a local file.
- [XMind's rich-note model](https://github.com/xmindltd/xmind-model/blob/c448b0fcfc8bee51f68124aae87a57e3947c6b44/src/models/notes.ts)
  includes plain text and paragraph/span structures, including image references
  and hyperlinks.
- [XMind's legacy XML constants](https://github.com/xmindltd/xmind-sdk-python/blob/master/xmind/core/const.py)
  identify `content.xml`, the XMind namespace, `attachments/`, `xlink:href`, and
  topic/relationship containers. The archived SDK remains useful for legacy
  format examples.

These references inform compatibility; they do not guarantee that every future
XMind extension has the same schema. Unsupported visual layout and application
features cannot be reproduced exactly in Markdown.

## Supplied sample evidence

The three supplied archives use data structure version 3. Each has one sheet,
modern JSON content, and a 4,309-byte XML compatibility warning. Parsing that XML
instead of the JSON would lose the actual map.

| Archive | Topics | Referenced PNG images | Maximum topic depth |
| --- | ---: | ---: | ---: |
| 10.1 Low unemployment | 104 | 6 | 7 |
| 10.2 Low and stable rate of inflation | 286 | 23 | 6 |
| 10.3 Exploring the relationship between unemployment and inflation | 21 | 8 | 2 |
| Total | 411 | 37 | |

Depth counts edges from the root. The 37 source image byte streams have distinct
SHA-256 hashes and total 4,064,510 bytes. Thirty-four topics have images without
titles; one further topic is empty apart from its ID. These nodes must survive
conversion. The samples use `attributedTitle` spans for styling, alongside
complete plain titles, and only the attached child group.

Integration tests independently enumerate image references from the source ZIPs,
check every exported reference resolves, compare bytes and hashes to the source,
assert topic/image counts, and repeat conversion to detect nondeterminism.
Generated fixtures exercise other sheets, child groups, legacy data, unusual
text, attachments, and failure cases beyond these samples.
