package directory

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// hostSecret is content placed outside the evidence tree. No source operation
// may ever return it.
const hostSecret = "COLLECTING-HOST-DATA"

// escapeTree builds an evidence tree whose symlinks point outside it, at a
// directory standing in for the collecting machine. It returns the tree root
// and the outside directory.
func escapeTree(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	tree := filepath.Join(base, "evidence")
	outside := filepath.Join(base, "host")

	for _, dir := range []string{
		filepath.Join(tree, "Users", "alice"),
		filepath.Join(tree, "private", "etc"),
		filepath.Join(outside, "etc"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte(hostSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "private", "etc", "hosts"), []byte("evidence hosts"), 0o600); err != nil {
		t.Fatal(err)
	}

	links := map[string]string{
		// Absolute symlink to a directory outside the tree.
		filepath.Join(tree, "Users", "alice", "Library"): outside,
		// Relative symlink climbing out of the tree.
		filepath.Join(tree, "Users", "alice", "up"): "../../../host",
		// Absolute symlink to a file outside the tree.
		filepath.Join(tree, "Users", "alice", "secret.txt"): filepath.Join(outside, "secret.txt"),
		// In-tree relative symlink, as macOS ships /etc -> private/etc.
		filepath.Join(tree, "etc"): "private/etc",
	}
	for link, target := range links {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	return tree, outside
}

// TestSymlinksDoNotEscapeRoot guards against a hostile tree making the
// collector read the collecting machine's files through symlinks.
func TestSymlinksDoNotEscapeRoot(t *testing.T) {
	tree, _ := escapeTree(t)
	src, err := New(tree)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		"/Users/alice/Library/secret.txt",
		"/Users/alice/up/secret.txt",
		"/Users/alice/secret.txt",
	} {
		t.Run("Open "+p, func(t *testing.T) {
			f, err := src.Open(p)
			if err == nil {
				b, _ := io.ReadAll(f)
				_ = f.Close()
				t.Fatalf("Open(%s) escaped the root and read %q", p, b)
			}
		})
		t.Run("ReadPlist "+p, func(t *testing.T) {
			var v map[string]any
			if err := src.ReadPlist(p, &v); err == nil {
				t.Fatalf("ReadPlist(%s) escaped the root", p)
			}
		})
	}

	t.Run("Stat through escaping directory", func(t *testing.T) {
		if _, err := src.Stat("/Users/alice/Library/secret.txt"); err == nil {
			t.Fatal("Stat followed a symlink out of the root")
		}
	})

	t.Run("ReadDir of escaping directory", func(t *testing.T) {
		entries, err := src.ReadDir("/Users/alice/Library")
		if err == nil {
			t.Fatalf("ReadDir listed a directory outside the root: %d entries", len(entries))
		}
	})

	t.Run("Glob", func(t *testing.T) {
		matches, err := src.Glob("/Users/*/Library/*.txt")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("Glob matched files outside the root: %v", matches)
		}
		matches, err = src.Glob("/Users/alice/up/*")
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("Glob matched files outside the root: %v", matches)
		}
	})

	t.Run("Walk reports symlinks without following them", func(t *testing.T) {
		var visited []string
		err := src.Walk("/", func(path string, _ macfor.FileInfo, _ error) error {
			visited = append(visited, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range visited {
			if strings.Contains(p, "secret.txt") && p != "/Users/alice/secret.txt" {
				t.Fatalf("Walk descended through a symlink out of the root: %s", p)
			}
		}
	})

	t.Run("in-tree symlinks still resolve", func(t *testing.T) {
		f, err := src.Open("/etc/hosts")
		if err != nil {
			t.Fatalf("Open(/etc/hosts) through in-tree symlink: %v", err)
		}
		b, _ := io.ReadAll(f)
		_ = f.Close()
		if string(b) != "evidence hosts" {
			t.Fatalf("Open(/etc/hosts) = %q", b)
		}
		matches, err := src.Glob("/etc/*")
		if err != nil || len(matches) != 1 || matches[0] != "/etc/hosts" {
			t.Fatalf("Glob(/etc/*) = %v, %v; want [/etc/hosts]", matches, err)
		}
	})

	t.Run("Readlink returns the stored target", func(t *testing.T) {
		target, err := src.Readlink("/etc")
		if err != nil || target != "private/etc" {
			t.Fatalf("Readlink(/etc) = %q, %v", target, err)
		}
	})
}

// TestSymlinksDoNotEscapeLayeredRoots checks confinement holds per root of a
// layered source, including when a later root holds the escaping symlink.
func TestSymlinksDoNotEscapeLayeredRoots(t *testing.T) {
	tree, _ := escapeTree(t)
	data := t.TempDir()
	src, err := NewLayered(data, tree)
	if err != nil {
		t.Fatal(err)
	}

	if f, err := src.Open("/Users/alice/Library/secret.txt"); err == nil {
		b, _ := io.ReadAll(f)
		_ = f.Close()
		t.Fatalf("layered Open escaped through the second root and read %q", b)
	}
	matches, err := src.Glob("/Users/*/Library/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("layered Glob matched files outside the roots: %v", matches)
	}
}

// TestTimezoneDoesNotFollowEscapingParent checks the localtime link is read
// from the tree even when a parent directory of it is a symlink out of the
// root: the host's localtime must never be reported as the tree's.
func TestTimezoneDoesNotFollowEscapingParent(t *testing.T) {
	base := t.TempDir()
	tree := filepath.Join(base, "evidence")
	outside := filepath.Join(base, "host")
	if err := os.MkdirAll(filepath.Join(outside, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/var/db/timezone/zoneinfo/Antarctica/Macfor_Test", filepath.Join(outside, "etc", "localtime")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tree, "private")); err != nil {
		t.Fatal(err)
	}

	src, err := New(tree)
	if err != nil {
		t.Fatal(err)
	}
	info, err := src.SystemInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.Timezone != unknownValue {
		t.Fatalf("Timezone = %q, read through a symlink out of the root; want %q", info.Timezone, unknownValue)
	}
}
