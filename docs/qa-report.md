# Markdown and image QA

The sample-content audit passes after the fixes below. This checks rendered
Markdown structure and original archive contents independently of the converter,
not just string counts or the converter's own parsed model.

## Supplied samples

| Sample | Topics | Images | Original image bytes | Maximum depth | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| 10.1 Low unemployment | 104 | 6 | 1,161,413 | 7 | PASS |
| 10.2 Low and stable rate of inflation | 286 | 23 | 2,440,181 | 6 | PASS |
| 10.3 Exploring the relationship between unemployment and inflation | 21 | 8 | 462,916 | 2 | PASS |
| Total | 411 | 37 | 4,064,510 | | PASS |

The independent source walk compares every topic title, topic ID, hyperlink,
image source, child group, and sibling position with the parsed model.
Two independent Markdown implementations—Goldmark 1.8.6 and Pandoc 3.8.3 GFM—then
verify every exported topic's visible text, order, depth, parent, and image
association. All single-sheet display names are retained. The intentionally
empty topic remains an explicit placeholder; image-only topics remain images.

Every sample image is fully decoded in tests, compared byte-for-byte with its
particular source occurrence, and checked against its SHA-256 filename.
Browser checks of the Pandoc HTML previews loaded all 37 images at nonzero
intrinsic dimensions, found the expected 411 heading/list topics, and found
no unintended code blocks or horizontal overflow at the tested desktop viewport.

## Defects found and fixed

1. **Single-sheet names were omitted.** A sheet title different from its root
   topic is now included even when the document has only one sheet.
2. **Plain notes could hide distinct rich-note text and hyperlinks.** Differing
   text alternatives now remain labeled; rich links survive independently,
   including embedded attachments and internal topic references.
3. **Indented notes could become code blocks.** Those blocks displayed Markdown
   escaping and HTML entities as literal characters. Note indentation now uses
   nonbreaking spaces, and explicit line breaks preserve the plain-note layout.
4. **Summary target references and unresolved internal links disappeared.**
   Summary annotations now link to their topics. Unresolved references stay
   visible and produce warnings.
5. **Rich HTML note content could merge or miss resources.** Adjacent table
   cells and block elements retain separators; uppercase HTML image/link
   elements and attributes are recognized.
6. **Image filenames depended on traversal order.** A resource attached as a
   download before being shown as an image could keep an incorrect source
   extension. Image type detection now applies consistently in either role.
7. **Concurrent exports could see partially written image files.** Assets now
   write to synced staging files before complete bytes are published. Twelve
   simultaneous maps sharing the same image pass; cancellation removes staging
   files and preserves previously exported Markdown.

Known unsupported semantic extensions now produce explicit warnings rather than
being silently treated as fully exported.

## Regression coverage

- All 411 source topics compared through independent source and rendered ASTs.
- All 37 original sample images fully decoded and compared with their source.
- Three forced repeats and relocation to paths containing Unicode and Markdown
  punctuation; relative image URLs continue to resolve to the same bytes.
- Thirty-three adversarial plain titles at three levels (99 cases): Markdown
  list markers, headings, code fences, HTML, entities, task boxes,
  strikethrough, Unicode, multiline text, URLs, and backslashes.
- Notes/images stay attached to the correct topic; 256 nested topic levels,
  multiple sheets, summary links, and relationship anchors retain structure.
- Rendered external URLs preserve spaces, parentheses, and entity-like queries.
- Modern and legacy note images, encoded resource names, deduplication, shared
  image/download references, missing note attachments, corrupt existing assets,
  symlink protection, staged-write cancellation, and concurrent exports.

## Reproduce

From the repository root:

    go test ./...
    go test -race ./...
    go vet ./...
    python3 scripts/qa_samples.py

The Python command needs Pandoc and regenerates the three Markdown files,
their image directory, HTML previews, and exports/qa/report.json.
The Go test suite requires no Pandoc, browser, or network at test runtime.

The final verification also runs bounded JSON/XML/rich-HTML fuzzing and
govulncheck. The CI workflow executes the tests natively on Linux, macOS, and
Windows and builds amd64/arm64 binaries for all three.

## Scope

No missing textual topic content, broken hierarchy, wrong image association,
changed image bytes, or unresolved local image paths were found in the supplied
maps after these fixes. All samples export without warnings.

This is a content-to-Markdown conversion: canvas layout, fonts, colors, rich
text emphasis, and spatial positioning are intentionally flattened. Rich-note
images are grouped after the note text. Comments, custom legends, task/audio
metadata, and unsupported semantic extensions produce warnings. Arbitrary
future XMind schema changes cannot be certified by this sample corpus.

Image payloads are preserved rather than repaired; sample decode checks do not
promise that a malformed source image will become displayable. Remote image
URLs are not downloaded. Filesystem rename guarantees vary; the hard-link-less
fallback was reviewed but was not exercised on a physical FAT/exFAT volume.
