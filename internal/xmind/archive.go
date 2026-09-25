package xmind

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"unicode/utf8"
)

// Resource limits bound work for malformed or hostile archives.
const (
	MaxEntries      = 10000
	MaxContentBytes = 32 << 20
	MaxAssetBytes   = 64 << 20
	MaxTotalBytes   = 512 << 20
	MaxDepth        = 256
	MaxTopics       = 100000
)

// Archive retains the open ZIP file while assets are exported.
type Archive struct {
	Document *Document
	zip      *zip.ReadCloser
	entries  map[string]*zip.File
}

// Open reads an XMind ZIP archive. A present content.json is authoritative:
// invalid modern content is never silently replaced by legacy XML.
func Open(filename string) (_ *Archive, err error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		if z != nil {
			_ = z.Close()
		}
		return nil, fmt.Errorf("open XMind archive %q (expected an unencrypted ZIP): %w", filename, err)
	}
	a := &Archive{zip: z, entries: make(map[string]*zip.File)}
	defer func() {
		if err != nil {
			_ = z.Close()
		}
	}()
	if len(z.File) > MaxEntries {
		return nil, fmt.Errorf("archive exceeds %d entries", MaxEntries)
	}
	var total uint64
	for _, f := range z.File {
		name, e := safeName(f.Name)
		if e != nil {
			return nil, fmt.Errorf("unsafe archive entry %q: %w", f.Name, e)
		}
		if _, ok := a.entries[name]; ok {
			return nil, fmt.Errorf("duplicate archive entry %q", name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink archive entry %q is unsupported", name)
		}
		if f.Flags&1 != 0 {
			return nil, fmt.Errorf("encrypted archive entry %q is unsupported", name)
		}
		if f.UncompressedSize64 > MaxTotalBytes-total {
			return nil, fmt.Errorf("archive exceeds %d bytes of expanded data", MaxTotalBytes)
		}
		total += f.UncompressedSize64
		a.entries[name] = f
	}
	if f, ok := a.entries["content.json"]; ok {
		var data []byte
		data, err = readEntry(f, MaxContentBytes)
		if err == nil {
			a.Document, err = parseJSON(data)
		}
	} else if f, ok := a.entries["content.xml"]; ok {
		var data []byte
		data, err = readEntry(f, MaxContentBytes)
		if err == nil {
			a.Document, err = parseXML(data)
		}
	} else {
		return nil, errors.New("unsupported XMind archive: missing content.json and content.xml")
	}
	if err != nil {
		return nil, fmt.Errorf("read XMind content: %w", err)
	}
	return a, nil
}

func (a *Archive) Close() error { return a.zip.Close() }

// ReadAsset reads a referenced embedded resource without extracting ZIP paths.
// Both xap:resources/file and xap:/resources/file references are supported.
func (a *Archive) ReadAsset(ref string) ([]byte, string, error) {
	name, err := resourceName(ref)
	if err != nil {
		return nil, "", err
	}
	f, ok := a.entries[name]
	if !ok {
		return nil, "", fmt.Errorf("embedded resource %q is missing", name)
	}
	data, err := readEntry(f, MaxAssetBytes)
	if err != nil {
		return nil, "", fmt.Errorf("read embedded resource %q: %w", name, err)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("embedded resource %q is empty", name)
	}
	return data, name, nil
}

func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if f.FileInfo().IsDir() {
		return nil, fmt.Errorf("%q is a directory", f.Name)
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%q exceeds %d bytes", f.Name, limit)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%q exceeds %d bytes", f.Name, limit)
	}
	return data, nil
}

func safeName(name string) (string, error) {
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", errors.New("invalid portable relative path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." {
			return "", errors.New("dot path component")
		}
	}
	clean := path.Clean(name)
	if clean == "." || strings.HasPrefix(clean, "../") {
		return "", errors.New("invalid relative path")
	}
	return clean, nil
}

func resourceName(ref string) (string, error) {
	if strings.HasPrefix(ref, "xap:") {
		ref = strings.TrimPrefix(ref, "xap:")
		ref = strings.TrimPrefix(ref, "/")
	}
	// URL decoding is deliberately performed before traversal validation.
	decoded, err := url.PathUnescape(ref)
	if err != nil {
		return "", fmt.Errorf("invalid embedded resource reference %q: %w", ref, err)
	}
	name, err := safeName(decoded)
	if err != nil {
		return "", fmt.Errorf("unsafe embedded resource reference %q: %w", ref, err)
	}
	return name, nil
}
