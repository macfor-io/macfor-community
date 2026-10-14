package directory

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/testdata/macostree"
)

// layeredTree holds the host roots of a synthetic 10.15+ System/Data split.
type layeredTree struct {
	system string
	data   string
}

// buildLayeredTree creates two synthetic roots mirroring an APFS volume group.
// The System root holds SystemVersion.plist and an empty Users/ firmlink
// placeholder; the Data root holds the user home, the dslocal user plist, and
// the SystemConfiguration preferences.plist. Both roots also carry overlapping
// entries for precedence and merge tests.
func buildLayeredTree(t *testing.T) layeredTree {
	t.Helper()

	data := macostree.MustBuild(t, macostree.Defaults())
	system := t.TempDir()

	// Move System/ from the generated tree onto the System root.
	if err := os.Rename(filepath.Join(data, "System"), filepath.Join(system, "System")); err != nil {
		t.Fatalf("move System: %v", err)
	}
	mustMkdir(t, filepath.Join(system, "Users"))

	// Overlapping file: the System copy must win.
	mustWrite(t, filepath.Join(system, "etc", "marker"), "system")
	mustWrite(t, filepath.Join(data, "etc", "marker"), "data")

	// Applications/ exists in both roots with partly overlapping contents.
	mustWrite(t, filepath.Join(system, "Applications", "A.txt"), "system-a")
	mustWrite(t, filepath.Join(data, "Applications", "B.txt"), "data-b")
	mustWrite(t, filepath.Join(system, "Applications", "C.txt"), "system-c")
	mustWrite(t, filepath.Join(data, "Applications", "C.txt"), "data-c")
	// Common is a directory on System and a file on Data: the directory wins.
	mustMkdir(t, filepath.Join(system, "Applications", "Common", "Contents"))
	mustWrite(t, filepath.Join(data, "Applications", "Common"), "data-common")

	return layeredTree{system: system, data: data}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newLayered(t *testing.T, tree layeredTree) *Source {
	t.Helper()
	src, err := NewLayered(tree.system, tree.data)
	if err != nil {
		t.Fatalf("NewLayered() error = %v", err)
	}
	return src
}

func readAll(t *testing.T, src *Source, path string) string {
	t.Helper()
	r, err := src.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(b)
}

func TestNewLayered_Errors(t *testing.T) {
	if _, err := NewLayered(); err == nil {
		t.Error("NewLayered() with no roots: expected error")
	}

	_, err := NewLayered(t.TempDir(), "/nonexistent/layered/root")
	if !errors.Is(err, macfor.ErrNotFound) {
		t.Errorf("NewLayered() with missing root: error = %v, want ErrNotFound", err)
	}
}

func TestNewLayered_SingleRootMatchesNew(t *testing.T) {
	dir := t.TempDir()
	src, err := NewLayered(dir)
	if err != nil {
		t.Fatalf("NewLayered() error = %v", err)
	}
	if src.root != dir || len(src.roots) != 1 {
		t.Errorf("root = %q, roots = %v; want single root %q", src.root, src.roots, dir)
	}
}

func TestLayered_OpenStatPrecedence(t *testing.T) {
	tree := buildLayeredTree(t)
	src := newLayered(t, tree)

	// Present in both roots: first root wins.
	if got := readAll(t, src, "/etc/marker"); got != "system" {
		t.Errorf("Open(/etc/marker) = %q, want %q", got, "system")
	}
	info, err := src.Stat("/etc/marker")
	if err != nil {
		t.Fatalf("Stat(/etc/marker) error = %v", err)
	}
	if info.Size() != int64(len("system")) {
		t.Errorf("Stat(/etc/marker).Size() = %d, want %d", info.Size(), len("system"))
	}

	// Present only in the System root.
	if _, err := src.Stat("/System/Library/CoreServices/SystemVersion.plist"); err != nil {
		t.Errorf("Stat(SystemVersion.plist) error = %v", err)
	}

	// Present only in the Data root, beneath a directory that is empty on System.
	info, err = src.Stat("/Users/alice")
	if err != nil {
		t.Fatalf("Stat(/Users/alice) error = %v", err)
	}
	if !info.IsDir() {
		t.Error("Stat(/Users/alice).IsDir() = false, want true")
	}

	// Directory on System shadows a same-named file on Data.
	info, err = src.Stat("/Applications/Common")
	if err != nil {
		t.Fatalf("Stat(/Applications/Common) error = %v", err)
	}
	if !info.IsDir() {
		t.Error("Stat(/Applications/Common).IsDir() = false, want true (System wins)")
	}

	// ReadPlist resolves through the same layering.
	var prefs preferencesPlist
	if err := src.ReadPlist("/Library/Preferences/SystemConfiguration/preferences.plist", &prefs); err != nil {
		t.Fatalf("ReadPlist(preferences.plist) error = %v", err)
	}
	if prefs.ComputerName != "Alice's MacBook Pro" {
		t.Errorf("ComputerName = %q, want %q", prefs.ComputerName, "Alice's MacBook Pro")
	}
}

func TestLayered_NotFoundFromFirstRoot(t *testing.T) {
	tree := buildLayeredTree(t)
	src := newLayered(t, tree)

	for name, call := range map[string]func() error{
		"Open": func() error { _, err := src.Open("/does/not/exist"); return err },
		"Stat": func() error { _, err := src.Stat("/does/not/exist"); return err },
		"ReadPlist": func() error {
			var v map[string]any
			return src.ReadPlist("/does/not/exist", &v)
		},
		"ReadDir": func() error { _, err := src.ReadDir("/does/not/exist"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !errors.Is(err, macfor.ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			var pathErr *fs.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("error %v does not wrap *fs.PathError", err)
			}
			if !strings.HasPrefix(pathErr.Path, tree.system) {
				t.Errorf("not-found path = %q, want it under first root %q", pathErr.Path, tree.system)
			}
		})
	}
}

func TestLayered_PermissionErrorNotMasked(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply when running as root")
	}

	tree := buildLayeredTree(t)
	// The Data root has the dslocal plist; an unreadable private/ on the System
	// root must surface as permission denied, not fall through to Data.
	locked := filepath.Join(tree.system, "private")
	mustMkdir(t, filepath.Join(locked, "var"))
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) }) //nolint:gosec // G302: Restore test dir permissions.

	src := newLayered(t, tree)
	path := "/private/var/db/dslocal/nodes/Default/users/alice.plist"

	if _, err := src.Open(path); !errors.Is(err, macfor.ErrPermissionDenied) {
		t.Errorf("Open() error = %v, want ErrPermissionDenied", err)
	}
	if _, err := src.Stat(path); !errors.Is(err, macfor.ErrPermissionDenied) {
		t.Errorf("Stat() error = %v, want ErrPermissionDenied", err)
	}
	if _, err := src.ReadDir("/private/var"); !errors.Is(err, macfor.ErrPermissionDenied) {
		t.Errorf("ReadDir() error = %v, want ErrPermissionDenied", err)
	}
}

