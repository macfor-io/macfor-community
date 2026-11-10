package atime_test

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/atime"
	"github.com/macfor-io/macfor-community/sources/directory"
	"github.com/macfor-io/macfor-community/sources/live"
)

// oldAtime is the access time staged on fixtures, well before their mtime,
// so that even lazy access-time updates apply on the next read.
var oldAtime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// stageOldAtime sets path's access time to oldAtime, keeping its mtime.
func stageOldAtime(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if err := os.Chtimes(path, oldAtime, fi.ModTime()); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// atimeOf returns path's current access time.
func atimeOf(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	st, ok := atime.StatOf(fi)
	if !ok {
		t.Skip("platform stat unavailable")
	}
	return st.Atime
}

// requireAtimeUpdates skips the test when reads on the temp volume do not
// update access times (a noatime or read-only mount).
func requireAtimeUpdates(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, ".probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	stageOldAtime(t, probe)
	if _, err := os.ReadFile(probe); err != nil {
		t.Fatalf("read probe: %v", err)
	}
	if atimeOf(t, probe).Equal(oldAtime) {
		t.Skip("temp volume does not update access times on read")
	}
	_ = os.Remove(probe)
}

// fixture creates dir/name with content and an old access time.
func fixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	stageOldAtime(t, p)
	return p
}

func newLive(t *testing.T) macfor.Source {
	t.Helper()
	src, err := live.New()
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	return src
}

// collectEvents sets a sink on tr that appends to the returned slice.
func collectEvents(tr *atime.Tracker) *[]atime.Event {
	var events []atime.Event
	tr.SetSink(func(e atime.Event) { events = append(events, e) })
	return &events
}

func TestOpenReadRecordsAccessTimeChange(t *testing.T) {
	dir := t.TempDir()
	requireAtimeUpdates(t, dir)
	path := fixture(t, dir, "History.db", "evidence")

	tr := atime.NewTracker()
	events := collectEvents(tr)
	src := tr.Source(newLive(t), "browser.safari", atime.PhaseCollect)

	rc, err := src.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = rc.Close() // A second Close must not record again.

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(*events), *events)
	}
	e := (*events)[0]
	if e.PluginID != "browser.safari" || e.Phase != atime.PhaseCollect || e.Operation != atime.OperationRead {
		t.Errorf("event context = %q/%q/%q", e.PluginID, e.Phase, e.Operation)
	}
	if e.Path != path || e.ResolvedPath != "" {
		t.Errorf("Path = %q, ResolvedPath = %q", e.Path, e.ResolvedPath)
	}
	if e.Attribution != atime.AttributionLikelyCollector {
		t.Errorf("Attribution = %q, want likely_collector", e.Attribution)
	}
	if !e.Before.Atime.Equal(oldAtime) {
		t.Errorf("Before.Atime = %v, want %v", e.Before.Atime, oldAtime)
	}
	if !e.After.Atime.After(oldAtime) {
		t.Errorf("After.Atime = %v, want later than %v", e.After.Atime, oldAtime)
	}
	if e.Before.Ino == 0 || e.Before.Ino != e.After.Ino || e.Before.Size != 8 || e.Before.Mode == "" {
		t.Errorf("stat fields not captured: before %+v after %+v", e.Before, e.After)
	}
	if e.ObservedAt.IsZero() {
		t.Error("ObservedAt not set")
	}

	s := tr.Summary()
	if s.ObservedReads != 1 || s.Modified != 1 || s.LikelyCollector != 1 || s.ConcurrentActivity != 0 || s.Unclosed != 0 {
		t.Errorf("summary = %+v", s)
	}
	if len(s.Inodes) != 1 || !s.Inodes[0].FirstBefore.Atime.Equal(oldAtime) || s.Inodes[0].Changes != 1 {
		t.Errorf("summary inodes = %+v", s.Inodes)
	}
	if len(s.Unobserved) == 0 {
		t.Error("summary should state the unobserved reads")
	}
}

func TestOpenWithoutReadAndStatRecordNothing(t *testing.T) {
	dir := t.TempDir()
	requireAtimeUpdates(t, dir)
	path := fixture(t, dir, "f.txt", "data")

	tr := atime.NewTracker()
	events := collectEvents(tr)
	src := tr.Source(newLive(t), "p", atime.PhaseCollect)

	if _, err := src.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
	rc, err := src.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = rc.Close()

	if len(*events) != 0 {
		t.Errorf("got %d events, want none: %+v", len(*events), *events)
	}
	if got := atimeOf(t, path); !got.Equal(oldAtime) {
		t.Errorf("atime changed by open/stat alone: %v", got)
	}
	if s := tr.Summary(); s.ObservedReads != 1 || s.Modified != 0 {
		t.Errorf("summary = %+v, want 1 observed, 0 modified", s)
	}
}

