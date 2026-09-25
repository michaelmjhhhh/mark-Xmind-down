// Package export converts XMind documents to deterministic Markdown and assets.
package export

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michaelmjhhhh/mark-Xmind-down/internal/xmind"
)

type Options struct {
	Output string
	Force  bool
}

type Result struct {
	Output   string
	Topics   int
	Images   int
	Sheets   int
	Warnings []string
}

// Convert validates and renders everything before publishing any output.
// Embedded resources retain their original bytes and use SHA-256 filenames.
func Convert(ctx context.Context, input string, opts Options) (Result, error) {
	result := Result{Output: opts.Output}
	if result.Output == "" {
		result.Output = strings.TrimSuffix(input, filepath.Ext(input)) + ".md"
	}
	if !strings.EqualFold(filepath.Ext(result.Output), ".md") {
		return result, errors.New("output filename must end in .md")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	inputAbs, err := filepath.Abs(input)
	if err != nil {
		return result, err
	}
	outputAbs, err := filepath.Abs(result.Output)
	if err != nil {
		return result, err
	}
	if strings.EqualFold(inputAbs, outputAbs) {
		return result, errors.New("output must differ from input")
	}
	if info, err := os.Lstat(outputAbs); err == nil {
		if !info.Mode().IsRegular() {
			return result, fmt.Errorf("output is not a regular file: %s", result.Output)
		}
		if !opts.Force {
			return result, fmt.Errorf("output already exists: %s (use --force to replace)", result.Output)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return result, err
	}
	a, err := xmind.Open(input)
	if err != nil {
		return result, err
	}
	defer a.Close()
	r := newRenderer(ctx, a)
	markdown, err := r.render()
	if err != nil {
		return result, err
	}
	result.Topics, result.Images, result.Sheets = r.topics, len(r.images), len(a.Document.Sheets)
	result.Warnings = append(result.Warnings, a.Document.Warnings...)
	result.Warnings = append(result.Warnings, r.warnings...)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := os.MkdirAll(filepath.Dir(outputAbs), 0755); err != nil {
		return result, fmt.Errorf("create output directory: %w", err)
	}
	root, err := os.OpenRoot(filepath.Dir(outputAbs))
	if err != nil {
		return result, err
	}
	defer root.Close()
	if err := publishAssets(ctx, root, r.assets); err != nil {
		return result, err
	}
	if err := publishMarkdown(ctx, root, filepath.Base(outputAbs), []byte(markdown), opts.Force); err != nil {
		return result, err
	}
	return result, nil
}

func assetExtension(name string, data []byte, _ bool) string {
	// Sniff images for attachments too: an archive path reused as both an
	// image and a download must have the same filename in either visit order.
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/x-icon":
		return ".ico"
	}

	ext := strings.ToLower(path.Ext(name))
	if len(ext) > 1 && len(ext) <= 12 {
		valid := true
		for _, c := range ext[1:] {
			if c < 'a' || c > 'z' {
				if c < '0' || c > '9' {
					valid = false
				}
			}
		}
		if valid {
			return ext
		}
	}
	return ".bin"
}

func assetName(name string, data []byte, isImage bool) string {
	return fmt.Sprintf("%x%s", sha256.Sum256(data), assetExtension(name, data, isImage))
}

func publishAssets(ctx context.Context, root *os.Root, assets map[string][]byte) error {
	if len(assets) == 0 {
		return nil
	}
	if info, err := root.Lstat("assets"); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("assets output path must be a real directory, not a symlink")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := root.MkdirAll("assets", 0755); err != nil {
		return err
	}
	dir, err := root.OpenRoot("assets")
	if err != nil {
		return err
	}
	defer dir.Close()
	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}
	sort.Strings(names)
	// Preflight all existing assets before writing anything; --force never replaces assets.
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := checkAsset(dir, name, assets[name]); err != nil {
			return err
		}
	}
	for _, name := range names {
		if err := publishAsset(ctx, dir, name, assets[name]); err != nil {
			return err
		}
	}
	return nil
}

// checkAsset accepts only a complete, regular file with the expected bytes.
// It is also used after a concurrent publisher wins the destination name.
func checkAsset(dir *os.Root, name string, expected []byte) (bool, error) {
	info, err := dir.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return true, fmt.Errorf("asset is not a regular file: %s", name)
	}
	if info.Size() != int64(len(expected)) {
		return true, fmt.Errorf("existing asset conflicts with its content hash: %s", name)
	}
	data, err := dir.ReadFile(name)
	if err != nil {
		return true, err
	}
	if !bytes.Equal(data, expected) {
		return true, fmt.Errorf("existing asset conflicts with its content hash: %s", name)
	}
	return true, nil
}

func publishAsset(ctx context.Context, dir *os.Root, name string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if exists, err := checkAsset(dir, name, data); exists || err != nil {
		return err
	}
	// Never write at a final hash filename: another map may already refer to it,
	// and another exporter must never observe a partially written resource.
	temp := ".xmind-md-asset-" + rand.Text() + ".tmp"
	f, err := dir.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Linking publishes complete bytes without replacing an existing file.
	if err := dir.Link(temp, name); err == nil {
		return nil
	}
	if exists, err := checkAsset(dir, name, data); exists || err != nil {
		return err
	}
	// Some portable filesystems (for example FAT/exFAT) do not support hard
	// links. A same-directory rename still publishes only complete bytes.
	// Concurrent exporters use the same bytes for this content-addressed name;
	// an existing destination is checked above rather than blindly replaced.
	if err := dir.Rename(temp, name); err != nil {
		if exists, checkErr := checkAsset(dir, name, data); exists || checkErr != nil {
			return checkErr
		}
		return err
	}
	return nil
}

func publishMarkdown(ctx context.Context, root *os.Root, name string, data []byte, force bool) error {
	temp := ".xmind-md-" + rand.Text() + ".tmp"
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !force {
		// Reserve exclusively so even simultaneous exports cannot overwrite a file.
		reservation, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return fmt.Errorf("create output (use --force if it already exists): %w", err)
		}
		if err := reservation.Close(); err != nil {
			_ = root.Remove(name)
			return err
		}
		if err := root.Rename(temp, name); err != nil {
			_ = root.Remove(name)
			return err
		}
		return nil
	}
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing to replace non-regular output file")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return root.Rename(temp, name)
}
