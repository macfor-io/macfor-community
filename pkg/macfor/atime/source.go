package atime

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
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
	if before, ok := fstat(f); ok {
		o.t.opened()
		return &observedFile{File: f, src: o, path: path, before: before}, nil
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
		if after, ok := fstat(d); ok {
			o.observe(OperationReadDir, path, before, after)
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
	before, ok := fstat(f)
	if !ok {
		_ = f.Close()
		return nil, Stat{}, false
	}
	return f, before, true
}

// Glob expands pattern, observing every directory the expansion lists.
//
// For a live source it follows filepath.Glob's algorithm exactly (results,
// ordering, errors and case-sensitive matching), listing each directory on
// an open descriptor that is stat-ed before and after the listing. For any
// other source it follows directory.Source.Glob: a component-at-a-time
// expansion over the observed ReadDir, with literal components checked by
// Stat.
func (o *observedSource) Glob(pattern string) ([]string, error) {
	if o.src.Type() == macfor.SourceTypeLive {
		matches, err := o.hostGlob(o.src.ResolvePath(pattern), 0)
		if err != nil {
			return nil, macfor.WrapIOError("glob", pattern, err)
		}
		return matches, nil
	}
	return o.sourceGlob(pattern)
}

// maxGlobDepth bounds hostGlob's recursion, as filepath.Glob does.
const maxGlobDepth = 10000

// hostGlob is filepath.Glob over host paths, with each directory listing
// observed. It mirrors the standard library's globWithLimit.
func (o *observedSource) hostGlob(pattern string, depth int) ([]string, error) {
	if depth == maxGlobDepth {
		return nil, filepath.ErrBadPattern
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, err
	}
	if !hasGlobMeta(pattern) {
		// As filepath.Glob: a missing path is no match, not an error.
		if _, err := os.Lstat(pattern); err != nil {
			return nil, nil //nolint:nilerr // Matches filepath.Glob, which ignores I/O errors.
		}
		return []string{pattern}, nil
	}

	dir, file := filepath.Split(pattern)
	dir = cleanGlobPath(dir)
	if !hasGlobMeta(dir) {
		return o.hostGlobDir(dir, file, nil)
	}
	if dir == pattern {
		return nil, filepath.ErrBadPattern
	}

	dirs, err := o.hostGlob(dir, depth+1)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, d := range dirs {
		if matches, err = o.hostGlobDir(d, file, matches); err != nil {
			return matches, err
		}
	}
	return matches, nil
}

// cleanGlobPath prepares a directory for globbing, as filepath.Glob does.
func cleanGlobPath(path string) string {
	switch path {
	case "":
		return "."
	case string(filepath.Separator):
		return path
	default:
		return path[:len(path)-1]
	}
}

// hostGlobDir appends to matches the names in dir matching pattern, as
// filepath.Glob's inner glob does, observing the directory listing.
func (o *observedSource) hostGlobDir(dir, pattern string, matches []string) ([]string, error) {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return matches, nil
	}
	d, err := os.Open(dir) //nolint:gosec // G304: live source paths are host paths, read for collection.
	if err != nil {
		return matches, nil
	}
	defer func() { _ = d.Close() }()

	before, observed := fstat(d)
	names, _ := d.Readdirnames(-1)
	if observed {
		if after, ok := fstat(d); ok {
			o.observe(OperationReadDir, dir, before, after)
		}
	}

	slices.Sort(names)
	for _, n := range names {
		matched, err := filepath.Match(pattern, n)
		if err != nil {
			return matches, err
		}
		if matched {
			matches = append(matches, filepath.Join(dir, n))
		}
	}
	return matches, nil
}

// fstat snapshots the open file f.
func fstat(f *os.File) (Stat, bool) {
	fi, err := f.Stat()
	if err != nil {
		return Stat{}, false
	}
	return StatOf(fi)
}

// sourceGlob mirrors directory.Source.Glob over the observed ReadDir.
func (o *observedSource) sourceGlob(pattern string) ([]string, error) {
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, macfor.WrapIOError("glob", pattern, err)
	}

	cleaned := filepath.ToSlash(filepath.Clean("/" + strings.TrimPrefix(filepath.ToSlash(pattern), "/")))
	matches := []string{"/"}
	for _, part := range strings.Split(strings.TrimPrefix(cleaned, "/"), "/") {
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

	if matches == nil || (len(matches) == 1 && matches[0] == "/") {
		return []string{}, nil
	}
	return matches, nil
}

// hasGlobMeta reports whether a path or path component contains glob syntax.
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
// it lists. Both the live and the directory walk list a directory right
// after fn returns nil for it and before the next call to fn. So the
// directory is stat-ed when fn returns, after any reads fn made itself, and
// again at the next call to fn (or when the walk ends), and the two are
// compared if the path still names the same inode. A directory fn skips is
// never listed and is not counted.
func (o *observedSource) Walk(root string, fn macfor.WalkFunc) error {
	var pendingPath string
	var pendingBefore Stat
	settle := func() {
		if pendingPath == "" {
			return
		}
		if fi, err := o.src.Stat(pendingPath); err == nil {
			if after, ok := StatOf(fi); ok {
				o.observe(OperationReadDir, pendingPath, pendingBefore, after)
			}
		}
		pendingPath = ""
	}

	err := o.src.Walk(root, func(path string, info macfor.FileInfo, err error) error {
		settle()
		ret := fn(path, info, err)
		if ret == nil && err == nil && info != nil && info.IsDir() {
			if fi, serr := o.src.Stat(path); serr == nil {
				if before, ok := StatOf(fi); ok {
					pendingPath, pendingBefore = path, before
				}
			}
		}
		return ret
	})
	settle()
	return err
}

// observe records one compared read with the tracker. When the access time
// changed on a live source, the event's resolved path follows symlinks to
// the file actually read.
func (o *observedSource) observe(op Operation, path string, before, after Stat) {
	resolved := ""
	if changed, _ := compare(before, after); changed && sameInode(before, after) {
		resolved = o.resolve(path)
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

// resolve returns the path the source read for path, or "" when it is path
// itself. A live source expands ~ and follows symlinks; other sources only
// expand ~, since their symlinks resolve inside the evidence tree.
func (o *observedSource) resolve(path string) string {
	resolved := o.src.ResolvePath(path)
	if o.src.Type() == macfor.SourceTypeLive {
		if target, err := filepath.EvalSymlinks(resolved); err == nil {
			resolved = target
		}
	}
	if resolved == path {
		return ""
	}
	return resolved
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
		if after, ok := fstat(f.File); ok {
			f.src.observe(OperationRead, f.path, f.before, after)
		}
		f.src.t.closed()
	})
	return f.File.Close()
}