func TestConcurrentWriteIsAttributed(t *testing.T) {
	dir := t.TempDir()
	requireAtimeUpdates(t, dir)
	path := fixture(t, dir, "chat.db", "before")

	tr := atime.NewTracker()
	events := collectEvents(tr)
	src := tr.Source(newLive(t), "p", atime.PhaseCollect)

	rc, err := src.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Another process appends while the collector holds the file open.
	w, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := w.WriteString(" and after"); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = w.Close()
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("read: %v", err)
	}
	_ = rc.Close()

	if len(*events) != 1 || (*events)[0].Attribution != atime.AttributionConcurrentActivity {
		t.Fatalf("events = %+v, want one concurrent_activity event", *events)
	}
	if s := tr.Summary(); s.ConcurrentActivity != 1 || s.LikelyCollector != 0 {
		t.Errorf("summary = %+v", s)
	}
}

func TestReadDirRecordsDirectoryAccessTime(t *testing.T) {
	dir := t.TempDir()
	requireAtimeUpdates(t, dir)
	sub := filepath.Join(dir, "Profiles")
	fixture(t, sub, "a", "1")
	stageOldAtime(t, sub)

	tr := atime.NewTracker()
	events := collectEvents(tr)
	src := tr.Source(newLive(t), "p", atime.PhaseDetect)

	entries, err := src.ReadDir(sub)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ReadDir = %v, %v", entries, err)
	}
	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	e := (*events)[0]
	if e.Operation != atime.OperationReadDir || e.Path != sub || e.Phase != atime.PhaseDetect {
		t.Errorf("event = %+v", e)
	}
	if !e.Before.Atime.Equal(oldAtime) {
		t.Errorf("Before.Atime = %v, want %v", e.Before.Atime, oldAtime)
	}
}

func TestGlobObservesListedDirectoriesAndMatchesFilepathGlob(t *testing.T) {
	dir := t.TempDir()
	requireAtimeUpdates(t, dir)
	for _, p := range []string{"u1/Library/a.db", "u1/Library/b.txt", "u2/Library/c.db", "u2/.hidden/d.db", "u3/x.db"} {
		fixture(t, dir, p, "x")
	}
	lib1 := filepath.Join(dir, "u1", "Library")
	stageOldAtime(t, lib1)

	tr := atime.NewTracker()
	events := collectEvents(tr)
	src := tr.Source(newLive(t), "p", atime.PhaseCollect)

	for _, pattern := range []string{
		filepath.Join(dir, "*", "Library", "*.db"),
		filepath.Join(dir, "*", "*", "*.db"),
		filepath.Join(dir, "u1", "Library", "a.db"),
		filepath.Join(dir, "nope", "*"),
		filepath.Join(dir, "u[12]", "Library", "?.db"),
	} {
		got, err := src.Glob(pattern)
		if err != nil {
			t.Fatalf("Glob(%s): %v", pattern, err)
		}
		want, _ := filepath.Glob(pattern)
		if !slices.Equal(got, want) {
			t.Errorf("Glob(%s) = %v, want %v", pattern, got, want)
		}
	}
	if _, err := src.Glob(filepath.Join(dir, "[")); err == nil {
		t.Error("Glob with a bad pattern should fail")
	}

	var sawLib bool
	for _, e := range *events {
		if e.Path == lib1 && e.Operation == atime.OperationReadDir && e.Before.Atime.Equal(oldAtime) {
			sawLib = true
		}
	}
	if !sawLib {
		t.Errorf("no readdir event for %s in %+v", lib1, *events)
	}
}