func TestLayered_PathTraversal(t *testing.T) {
	src := newLayered(t, buildLayeredTree(t))

	if _, err := src.Open("/../../etc/passwd"); !errors.Is(err, macfor.ErrPermissionDenied) {
		t.Errorf("Open() traversal error = %v, want ErrPermissionDenied", err)
	}
	if _, err := src.ReadDir("/../.."); !errors.Is(err, macfor.ErrPermissionDenied) {
		t.Errorf("ReadDir() traversal error = %v, want ErrPermissionDenied", err)
	}
}

func TestLayered_ReadDirMerge(t *testing.T) {
	src := newLayered(t, buildLayeredTree(t))

	entries, err := src.ReadDir("/Applications")
	if err != nil {
		t.Fatalf("ReadDir(/Applications) error = %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if e.Name() == "Common" && !e.IsDir() {
			t.Error("Common entry is not a directory; want System root to win")
		}
	}
	want := []string{"A.txt", "B.txt", "C.txt", "Common"}
	if !slices.Equal(names, want) {
		t.Errorf("ReadDir(/Applications) = %v, want %v", names, want)
	}

	entries, err = src.ReadDir("/Users")
	if err != nil {
		t.Fatalf("ReadDir(/Users) error = %v", err)
	}
	names = names[:0]
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"Shared", "alice"}; !slices.Equal(names, want) {
		t.Errorf("ReadDir(/Users) = %v, want %v", names, want)
	}
}

