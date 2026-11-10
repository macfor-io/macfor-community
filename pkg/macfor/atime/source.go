package atime

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// readlinker is implemented by sources that can read a symlink target
// without following it. An observed source implements it exactly when the
// source it wraps does, since plugins test for it.
type readlinker interface {
	Readlink(path string) (string, error)
}

// Source returns a view of src whose reads are observed by t and attributed
// to pluginID in phase. File reads (Open, ReadPlist) take an fstat on the
// open descriptor before the first read and again on Close; directory
// listings (ReadDir, Glob, Walk) stat the directory before and after. Stat
// and the source's metadata methods pass straight through, since they never
// change an access time.
func (t *Tracker) Source(src macfor.Source, pluginID string, phase Phase) macfor.Source {
	o := &observedSource{src: src, t: t, plugin: pluginID, phase: phase}
	if rl, ok := src.(readlinker); ok {
		return &observedReadlinkSource{observedSource: o, rl: rl}
	}
	return o
}

// observedSource wraps a macfor.Source and observes its reads.
type observedSource struct {
	src    macfor.Source
	t      *Tracker
	plugin string
	phase  Phase
}

// observedReadlinkSource is an observedSource over a source with Readlink.
type observedReadlinkSource struct {
	*observedSource
	rl readlinker
}

// Readlink forwards to the wrapped source; reading a link target does not
// change an access time.
func (o *observedReadlinkSource) Readlink(path string) (string, error) {
	return o.rl.Readlink(path)
}

// Type forwards to the wrapped source.
func (o *observedSource) Type() macfor.SourceType { return o.src.Type() }

// OSVersion forwards to the wrapped source.
func (o *observedSource) OSVersion() (string, error) { return o.src.OSVersion() }

// Hostname forwards to the wrapped source.
func (o *observedSource) Hostname() (string, error) { return o.src.Hostname() }

// SystemInfo forwards to the wrapped source.
func (o *observedSource) SystemInfo() (*macfor.SystemInfo, error) { return o.src.SystemInfo() }

// Users forwards to the wrapped source.
func (o *observedSource) Users() ([]macfor.User, error) { return o.src.Users() }

// ResolvePath forwards to the wrapped source.
func (o *observedSource) ResolvePath(path string) string { return o.src.ResolvePath(path) }

// Stat forwards to the wrapped source. Stat does not read the file, so it
// does not change the access time.
func (o *observedSource) Stat(path string) (macfor.FileInfo, error) { return o.src.Stat(path) }

// Open opens path through the wrapped source and observes the read. When the
// source returns something other than an *os.File, the read is passed
// through unobserved.
func (o *observedSource) Open(path string) (io.ReadCloser, error) {
	rc, err := o.src.Open(path)
	if err != nil || rc == nil {
		return rc, err
	}
	f, ok := rc.(*os.File)
	if !ok {
		return rc, nil
	}
	if fi, serr := f.Stat(); serr == nil {
		if before, ok := StatOf(fi); ok {
			o.t.opened()
			return &observedFile{File: f, src: o, path: path, before: before}, nil
		}
	}
	return rc, nil
}

// ReadPlist reads and decodes a property list through the observed Open.
func (o *observedSource) ReadPlist(path string, v any) error {
	f, err := o.Open(path)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(f)
	_ = f.Close()
	resolved := o.src.ResolvePath(path)
	if err != nil {
		return macfor.WrapIOError("read", resolved, err)
	}
	if err := plist.NewDecoder(bytes.NewReader(data)).Decode(v); err != nil {
		return macfor.WrapParseError(resolved, 0, 0, "plist decode failed", err)
	}
	return nil
}

// ReadDir lists path through the wrapped source and observes the listing.
// The directory is opened first, without reading it, so its before- and
// after-stats come from the same descriptor.
func (o *observedSource) ReadDir(path string) ([]macfor.DirEntry, error) {
	d, before, observed := o.openForStat(path)
	entries, err := o.src.ReadDir(path)
	if observed {
		if fi, serr := d.Stat(); serr == nil {
			if after, ok := StatOf(fi); ok {
				o.observe(OperationReadDir, path, before, after)
			}
		}
		_ = d.Close()
	}
	return entries, err
}