func TestWalkObservesDirectories(t *testing.T) {
	dir := t.TempDir()
	requireAtimeUpdates(t, dir)
	fixture(t, dir, "tree/a/1.txt", "1")
	fixture(t, dir, "tree/b/2.txt", "2")
	a := filepath.Join(dir, "tree", "a")
	stageOldAtime(t, a)

	tr := atime.NewTracker()
	events := collectEvents(tr)
	src := tr.Source(newLive(t), "p", atime.PhaseCollect)

	var visited []string
	err := src.Walk(filepath.Join(dir, "tree"), func(path string, _ macfor.FileInfo, err error) error {
		visited = append(visited, path)
		return err
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(visited) != 5 {
		t.Errorf("visited %d paths, want 5: %v", len(visited), visited)
	}
	var sawA bool
	for _, e := range *events {
		if e.Path == a && e.Operation == atime.OperationReadDir {
			sawA = true
		}
	}
	if !sawA {
		t.Errorf("no readdir event for %s in %+v", a, *events)
	}
}

func TestReadPlistIsObserved(t *testing.T) {
	dir := t.TempDir()
	requireAtimeUpdates(t, dir)
	path := fixture(t, dir, "Info.plist", `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>Name</key><string>macfor</string></dict></plist>`)

	tr := atime.NewTracker()
	events := collectEvents(tr)
	src := tr.Source(newLive(t), "p", atime.PhaseCollect)

	var v struct {
		Name string `plist:"Name"`
	}
	if err := src.ReadPlist(path, &v); err != nil {
		t.Fatalf("ReadPlist: %v", err)
	}
	if v.Name != "macfor" {
		t.Errorf("Name = %q", v.Name)
	}
	if len(*events) != 1 || (*events)[0].Operation != atime.OperationRead {
		t.Errorf("events = %+v, want one read event", *events)
	}
	if err := src.ReadPlist(filepath.Join(dir, "missing.plist"), &v); err == nil {
		t.Error("ReadPlist of a missing file should fail")
	}
	bad := fixture(t, dir, "bad.plist", "not a plist \x00\x01")
	if err := src.ReadPlist(bad, &v); err == nil || !strings.Contains(err.Error(), "plist") {
		t.Errorf("ReadPlist of a bad plist = %v, want a parse error", err)
	}
}

func TestEventsBufferUntilSinkIsSet(t *testing.T) {
	dir := t.TempDir()
	requireAtimeUpdates(t, dir)
	path := fixture(t, dir, "f", "data")

	tr := atime.NewTracker()
	src := tr.Source(newLive(t), "p", atime.PhaseDetect)
	rc, err := src.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, _ = io.ReadAll(rc)
	_ = rc.Close()

	events := collectEvents(tr)
	if len(*events) != 1 {
		t.Fatalf("buffered events delivered = %d, want 1", len(*events))
	}
}

func TestUnclosedFilesAreCounted(t *testing.T) {
	dir := t.TempDir()
	path := fixture(t, dir, "f", "data")
	tr := atime.NewTracker()
	src := tr.Source(newLive(t), "p", atime.PhaseCollect)
	rc, err := src.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s := tr.Summary(); s.Unclosed != 1 {
		t.Errorf("Unclosed = %d, want 1 while open", s.Unclosed)
	}
	_ = rc.Close()
	if s := tr.Summary(); s.Unclosed != 0 {
		t.Errorf("Unclosed = %d, want 0 after Close", s.Unclosed)
	}
}

func TestObservedFileKeepsSeekAndReaderAt(t *testing.T) {
	dir := t.TempDir()
	path := fixture(t, dir, "f", "0123456789")
	tr := atime.NewTracker()
	src := tr.Source(newLive(t), "p", atime.PhaseCollect)
	rc, err := src.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	s, ok := rc.(io.Seeker)
	if !ok {
		t.Fatal("observed file should implement io.Seeker")
	}
	if _, err := s.Seek(5, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	rest, _ := io.ReadAll(rc)
	if string(rest) != "56789" {
		t.Errorf("read after seek = %q", rest)
	}
	if _, ok := rc.(io.ReaderAt); !ok {
		t.Error("observed file should implement io.ReaderAt")
	}
}

type readlinker interface {
	Readlink(path string) (string, error)
}

func TestReadlinkIsForwardedOnlyWhenWrappedSourceHasIt(t *testing.T) {
	tr := atime.NewTracker()

	if _, ok := tr.Source(newLive(t), "p", atime.PhaseCollect).(readlinker); ok {
		t.Error("observed live source must not claim Readlink: the live source has none")
	}

	root := t.TempDir()
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	dsrc, err := directory.New(root)
	if err != nil {
		t.Fatalf("directory.New: %v", err)
	}
	obs := tr.Source(dsrc, "p", atime.PhaseCollect)
	rl, ok := obs.(readlinker)
	if !ok {
		t.Fatal("observed directory source must forward Readlink")
	}
	if got, err := rl.Readlink("/link"); err != nil || got != "target" {
		t.Errorf("Readlink = %q, %v", got, err)
	}
	if obs.Type() != macfor.SourceTypeDirectory {
		t.Errorf("Type = %q, want directory", obs.Type())
	}
}

func TestDirectorySourceReadsAreObserved(t *testing.T) {
	root := t.TempDir()
	requireAtimeUpdates(t, root)
	host := fixture(t, root, "Users/alice/.zsh_history", ": 1:0;ls")

	dsrc, err := directory.New(root)
	if err != nil {
		t.Fatalf("directory.New: %v", err)
	}
	tr := atime.NewTracker()
	events := collectEvents(tr)
	src := tr.Source(dsrc, "shell.history", atime.PhaseCollect)

	rc, err := src.Open("/Users/alice/.zsh_history")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, _ = io.ReadAll(rc)
	_ = rc.Close()

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	if !(*events)[0].Before.Atime.Equal(oldAtime) {
		t.Errorf("Before.Atime = %v", (*events)[0].Before.Atime)
	}
	if got := atimeOf(t, host); got.Equal(oldAtime) {
		t.Error("host file atime should have changed")
	}
}

// BenchmarkObservedOpen measures the overhead of the two extra fstat calls
// per file read.
func BenchmarkObservedOpen(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		b.Fatal(err)
	}
	src, err := live.New()
	if err != nil {
		b.Fatal(err)
	}
	run := func(b *testing.B, s macfor.Source) {
		for b.Loop() {
			rc, err := s.Open(path)
			if err != nil {
				b.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, rc)
			_ = rc.Close()
		}
	}
	b.Run("plain", func(b *testing.B) { run(b, src) })
	b.Run("observed", func(b *testing.B) {
		run(b, atime.NewTracker().Source(src, "p", atime.PhaseCollect))
	})
}
