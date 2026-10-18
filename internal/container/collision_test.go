package container

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// zipEntries reads every entry of the ZIP at path into a name -> content map,
// failing the test if any name appears twice.
func zipEntries(t *testing.T, path string) map[string]string {
	t.Helper()

	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = zr.Close() }()

	entries := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		if _, dup := entries[f.Name]; dup {
			t.Fatalf("zip has duplicate entry %q", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening entry %q: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("reading entry %q: %v", f.Name, err)
		}
		entries[f.Name] = string(data)
	}
	return entries
}

func TestCollisionVariant(t *testing.T) {
	tests := []struct {
		path string
		n    int
		want string
	}{
		{"artifacts/browser/browser.chrome/files/a/History", 2, "artifacts/browser/browser.chrome/files/a/History~2"},
		{"outputs/report.md", 2, "outputs/report~2.md"},
		{"outputs/report.md", 3, "outputs/report~3.md"},
		{"files/a/.zsh_history", 2, "files/a/.zsh_history~2"},
		{"files/a/.config.json", 2, "files/a/.config~2.json"},
		{"files/a/archive.tar.gz", 2, "files/a/archive.tar~2.gz"},
		{"History", 2, "History~2"},
	}
	for _, tt := range tests {
		if got := collisionVariant(tt.path, tt.n); got != tt.want {
			t.Errorf("collisionVariant(%q, %d) = %q, want %q", tt.path, tt.n, got, tt.want)
		}
	}
}

func TestContainer_AddFileModifiedUnique(t *testing.T) {
	t.Run("disambiguates colliding paths", func(t *testing.T) {
		tests := []struct {
			name  string
			path  string
			wants []string
		}{
			{"no extension", "artifacts/x/History", []string{"artifacts/x/History", "artifacts/x/History~2"}},
			{"extension", "artifacts/x/report.md", []string{"artifacts/x/report.md", "artifacts/x/report~2.md"}},
			{"dotfile", "artifacts/x/.zsh_history", []string{"artifacts/x/.zsh_history", "artifacts/x/.zsh_history~2"}},
			{"three-way", "artifacts/x/data.db", []string{"artifacts/x/data.db", "artifacts/x/data~2.db", "artifacts/x/data~3.db"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				zipPath := filepath.Join(t.TempDir(), "test.zip")
				c, err := NewContainer(Config{OutputPath: zipPath})
				if err != nil {
					t.Fatalf("NewContainer() error = %v", err)
				}

				contents := make(map[string]string)
				for i, want := range tt.wants {
					content := fmt.Sprintf("content %d", i)
					got, err := c.AddFileModifiedUnique(tt.path, time.Time{}, strings.NewReader(content))
					if err != nil {
						t.Fatalf("AddFileModifiedUnique() #%d error = %v", i, err)
					}
					if got != want {
						t.Errorf("AddFileModifiedUnique() #%d path = %q, want %q", i, got, want)
					}
					contents[want] = content
				}

				hashes := c.Hashes()
				for p, content := range contents {
					if hashes[p] != computeSHA256(content) {
						t.Errorf("hash[%q] = %q, want %q", p, hashes[p], computeSHA256(content))
					}
				}
				if err := c.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}

				entries := zipEntries(t, zipPath)
				if len(entries) != len(contents) {
					t.Errorf("zip has %d entries, want %d", len(entries), len(contents))
				}
				for p, content := range contents {
					if entries[p] != content {
						t.Errorf("entry %q = %q, want %q", p, entries[p], content)
					}
				}
			})
		}
	})

	t.Run("skips a variant that is already taken", func(t *testing.T) {
		c, err := NewContainer(Config{OutputPath: filepath.Join(t.TempDir(), "test.zip")})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		for _, p := range []string{"a/History~2", "a/History"} {
			if _, err := c.AddFileModifiedUnique(p, time.Time{}, strings.NewReader(p)); err != nil {
				t.Fatalf("AddFileModifiedUnique(%q) error = %v", p, err)
			}
		}
		got, err := c.AddFileModifiedUnique("a/History", time.Time{}, strings.NewReader("third"))
		if err != nil {
			t.Fatalf("AddFileModifiedUnique() error = %v", err)
		}
		if got != "a/History~3" {
			t.Errorf("path = %q, want %q", got, "a/History~3")
		}
	})

	t.Run("concurrent writers get distinct paths", func(t *testing.T) {
		zipPath := filepath.Join(t.TempDir(), "test.zip")
		c, err := NewContainer(Config{OutputPath: zipPath})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		const n = 50
		paths := make([]string, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				p, err := c.AddFileModifiedUnique("a/History", time.Time{}, strings.NewReader(fmt.Sprint(i)))
				if err != nil {
					t.Errorf("AddFileModifiedUnique() error = %v", err)
				}
				paths[i] = p
			}(i)
		}
		wg.Wait()

		seen := make(map[string]bool, n)
		for _, p := range paths {
			if seen[p] {
				t.Errorf("path %q handed out twice", p)
			}
			seen[p] = true
		}
		if got := len(c.Hashes()); got != n {
			t.Errorf("Hashes() has %d entries, want %d", got, n)
		}
		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if got := len(zipEntries(t, zipPath)); got != n {
			t.Errorf("zip has %d entries, want %d", got, n)
		}
	})
}

