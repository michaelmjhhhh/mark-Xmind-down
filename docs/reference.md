# Command and format reference

For installation and the common commands, start with the [README](../README.md).

Convert XMind mind maps into portable Markdown, with embedded images copied byte-for-byte into an `assets/` directory beside each Markdown file. A single Go binary provides batch conversion and a [Charmbracelet Bubble Tea](https://github.com/charmbracelet/bubbletea) terminal interface.

## Installation

The [README installers](../README.md#install) download the latest release for macOS, Linux, or Windows (amd64/arm64), verify its SHA-256 checksum, and install for the current user. Go and administrator access are not required.

- macOS/Linux: `~/.local/bin/xmind-md`. The sourced installer updates the current PATH and adds an idempotent block to your shell's startup files. Bash login/interactive files and Zsh's `ZDOTDIR` are respected.
- Windows: `%LOCALAPPDATA%\Programs\xmind-md\xmind-md.exe`. The installer updates the current process and persistent User PATH, preserving other entries.

Re-run the same installer to update. Set `XMIND_MD_VERSION` to a release tag such as `v0.1.0` to install a specific version. Checksum or download failures leave the previous executable intact.

For Fish, this also configures the current shell automatically:

```fish
curl -fsSL https://raw.githubusercontent.com/michaelmjhhhh/mark-Xmind-down/main/scripts/install.sh | sh; and source "$__fish_config_dir/conf.d/xmind-md.fish"
```

For a POSIX shell, download the installer and source it for immediate use, or run it with `sh` and start a new login session; `.profile` is configured automatically. No manual PATH edits are needed.

To uninstall, remove the executable and its marked `xmind-md PATH` startup block (or its directory from Windows User PATH). See [GitHub Releases](https://github.com/michaelmjhhhh/mark-Xmind-down/releases) for direct binary downloads and checksums.

## Build and run

Requires **Go 1.26.7 or newer** to build. The compiled executable has no Go, Python, Node.js, XMind, or network requirement at runtime.

Clone the repository with Git and enter it:

```sh
git clone https://github.com/michaelmjhhhh/mark-Xmind-down.git
cd mark-Xmind-down
```

Build and run on macOS or Linux:

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

For Go developers whose Go binary directory is already on PATH, direct installation also works:

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

In the TUI, use arrow keys or `j`/`k` to move, Enter to open a directory or select a file, Space to toggle selection, and `a` to select all files in the current directory. Press `e` to export selected files; if nothing is selected, it exports the highlighted file. Press `f` to toggle whether existing Markdown can be replaced. Backspace goes to the parent directory. `q`/Esc exits; Ctrl+C cancels. When input paths are supplied with `--tui`, Enter starts the prepared batch.

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

Markdown intentionally flattens presentation: fonts, colors, positioning, branch shapes, rich text styling, and other canvas layout are not reproduced. Rich notes become plain text with note images and hyperlinks retained; embedded note attachments are extracted too. If plain and rich note alternatives contain different text, both are labeled and preserved. Plain-note indentation and line breaks remain visible. Relationships and boundaries become textual annotations; summary annotations link to their summary topics. External image URLs remain URLs and produce a warning; the tool never downloads them. Unsupported hyperlink schemes and unresolved internal links remain visible as text and produce warnings.

Known unsupported semantic content, including comments, custom legends, task/audio metadata, and non-presentation extensions, produces explicit warnings. Inspect these warnings before treating an unfamiliar map as a complete export. The supplied samples contain none of these unsupported fields.

Modern JSON takes precedence even when XML is also present. Some modern XMind files contain only a compatibility warning in `content.xml`; corrupt JSON therefore fails explicitly instead of falling back and exporting the warning as your map. Password-protected/encrypted files and non-ZIP formats are unsupported. Future files using the supported schemas can be converted; incompatible future schemas require a parser update.

## Reliability and safety

Conversion parses and resolves all referenced resources before writing output. Missing or empty resources and ZIP checksum/decompression failures stop the export. Image bytes are preserved without decoding or repairing the image format itself. The source archive is never modified. Existing Markdown requires `--force`; existing hashed assets are verified and reused. Output symlinks and an `assets/` symlink are rejected; resource writes use Go's directory-confined `os.Root` APIs.

Markdown and assets are staged and synced before publication. Assets use atomic no-clobber hard links where supported, with same-directory rename on filesystems without hard links; a simultaneous exporter may replace an identical asset during that fallback. Filesystem rename guarantees depend on the platform/filesystem; this is not a transactional multi-file operation. A canceled or failed write can leave unused complete assets, but never a successfully reported Markdown file with missing assets. Unused assets are not automatically deleted because another exported map may reference them.

ZIP paths, duplicate entries, symbolic links, encryption flags, checksums, and resource references are validated. Limits bound ordinary hostile/corrupt input: 10,000 entries, 32 MiB content, 64 MiB per resource, 512 MiB advertised expanded archive size, 100,000 topics, and 256 topic levels. ZIP files are read in place; archive paths are never extracted to disk. Unknown visual fields are tolerated, and unknown child groups retain their topics in a deterministic order.

## Development and verification

```sh
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/xmind-md assets --output-dir exports
```

The integration tests independently read the actual source samples and parse the exported Markdown with Goldmark's CommonMark/GFM parser. They compare every topic's rendered text, sibling order, and parent; decode every sample image; verify its source bytes and link destination; and repeat and relocate exports. Goldmark is a test dependency only. Generated fixtures cover 99 special-title cases, 256 nested topics, metadata, notes, attachments, malformed archives, path limits, cancellation, and simultaneous exports. CI runs tests on Linux, macOS, and Windows and compiles binaries for their amd64 and arm64 targets.

For a second independent parser, install Pandoc and run:

    python3 scripts/qa_samples.py

This re-exports the sample maps, checks their full hierarchy and each image association using Pandoc's GFM AST, and writes HTML previews plus a JSON report under exports/. See [the detailed QA report](qa-report.md) for findings, fixes, and the scope of the checks.

| Supplied sample | Topics | Embedded images |
| --- | ---: | ---: |
| 10.1 Low unemployment | 104 | 6 |
| 10.2 Low and stable rate of inflation | 286 | 23 |
| 10.3 Exploring the relationship between unemployment and inflation | 21 | 8 |
| Total | 411 | 37 |

Format decisions and primary open-source references are recorded in [research.md](research.md). Dependencies are pinned in `go.mod`/`go.sum`; use conventional commit messages for changes.