// openForStat opens path through the wrapped source and stats the
// descriptor. It reports false, with nothing left open, when that fails.
func (o *observedSource) openForStat(path string) (*os.File, Stat, bool) {
	rc, err := o.src.Open(path)
	if err != nil || rc == nil {
		return nil, Stat{}, false
	}
	f, ok := rc.(*os.File)
	if !ok {
		_ = rc.Close()
		return nil, Stat{}, false
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, Stat{}, false
	}
	before, ok := StatOf(fi)
	if !ok {
		_ = f.Close()
		return nil, Stat{}, false
	}
	return f, before, true
}

// Glob expands an absolute pattern one path component at a time, listing
// directories through the observed ReadDir, so every directory the
// expansion reads is observed. Literal components are checked with Stat,
// which reads no directory. A relative pattern is passed to the wrapped
// source unobserved.
func (o *observedSource) Glob(pattern string) ([]string, error) {
	if o.src.Type() == macfor.SourceTypeLive {
		pattern = o.src.ResolvePath(pattern)
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, macfor.WrapIOError("glob", pattern, err)
	}
	if !strings.HasPrefix(pattern, "/") {
		return o.src.Glob(pattern)
	}

	matches := []string{"/"}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(pattern), "/"), "/") {
		if part == "" {
			continue
		}
		var next []string
		for _, dir := range matches {
			if !hasGlobMeta(part) {
				candidate := joinPath(dir, part)
				if _, err := o.src.Stat(candidate); err == nil {
					next = append(next, candidate)
				}
				continue
			}
			entries, err := o.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if ok, _ := filepath.Match(part, e.Name()); ok {
					next = append(next, joinPath(dir, e.Name()))
				}
			}
		}
		matches = next
	}
	if len(matches) == 1 && matches[0] == "/" {
		return nil, nil
	}
	return matches, nil
}

// hasGlobMeta reports whether a path component contains glob syntax.
func hasGlobMeta(part string) bool {
	return strings.ContainsAny(part, `*?[\`)
}

// joinPath appends name to the absolute directory dir.
func joinPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// Walk walks the tree through the wrapped source and observes each directory
// it lists. A walk reports a directory to fn, with a stat taken before the
// directory is read, and only then lists it; the after-stats are taken once
// the walk returns, and compared only where the path still names the same
// inode.
func (o *observedSource) Walk(root string, fn macfor.WalkFunc) error {
	type dirSnap struct {
		path   string
		before Stat
	}
	var dirs []dirSnap
	err := o.src.Walk(root, func(path string, info macfor.FileInfo, err error) error {
		if err == nil && info != nil && info.IsDir() {
			if before, ok := StatOf(info); ok {
				dirs = append(dirs, dirSnap{path: path, before: before})
			}
		}
		return fn(path, info, err)
	})
	for _, d := range dirs {
		fi, serr := o.src.Stat(d.path)
		if serr != nil {
			continue
		}
		if after, ok := StatOf(fi); ok {
			o.observe(OperationReadDir, d.path, d.before, after)
		}
	}
	return err
}

// observe records one compared read with the tracker.
func (o *observedSource) observe(op Operation, path string, before, after Stat) {
	resolved := o.src.ResolvePath(path)
	if resolved == path {
		resolved = ""
	}
	o.t.record(Event{
		PluginID:     o.plugin,
		Phase:        o.phase,
		Operation:    op,
		Path:         path,
		ResolvedPath: resolved,
		Before:       before,
		After:        after,
	})
}

// observedFile is a file opened through an observed source. It behaves as
// the *os.File it embeds (Read, ReadAt, Seek, Stat, ...) and compares the
// access time on Close.
type observedFile struct {
	*os.File
	src    *observedSource
	path   string
	before Stat
	once   sync.Once
}

// Close takes the after-stat on the still-open descriptor, records the
// comparison, and closes the file. Only the first Close compares.
func (f *observedFile) Close() error {
	f.once.Do(func() {
		if fi, err := f.Stat(); err == nil {
			if after, ok := StatOf(fi); ok {
				f.src.observe(OperationRead, f.path, f.before, after)
			}
		}
		f.src.t.closed()
	})
	return f.File.Close()
}
