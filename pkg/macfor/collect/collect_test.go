package collect

import (
	"errors"
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// testManifest builds a small manifest exercising a single-path file artifact,
// a multi-path version-gated artifact, and a glob artifact.
func testManifest() *artifacts.Manifest {
	return &artifacts.Manifest{
		SchemaVersion: "1.0",
		Plugin:        artifacts.Plugin{ID: "test.plugin"},
		Artifacts: []artifacts.Artifact{
			{
				ID: "history",
				Paths: []artifacts.PathSpec{
					{Pattern: "~/Library/Safari/History.db", Type: artifacts.PathTypeFile},
				},
			},
			{
				ID:       "cookies",
				Optional: true,
				Paths: []artifacts.PathSpec{
					{Pattern: "~/Containers/Cookies.binarycookies", Type: artifacts.PathTypeFile, OSMin: "10.14"},
					{Pattern: "~/Cookies.binarycookies", Type: artifacts.PathTypeFile},
				},
			},
			{
				ID:       "local_storage",
				Optional: true,
				Paths: []artifacts.PathSpec{
					{Pattern: "~/LocalStorage/*.localstorage", Type: artifacts.PathTypeGlob},
				},
			},
			{
				ID:       "tab_groups",
				Optional: true,
				Paths: []artifacts.PathSpec{
					{Pattern: "~/SafariTabs.db", Type: artifacts.PathTypeFile, AppMin: "15.0"},
				},
			},
		},
	}
}

func testUser() macfor.User {
	return macfor.User{Username: "alice", HomeDir: "/Users/alice"}
}

func TestLocator_Path_SinglePath(t *testing.T) {
	l := NewLocator(testManifest())
	src := &fakeSource{osVersion: "14.2.1"}

	got, err := l.Path(src, testUser(), "history")
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	want := "/Users/alice/Library/Safari/History.db"
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestLocator_Paths_MultiPathOrderingAndVersionGating(t *testing.T) {
	l := NewLocator(testManifest())

	// On macOS 14, both cookie paths resolve, sandboxed (manifest-first) first.
	modern := &fakeSource{osVersion: "14.2.1"}
	paths, err := l.Paths(modern, testUser(), "cookies")
	if err != nil {
		t.Fatalf("Paths() error = %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 cookie paths on macOS 14, got %d", len(paths))
	}
	if paths[0].Path != "/Users/alice/Containers/Cookies.binarycookies" {
		t.Errorf("expected sandboxed path first, got %q", paths[0].Path)
	}

	// On macOS 10.13, the sandboxed path (os_min 10.14) is filtered out.
	old := &fakeSource{osVersion: "10.13"}
	paths, err = l.Paths(old, testUser(), "cookies")
	if err != nil {
		t.Fatalf("Paths() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 cookie path on macOS 10.13, got %d", len(paths))
	}
	if paths[0].Path != "/Users/alice/Cookies.binarycookies" {
		t.Errorf("expected legacy path on macOS 10.13, got %q", paths[0].Path)
	}
}

func TestLocator_Path_UnknownArtifact(t *testing.T) {
	l := NewLocator(testManifest())
	src := &fakeSource{osVersion: "14.2.1"}

	if _, err := l.Path(src, testUser(), "nope"); err == nil {
		t.Error("expected error for artifact not in manifest")
	}
}

func TestLocator_Present_File(t *testing.T) {
	l := NewLocator(testManifest())
	src := &fakeSource{
		osVersion: "14.2.1",
		files:     map[string]bool{"/Users/alice/Library/Safari/History.db": true},
	}

	if !l.Present(src, testUser(), "history") {
		t.Error("expected history present when file exists")
	}
	if l.Present(src, testUser(), "cookies") {
		t.Error("expected cookies absent when no file exists")
	}
}

func TestLocator_Present_Glob(t *testing.T) {
	l := NewLocator(testManifest())
	src := &fakeSource{
		osVersion: "14.2.1",
		globs: map[string][]string{
			"/Users/alice/LocalStorage/*.localstorage": {"/Users/alice/LocalStorage/a.localstorage"},
		},
	}

	if !l.Present(src, testUser(), "local_storage") {
		t.Error("expected local_storage present when glob matches")
	}

	// No glob matches → absent.
	empty := &fakeSource{osVersion: "14.2.1"}
	if l.Present(empty, testUser(), "local_storage") {
		t.Error("expected local_storage absent when glob has no matches")
	}
}

func TestLocator_AppVersionGating(t *testing.T) {
	src := &fakeSource{osVersion: "14.2.1"}
	appVersion := func(v string) Option {
		return WithAppVersion(func(macfor.Source) string { return v })
	}

	resolve := func(l *Locator) int {
		t.Helper()
		paths, err := l.Paths(src, testUser(), "tab_groups")
		if err != nil {
			t.Fatalf("Paths() error = %v", err)
		}
		return len(paths)
	}

	// App version below app_min: the path is filtered out.
	if n := resolve(NewLocator(testManifest(), appVersion("14.1.2"))); n != 0 {
		t.Errorf("expected 0 paths below app_min, got %d", n)
	}

	// App version at/above app_min: the path resolves.
	if n := resolve(NewLocator(testManifest(), appVersion("17.2"))); n != 1 {
		t.Errorf("expected 1 path at app version 17.2, got %d", n)
	}

	// Unknown app version (empty): constraints are not applied — degrade to
	// including the path rather than silently dropping the artifact.
	if n := resolve(NewLocator(testManifest(), appVersion(""))); n != 1 {
		t.Errorf("expected 1 path for unknown app version, got %d", n)
	}

	// No AppVersionFunc configured: same degradation.
	if n := resolve(NewLocator(testManifest())); n != 1 {
		t.Errorf("expected 1 path without an AppVersionFunc, got %d", n)
	}
}

func TestLocator_Present_GlobErrorIsAbsent(t *testing.T) {
	l := NewLocator(testManifest())
	src := &fakeSource{osVersion: "14.2.1", globErr: errors.New("glob unsupported")}

	if l.Present(src, testUser(), "local_storage") {
		t.Error("expected absent when Glob returns an error")
	}
}

// --- fakeSource: a minimal Source for path-resolution tests. ---

type fakeSource struct {
	osVersion string
	files     map[string]bool
	globs     map[string][]string
	globErr   error
}

func (s *fakeSource) Type() macfor.SourceType     { return macfor.SourceTypeLive }
func (s *fakeSource) OSVersion() (string, error)  { return s.osVersion, nil }
func (s *fakeSource) Hostname() (string, error)   { return "test", nil }
func (s *fakeSource) ResolvePath(p string) string { return p }
func (s *fakeSource) Open(string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (s *fakeSource) ReadDir(string) ([]macfor.DirEntry, error) {
	return nil, errors.New("not implemented")
}
func (s *fakeSource) Walk(string, macfor.WalkFunc) error { return errors.New("not implemented") }
func (s *fakeSource) Users() ([]macfor.User, error)      { return nil, nil }
func (s *fakeSource) ReadPlist(string, any) error        { return errors.New("not implemented") }
func (s *fakeSource) SystemInfo() (*macfor.SystemInfo, error) {
	return &macfor.SystemInfo{OSVersion: s.osVersion}, nil
}

func (s *fakeSource) Stat(path string) (macfor.FileInfo, error) {
	if s.files[path] {
		return fakeFileInfo{}, nil
	}
	return nil, fs.ErrNotExist
}

func (s *fakeSource) Glob(pattern string) ([]string, error) {
	if s.globErr != nil {
		return nil, s.globErr
	}
	return s.globs[pattern], nil
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string          { return "" }
func (fakeFileInfo) Size() int64           { return 0 }
func (fakeFileInfo) Mode() fs.FileMode     { return 0 }
func (fakeFileInfo) ModTime() time.Time    { return time.Time{} }
func (fakeFileInfo) IsDir() bool           { return false }
func (fakeFileInfo) Sys() any              { return nil }
func (fakeFileInfo) AccessTime() time.Time { return time.Time{} }
func (fakeFileInfo) ChangeTime() time.Time { return time.Time{} }
func (fakeFileInfo) BirthTime() time.Time  { return time.Time{} }