func TestContainer_AddFile_RejectsCollision(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	c, err := NewContainer(Config{OutputPath: zipPath})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	if err := c.AddFile("records/p/parsed.jsonl", strings.NewReader("first")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}
	err = c.AddFile("records/p/parsed.jsonl", strings.NewReader("second"))
	if !errors.Is(err, ErrPathExists) {
		t.Fatalf("second AddFile() error = %v, want ErrPathExists", err)
	}

	// A generated file must not take a path already used by evidence either.
	if _, err := c.AddFileModifiedUnique("artifacts/x/file", time.Time{}, strings.NewReader("ev")); err != nil {
		t.Fatalf("AddFileModifiedUnique() error = %v", err)
	}
	if err := c.AddFile("artifacts/x/file", strings.NewReader("gen")); !errors.Is(err, ErrPathExists) {
		t.Fatalf("AddFile() over evidence error = %v, want ErrPathExists", err)
	}

	// The rejected writes leave the original content and hash intact, and the
	// container remains valid.
	if got := c.Hashes()["records/p/parsed.jsonl"]; got != computeSHA256("first") {
		t.Errorf("hash = %q, want hash of first content", got)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	entries := zipEntries(t, zipPath)
	if entries["records/p/parsed.jsonl"] != "first" {
		t.Errorf("entry = %q, want %q", entries["records/p/parsed.jsonl"], "first")
	}
	if len(entries) != 2 {
		t.Errorf("zip has %d entries, want 2", len(entries))
	}
}

func TestCollector_AddFile_BaseNameCollision(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	c, err := NewContainer(Config{OutputPath: zipPath})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0", SourceType: "live"})

	coll := NewCollector(c, "browser.chrome")

	const base = "/Users/a/Library/Application Support/Google/Chrome/"
	sources := []struct {
		sourcePath string
		content    string
	}{
		{base + "Default/History", "default profile history"},
		{base + "Profile 1/History", "profile 1 history"},
	}

	var requested string
	for _, s := range sources {
		artifact, err := macfor.NewArtifact("browser.chrome", "browser", "history")
		if err != nil {
			t.Fatalf("NewArtifact() error = %v", err)
		}
		artifact.Username = "a"
		artifact.SourcePath = s.sourcePath
		requested = artifact.ContainerPath()
		if err := coll.AddFile(context.Background(), *artifact, strings.NewReader(s.content)); err != nil {
			t.Fatalf("AddFile(%q) error = %v", s.sourcePath, err)
		}
	}

	if _, err := coll.Finalize(); err != nil {
		t.Fatalf("collector Finalize() error = %v", err)
	}
	if err := c.Finalize(); err != nil {
		t.Fatalf("container Finalize() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	renamed := requested + "~2"
	wantPaths := map[string]string{
		requested: sources[0].content,
		renamed:   sources[1].content,
	}
	wantSource := map[string]string{
		requested: sources[0].sourcePath,
		renamed:   sources[1].sourcePath,
	}

	entries := zipEntries(t, zipPath)
	var hashManifest HashManifest
	if err := json.Unmarshal([]byte(entries["hashes.json"]), &hashManifest); err != nil {
		t.Fatalf("unmarshaling hashes.json: %v", err)
	}
	for p, content := range wantPaths {
		if entries[p] != content {
			t.Errorf("entry %q = %q, want %q", p, entries[p], content)
		}
		if got := hashManifest.Files[p]; got != computeSHA256(content) {
			t.Errorf("hashes.json[%q] = %q, want %q", p, got, computeSHA256(content))
		}
	}

	var custody ChainOfCustodyLog
	if err := json.Unmarshal([]byte(entries["chain-of-custody.json"]), &custody); err != nil {
		t.Fatalf("unmarshaling chain-of-custody.json: %v", err)
	}
	found := 0
	for _, e := range custody.Entries {
		if e.Action != CustodyActionFileAdded {
			continue
		}
		found++
		content, ok := wantPaths[e.Target]
		if !ok {
			t.Errorf("unexpected custody target %q", e.Target)
			continue
		}
		if e.Hash != computeSHA256(content) {
			t.Errorf("custody hash for %q = %q, want %q", e.Target, e.Hash, computeSHA256(content))
		}
		if e.Details["source_path"] != wantSource[e.Target] {
			t.Errorf("custody source_path for %q = %v, want %q", e.Target, e.Details["source_path"], wantSource[e.Target])
		}
		if e.Target == renamed {
			if e.Details["requested_path"] != requested {
				t.Errorf("requested_path = %v, want %q", e.Details["requested_path"], requested)
			}
			if e.Details["renamed_for_collision"] != true {
				t.Errorf("renamed_for_collision = %v, want true", e.Details["renamed_for_collision"])
			}
		} else {
			if _, ok := e.Details["requested_path"]; ok {
				t.Error("unrenamed entry should not carry requested_path")
			}
			if _, ok := e.Details["renamed_for_collision"]; ok {
				t.Error("unrenamed entry should not carry renamed_for_collision")
			}
		}
	}
	if found != 2 {
		t.Errorf("found %d file_added custody entries, want 2", found)
	}
}
