# mark-Xmind-down

Convert XMind mind maps into portable Markdown, with embedded images copied byte-for-byte into an `assets/` directory beside each Markdown file. A single Go binary provides batch conversion and a [Charmbracelet Bubble Tea](https://github.com/charmbracelet/bubbletea) terminal interface.

## Build and run

Requires **Go 1.26.7 or newer** to build. The compiled executable has no Go, Python, Node.js, XMind, or network requirement at runtime.

```sh
go build -trimpath -o bin/xmind-md ./cmd/xmind-md
./bin/xmind-md --help
./bin/xmind-md assets --output-dir exports
```

On Windows:

```powershell
go build -trimpath -o bin/xmind-md.exe ./cmd/xmind-md
.\bin\xmind-md.exe assets --output-dir exports
```

Install directly from [GitHub](https://github.com/michaelmjhhhh/mark-Xmind-down) into your Go binary directory:

```sh
go install github.com/michaelmjhhhh/mark-Xmind-down/cmd/xmind-md@latest
```

From a local checkout, `go install ./cmd/xmind-md` also works.

```sh
# One file; default destination is beside the input.
xmind-md "My Map.xmind"

# Choose the Markdown filename. Images go to notes/assets/.
xmind-md "My Map.xmind" --output notes/map.md

# Multiple files or a directory; quoted globs also work on Windows.
xmind-md "assets/*.xmind" --output-dir exports
xmind-md maps --recursive --output-dir exports

# Explicitly replace existing Markdown. Images are verified and reused.
xmind-md maps --output-dir exports --force

# Interactive file browser. No arguments also open it in a terminal.
xmind-md --tui --output-dir exports
```

Flags can appear before or after paths. `--output` accepts exactly one input; `--output-dir` supports batches. Batch discovery sorts paths, skips directory symlinks, and rejects output-name collisions before exporting. When two inputs have the same stem, use separate output directories. Use `--` before filenames beginning with a hyphen.

Batch operation works with redirected input/output and needs no terminal. Interactive mode requires a terminal on both stdin and stdout. Exit codes: `0` success, `1` input/conversion failure, `2` usage error, `130` canceled. A failed batch item does not prevent the remaining valid items from being attempted.

In the TUI, use arrow keys or `j`/`k` to move, Enter to open a directory or select a file, Space to toggle selection, `a` to select all files in the current directory, and `e` to export. Backspace goes to the parent directory. `q`/Esc exits; Ctrl+C cancels. When input paths are supplied with `--tui`, Enter starts the prepared batch.

## Output

```text
exports/
  My Map.md
  assets/
    <sha256>.png
    <sha256>.jpg
```

Markdown links use forward-slash relative paths such as `![Image](assets/<sha256>.png)`. Move or share the Markdown together with its `assets/` folder. Asset filenames are derived from the complete SHA-256 content hash, so repeated images are reused and multiple maps can safely share the directory. Unreferenced resources and XMind thumbnails are excluded.

Each sheet gets a level-one heading. Main branches get level-two headings; deeper topics become nested lists without a six-level heading cutoff. Topic and sheet order are preserved. Multiline titles use `<br>`; Markdown syntax and HTML in source text are escaped. An empty topic remains an `Untitled topic` placeholder; image-only topics remain images. The result uses UTF-8, LF line endings, and no timestamps or random output identifiers, so the same input produces identical Markdown and asset names.

Supported content:

- Modern ZIP archives containing `content.json` and legacy XMind ZIP archives containing `content.xml`.
- All sheets; attached, floating, summary, callout, and unknown topic groups.
- Titles, textual notes, embedded topic/note images, labels, marker IDs, web links, embedded attachments, internal topic links, relationship endpoints/titles, and boundary/summary annotations.
- Unicode, deep hierarchies, repeated assets, spaces and URL-encoded resource names.

Markdown intentionally flattens presentation: fonts, colors, positioning, branch shapes, rich text styling, and other canvas layout are not reproduced. Rich notes become plain text with note images retained. Relationships and boundaries become textual annotations. External image URLs remain URLs and produce a warning; the tool never downloads them. Unsupported hyperlink schemes remain visible as text. Internal links that cannot be resolved produce a warning.

Modern JSON takes precedence even when XML is also present. Some modern XMind files contain only a compatibility warning in `content.xml`; corrupt JSON therefore fails explicitly instead of falling back and exporting the warning as your map. Password-protected/encrypted files and non-ZIP formats are unsupported. Future files using the supported schemas can be converted; incompatible future schemas require a parser update.

## Reliability and safety

Conversion parses and resolves all referenced resources before writing output. Missing or empty resources and ZIP checksum/decompression failures stop the export. Image bytes are preserved without decoding or repairing the image format itself. The source archive is never modified. Existing Markdown requires `--force`; existing hashed assets are verified and never overwritten. Output symlinks and an `assets/` symlink are rejected; resource writes use Go's directory-confined `os.Root` APIs.

Markdown is staged and synced before replacement. Filesystem rename guarantees depend on the platform/filesystem; this is not a transactional multi-file operation. A canceled or failed write can leave unused complete assets, but never a successfully reported Markdown file with missing assets. Unused assets are not automatically deleted because another exported map may reference them.

ZIP paths, duplicate entries, symbolic links, encryption flags, checksums, and resource references are validated. Limits bound ordinary hostile/corrupt input: 10,000 entries, 32 MiB content, 64 MiB per resource, 512 MiB advertised expanded archive size, 100,000 topics, and 256 topic levels. ZIP files are read in place; archive paths are never extracted to disk. Unknown visual fields are tolerated, and unknown child groups retain their topics in a deterministic order.

## Development and verification

```sh
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/xmind-md assets --output-dir exports
```

The integration tests open the actual supplied samples, check counts, verify image bytes and links, and compare repeat exports. Generated fixtures exercise legacy XML, Unicode/escaping, multiple sheets, notes, links, duplicate images, malformed archives, path traversal, depth/size limits, cancellation, and overwrite protection. CI runs tests on Linux, macOS, and Windows and compiles binaries for their amd64 and arm64 targets.

| Supplied sample | Topics | Embedded images |
| --- | ---: | ---: |
| 10.1 Low unemployment | 104 | 6 |
| 10.2 Low and stable rate of inflation | 286 | 23 |
| 10.3 Exploring the relationship between unemployment and inflation | 21 | 8 |
| Total | 411 | 37 |

Format decisions and primary open-source references are recorded in [docs/research.md](docs/research.md). Dependencies are pinned in `go.mod`/`go.sum`; use conventional commit messages for changes.