func TestLayered_GlobUnion(t *testing.T) {
	src := newLayered(t, buildLayeredTree(t))

	got, err := src.Glob("/Applications/*")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	want := []string{
		"/Applications/A.txt",
		"/Applications/B.txt",
		"/Applications/C.txt",
		"/Applications/Common",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Glob(/Applications/*) = %v, want %v", got, want)
	}

	got, err = src.Glob("/Users/*")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if want := []string{"/Users/Shared", "/Users/alice"}; !slices.Equal(got, want) {
		t.Errorf("Glob(/Users/*) = %v, want %v", got, want)
	}

	got, err = src.Glob("/nothing/*")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Glob(no match) = %#v, want empty non-nil slice", got)
	}
}

func TestLayered_WalkOncePerPath(t *testing.T) {
	src := newLayered(t, buildLayeredTree(t))

	seen := map[string]int{}
	err := src.Walk("/", func(path string, info macfor.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil {
			t.Errorf("nil FileInfo for %s", path)
		}
		if !strings.HasPrefix(path, "/") {
			t.Errorf("path %q is not source-absolute", path)
		}
		seen[path]++
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	for p, n := range seen {
		if n != 1 {
			t.Errorf("path %s visited %d times, want 1", p, n)
		}
	}
	for _, p := range []string{
		"/System/Library/CoreServices/SystemVersion.plist",
		"/Users/alice",
		"/private/var/db/dslocal/nodes/Default/users/alice.plist",
		"/Library/Preferences/SystemConfiguration/preferences.plist",
		"/Applications/A.txt",
		"/Applications/B.txt",
		"/Applications/C.txt",
		"/Applications/Common/Contents",
		"/etc/marker",
	} {
		if seen[p] != 1 {
			t.Errorf("path %s visited %d times, want 1", p, seen[p])
		}
	}
}

func TestLayered_WalkSkipDir(t *testing.T) {
	src := newLayered(t, buildLayeredTree(t))

	// SkipDir on a directory skips it in every root.
	var visited []string
	err := src.Walk("/", func(path string, _ macfor.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited = append(visited, path)
		if path == "/Applications" {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	for _, p := range visited {
		if strings.HasPrefix(p, "/Applications/") {
			t.Errorf("visited %s after SkipDir on /Applications", p)
		}
	}
	if !slices.Contains(visited, "/Users/alice") {
		t.Error("walk did not continue past /Applications")
	}

	// SkipDir on a file skips the rest of its (merged) parent directory.
	visited = visited[:0]
	err = src.Walk("/Applications", func(path string, _ macfor.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited = append(visited, path)
		if path == "/Applications/A.txt" {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if want := []string{"/Applications", "/Applications/A.txt"}; !slices.Equal(visited, want) {
		t.Errorf("visited = %v, want %v", visited, want)
	}
}

func TestLayered_WalkSkipAll(t *testing.T) {
	src := newLayered(t, buildLayeredTree(t))

	calls := 0
	err := src.Walk("/", func(_ string, _ macfor.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		calls++
		if calls == 3 {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v, want nil after SkipAll", err)
	}
	if calls != 3 {
		t.Errorf("fn called %d times, want 3", calls)
	}
}

func TestLayered_WalkMissingStart(t *testing.T) {
	src := newLayered(t, buildLayeredTree(t))

	var gotErr error
	err := src.Walk("/missing", func(_ string, _ macfor.FileInfo, walkErr error) error {
		gotErr = walkErr
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if !errors.Is(gotErr, macfor.ErrNotFound) {
		t.Errorf("walk error = %v, want ErrNotFound", gotErr)
	}
}

func TestLayered_Users(t *testing.T) {
	src := newLayered(t, buildLayeredTree(t))

	users, err := src.Users()
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("Users() returned %d users, want 1: %+v", len(users), users)
	}
	u := users[0]
	if u.Username != "alice" || u.HomeDir != "/Users/alice" || u.UID != 501 || u.Name != "Alice Smith" {
		t.Errorf("Users()[0] = %+v, want alice/501 from Data root", u)
	}
	if got := src.ResolvePath("~/Library"); got != "/Users/alice/Library" {
		t.Errorf("ResolvePath(~/Library) = %q, want /Users/alice/Library", got)
	}
}

func TestLayered_SystemInfo(t *testing.T) {
	tree := buildLayeredTree(t)
	src := newLayered(t, tree)

	if v, _ := src.OSVersion(); v != "14.2.1" {
		t.Errorf("OSVersion() = %q, want 14.2.1 (from System root)", v)
	}
	if h, _ := src.Hostname(); h != "Alice's MacBook Pro" {
		t.Errorf("Hostname() = %q, want Alice's MacBook Pro (from Data root)", h)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info.OSVersion != "14.2.1" || info.OSBuild != "23C71" {
		t.Errorf("OS = %q/%q, want 14.2.1/23C71", info.OSVersion, info.OSBuild)
	}
	if info.ComputerName != "Alice's MacBook Pro" || info.Hostname != "alices-mbp.local" {
		t.Errorf("network identity = %q/%q, want values from Data root", info.ComputerName, info.Hostname)
	}
	if info.ImagePath != tree.system {
		t.Errorf("ImagePath = %q, want first root %q", info.ImagePath, tree.system)
	}
	if info.SourceType != macfor.SourceTypeDirectory {
		t.Errorf("SourceType = %v, want %v", info.SourceType, macfor.SourceTypeDirectory)
	}
}

// TestLayered_DataFirstProductionOrder layers the roots in the order the image
// source uses, Data before System, and pins the precedence that order gives.
// The other layered tests put System first; this is the order that matters.
func TestLayered_DataFirstProductionOrder(t *testing.T) {
	tree := buildLayeredTree(t)
	src, err := NewLayered(tree.data, tree.system)
	if err != nil {
		t.Fatalf("NewLayered() error = %v", err)
	}

	// Duplicate files resolve to the Data copy.
	if got := readAll(t, src, "/etc/marker"); got != "data" {
		t.Errorf("/etc/marker = %q, want the Data copy", got)
	}
	if got := readAll(t, src, "/Applications/C.txt"); got != "data-c" {
		t.Errorf("/Applications/C.txt = %q, want the Data copy", got)
	}

	// Common is a file on Data and a directory on System: Data's file wins.
	info, err := src.Stat("/Applications/Common")
	if err != nil {
		t.Fatalf("Stat(/Applications/Common) error = %v", err)
	}
	if info.IsDir() {
		t.Error("/Applications/Common is the System directory, want the Data file")
	}

	// A path under Common does not exist on Data (Common is a file there, so the
	// lookup fails with ENOTDIR, which counts as absent) and resolves on System.
	if _, err := src.Stat("/Applications/Common/Contents"); err != nil {
		t.Errorf("Stat(/Applications/Common/Contents) error = %v, want it served from System", err)
	}

	// Listings merge both roots.
	entries, err := src.ReadDir("/Applications")
	if err != nil {
		t.Fatalf("ReadDir(/Applications) error = %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"A.txt", "B.txt", "C.txt", "Common"}; !slices.Equal(names, want) {
		t.Errorf("ReadDir(/Applications) = %v, want %v", names, want)
	}

	// Identity comes from both volumes: users from Data, OS version from System.
	users, err := src.Users()
	if err != nil || len(users) != 1 || users[0].Username != "alice" || users[0].UID != 501 {
		t.Errorf("Users() = %+v, %v; want alice (501) from Data", users, err)
	}
	if v, _ := src.OSVersion(); v != macostree.Defaults().OSVersion {
		t.Errorf("OSVersion() = %q, want %q from System", v, macostree.Defaults().OSVersion)
	}
}

// TestGlob_BadPattern checks a malformed pattern is reported, as filepath.Glob does.
func TestGlob_BadPattern(t *testing.T) {
	src, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Glob("/Users/[a-"); err == nil {
		t.Error("Glob with a malformed pattern returned no error")
	}
}
