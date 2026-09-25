# Changelog

Notable user-visible changes are recorded here, with the newest release first.

## [Unreleased]

## [0.2.0] - 2026-09-25

### Added

- A scrollable keyboard file browser with a visible scrollbar, Page Up/Down, and Home/End navigation.
- An output-folder picker inside the TUI, so files and their destination can be chosen without typing paths.
- Browser refresh and a return-to-browser action after export for repeated conversions in one session.
- A maintained changelog, with GitHub release notes generated from each version's entry.

### Changed

- `xmind-md --tui DIRECTORY` opens that directory for browsing and selection.
- The README starts with the interactive workflow; batch commands remain available for automation.

### Fixed

- The macOS Bash installer bootstrap now configures PATH in the current shell reliably.

## [0.1.0] - 2026-09-25

### Added

- XMind-to-Markdown conversion for modern JSON and legacy XML archives, including multiple sheets, topic hierarchies, notes, links, and annotations.
- Embedded images and attachments exported to a linked `assets/` folder, with deterministic names and byte-for-byte preservation.
- A basic Bubble Tea file browser with keyboard selection, batch export, and overwrite controls.
- Command-line file, folder, glob, and recursive exports, with configurable output locations.
- Prebuilt macOS, Windows, and Linux binaries for amd64 and arm64, verified checksums, and installers that configure PATH automatically.
- Fidelity checks for all 411 topics and 37 images in the supplied sample maps, plus cross-platform CI.
- Validation of malformed archives, missing resources, unsafe paths, and output conflicts; warnings for known unsupported content.

[Unreleased]: https://github.com/michaelmjhhhh/mark-Xmind-down/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/michaelmjhhhh/mark-Xmind-down/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/michaelmjhhhh/mark-Xmind-down/releases/tag/v0.1.0
