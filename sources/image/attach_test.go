package image

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const (
	singleFixture = "hdiutil_attach_single.plist"
	groupFixture  = "hdiutil_attach_apfs_group.plist"
)

// fakeCall records one invocation of fakeRunner.
type fakeCall struct {
	name string
	args []string
}

// fakeRunner is a commandRunner that answers from a script instead of running
// real tools.
type fakeRunner struct {
	mu    sync.Mutex
	calls []fakeCall

	// respond returns the result for a call; nil means success with no output.
	respond func(ctx context.Context, n int, args []string) ([]byte, error)
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{name: name, args: args})
	n := len(f.calls) - 1
	f.mu.Unlock()

	if f.respond == nil {
		return nil, nil
	}
	return f.respond(ctx, n, args)
}

// commands returns each recorded call as a single space-joined string.
func (f *fakeRunner) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, strings.Join(append([]string{filepath.Base(c.name)}, c.args...), " "))
	}
	return out
}

// loadFixture returns a recorded hdiutil plist with its /Volumes mount points
// relocated under root, so the classification can inspect real directories.
func loadFixture(t *testing.T, name, root string) []byte {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return bytes.ReplaceAll(b, []byte("/Volumes/"), []byte(root+"/"))
}

// mkTree creates each relative path under root; a trailing slash makes a
// directory, anything else an empty file.
func mkTree(t *testing.T, root string, paths ...string) {
	t.Helper()

	for _, p := range paths {
		full := filepath.Join(root, p)
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, nil, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

// layoutGroup builds the volumes of a 10.15+ APFS volume group under root.
func layoutGroup(t *testing.T, root string) {
	t.Helper()

	mkTree(t, filepath.Join(root, "Macintosh HD"), systemVersionPlist, "usr/bin/")
	mkTree(t, filepath.Join(root, "Macintosh HD - Data"), "Users/alice/", "private/var/db/", "Library/")
	mkTree(t, filepath.Join(root, "Preboot"), "8A3F/")
	mkTree(t, filepath.Join(root, "Recovery"), "8A3F/")
	mkTree(t, filepath.Join(root, "VM"), "sleepimage")
}

func TestParseAttachPlist(t *testing.T) {
	t.Run("single volume", func(t *testing.T) {
		entities, err := parseAttachPlist(loadFixture(t, singleFixture, "/Volumes"))
		if err != nil {
			t.Fatalf("parseAttachPlist: %v", err)
		}
		if len(entities) != 3 {
			t.Fatalf("got %d entities, want 3", len(entities))
		}
		if entities[0].DevEntry != "/dev/disk4" || entities[0].MountPoint != "" {
			t.Errorf("scheme entity = %+v", entities[0])
		}
		if got := entities[2]; got.MountPoint != "/Volumes/Macintosh HD" || got.VolumeKind != "hfs" {
			t.Errorf("volume entity = %+v", got)
		}
	})

	t.Run("APFS volume group", func(t *testing.T) {
		entities, err := parseAttachPlist(loadFixture(t, groupFixture, "/Volumes"))
		if err != nil {
			t.Fatalf("parseAttachPlist: %v", err)
		}
		if len(entities) != 9 {
			t.Fatalf("got %d entities, want 9", len(entities))
		}
		var mounted int
		for _, e := range entities {
			if e.MountPoint != "" {
				mounted++
			}
		}
		if mounted != 5 {
			t.Errorf("got %d mounted entities, want 5", mounted)
		}
	})

	t.Run("leading text before XML", func(t *testing.T) {
		out := append([]byte("expected CRC32 $D1E0C2A5\n"), loadFixture(t, singleFixture, "/Volumes")...)
		if _, err := parseAttachPlist(out); err != nil {
			t.Fatalf("parseAttachPlist: %v", err)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		if _, err := parseAttachPlist([]byte("<?xml version=\"1.0\"?><plist><dict><key>")); err == nil {
			t.Fatal("want error for malformed plist")
		}
	})

	t.Run("no system-entities", func(t *testing.T) {
		empty := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>system-entities</key><array/></dict></plist>`
		if _, err := parseAttachPlist([]byte(empty)); err == nil {
			t.Fatal("want error for empty system-entities")
		}
	})
}

func TestTopLevelDevNode(t *testing.T) {
	tests := []struct {
		name     string
		entities []hdiutilEntity
		want     string
	}{
		{
			name:     "single volume uses physical disk",
			entities: mustParse(t, singleFixture),
			want:     "/dev/disk4",
		},
		{
			name:     "APFS group uses physical disk, not synthesized container",
			entities: mustParse(t, groupFixture),
			want:     "/dev/disk4",
		},
		{
			name:     "real HFS+ attach lists the slice before the whole disk",
			entities: mustParse(t, "hdiutil_attach_hfs_real.plist"),
			want:     "/dev/disk4",
		},
		{
			name:     "real APFS attach includes a synthesized container",
			entities: mustParse(t, "hdiutil_attach_apfs_real.plist"),
			want:     "/dev/disk4",
		},
		{
			name: "synthesized container listed before the image disk",
			entities: []hdiutilEntity{
				{DevEntry: "/dev/disk5", ContentHint: apfsContainerHint},
				{DevEntry: "/dev/disk5s1", MountPoint: "/Volumes/X"},
				{DevEntry: "/dev/disk4", ContentHint: "GUID_partition_scheme"},
				{DevEntry: "/dev/disk4s1", ContentHint: "Apple_APFS"},
			},
			want: "/dev/disk4",
		},
		{
			name:     "unpartitioned raw volume",
			entities: []hdiutilEntity{{DevEntry: "/dev/disk7", MountPoint: "/Volumes/X"}},
			want:     "/dev/disk7",
		},
		{
			name:     "only slices reported",
			entities: []hdiutilEntity{{DevEntry: "/dev/disk9s1s1"}, {DevEntry: "/dev/disk9s2"}},
			want:     "/dev/disk9",
		},
		{
			name:     "nothing usable",
			entities: []hdiutilEntity{{DevEntry: "bogus"}},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topLevelDevNode(tt.entities); got != tt.want {
				t.Errorf("topLevelDevNode = %q, want %q", got, tt.want)
			}
		})
	}
}

// mustParse parses a fixture without relocating its mount points.
func mustParse(t *testing.T, name string) []hdiutilEntity {
	t.Helper()

	entities, err := parseAttachPlist(loadFixture(t, name, "/Volumes"))
	if err != nil {
		t.Fatalf("parseAttachPlist: %v", err)
	}
	return entities
}

func TestNewAttachmentRoots(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		layout  func(t *testing.T, root string)
		want    func(root string) []string
	}{
		{
			name:    "single volume with users is not split",
			fixture: singleFixture,
			layout: func(t *testing.T, root string) {
				mkTree(t, filepath.Join(root, "Macintosh HD"), systemVersionPlist, "Users/bob/")
			},
			want: func(root string) []string {
				return []string{filepath.Join(root, "Macintosh HD")}
			},
		},
		{
			name:    "APFS volume group is data then system",
			fixture: groupFixture,
			layout:  layoutGroup,
			want: func(root string) []string {
				return []string{filepath.Join(root, "Macintosh HD - Data"), filepath.Join(root, "Macintosh HD")}
			},
		},
		{
			name:    "group without a data volume is system only",
			fixture: groupFixture,
			layout: func(t *testing.T, root string) {
				mkTree(t, filepath.Join(root, "Macintosh HD"), systemVersionPlist)
			},
			want: func(root string) []string {
				return []string{filepath.Join(root, "Macintosh HD")}
			},
		},
		{
			name:    "data volume via private/var only",
			fixture: groupFixture,
			layout: func(t *testing.T, root string) {
				mkTree(t, filepath.Join(root, "Macintosh HD"), systemVersionPlist)
				mkTree(t, filepath.Join(root, "Macintosh HD - Data"), "private/var/")
			},
			want: func(root string) []string {
				return []string{filepath.Join(root, "Macintosh HD - Data"), filepath.Join(root, "Macintosh HD")}
			},
		},
		{
			name:    "no system volume falls back to first mounted",
			fixture: groupFixture,
			layout:  func(*testing.T, string) {},
			want: func(root string) []string {
				return []string{filepath.Join(root, "Macintosh HD - Data")}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.layout(t, root)

			entities, err := parseAttachPlist(loadFixture(t, tt.fixture, root))
			if err != nil {
				t.Fatalf("parseAttachPlist: %v", err)
			}

			a := newAttachment(&fakeRunner{}, entities)
			if got, want := a.roots(), tt.want(root); !reflect.DeepEqual(got, want) {
				t.Errorf("roots = %q, want %q", got, want)
			}
		})
	}

	t.Run("nothing mounted", func(t *testing.T) {
		a := newAttachment(&fakeRunner{}, []hdiutilEntity{{DevEntry: "/dev/disk4"}})
		if a.mounted() || a.roots() != nil {
			t.Errorf("mounted = %v, roots = %q", a.mounted(), a.roots())
		}
	})
}

func TestAttachArgs(t *testing.T) {
	if got, want := attachArgs("/e/x.dmg", FormatDMG), []string{"attach", "-readonly", "-nobrowse", "-plist", "/e/x.dmg"}; !reflect.DeepEqual(got, want) {
		t.Errorf("dmg args = %q, want %q", got, want)
	}

	want := []string{"attach", "-readonly", "-nobrowse", "-plist", "-imagekey", "diskimage-class=CRawDiskImage", "/e/x.dd"}
	if got := attachArgs("/e/x.dd", FormatRaw); !reflect.DeepEqual(got, want) {
		t.Errorf("raw args = %q, want %q", got, want)
	}
}

func TestAttachWith(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		root := t.TempDir()
		layoutGroup(t, root)
		out := loadFixture(t, groupFixture, root)

		r := &fakeRunner{respond: func(_ context.Context, _ int, _ []string) ([]byte, error) { return out, nil }}
		a, err := attachWith(context.Background(), r, "/e/mac.dmg", FormatDMG)
		if err != nil {
			t.Fatalf("attachWith: %v", err)
		}
		if a.devNode != "/dev/disk4" || len(a.roots()) != 2 {
			t.Errorf("devNode = %q, roots = %q", a.devNode, a.roots())
		}
		if got := r.commands(); len(got) != 1 || got[0] != "hdiutil attach -readonly -nobrowse -plist /e/mac.dmg" {
			t.Errorf("commands = %q", got)
		}
		if r.calls[0].name != hdiutilPath {
			t.Errorf("ran %q, want %q", r.calls[0].name, hdiutilPath)
		}
	})

	t.Run("hdiutil failure carries stderr", func(t *testing.T) {
		r := &fakeRunner{respond: func(context.Context, int, []string) ([]byte, error) {
			return nil, errors.New("hdiutil attach: exit status 1: hdiutil: attach failed - image not recognized")
		}}
		_, err := attachWith(context.Background(), r, "/e/bad.dmg", FormatDMG)
		if err == nil || !strings.Contains(err.Error(), "image not recognized") {
			t.Fatalf("err = %v, want stderr text", err)
		}
	})

	t.Run("unparseable output", func(t *testing.T) {
		r := &fakeRunner{respond: func(context.Context, int, []string) ([]byte, error) { return []byte("garbage"), nil }}
		if _, err := attachWith(context.Background(), r, "/e/x.dmg", FormatDMG); err == nil {
			t.Fatal("want parse error")
		}
	})

	t.Run("nothing mounted detaches", func(t *testing.T) {
		unmounted := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>system-entities</key><array>` +
			`<dict><key>dev-entry</key><string>/dev/disk6</string></dict>` +
			`<dict><key>dev-entry</key><string>/dev/disk6s1</string></dict></array></dict></plist>`
		r := &fakeRunner{respond: func(_ context.Context, n int, _ []string) ([]byte, error) {
			if n == 0 {
				return []byte(unmounted), nil
			}
			return nil, nil
		}}

		_, err := attachWith(context.Background(), r, "/e/x.dd", FormatRaw)
		if !errors.Is(err, ErrNoMountedVolume) {
			t.Fatalf("err = %v, want ErrNoMountedVolume", err)
		}
		if got := r.commands(); len(got) != 2 || got[1] != "hdiutil detach /dev/disk6" {
			t.Errorf("commands = %q", got)
		}
	})

	t.Run("cancelled during attach detaches", func(t *testing.T) {
		root := t.TempDir()
		layoutGroup(t, root)
		out := loadFixture(t, groupFixture, root)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var detachCtxErr error
		r := &fakeRunner{respond: func(c context.Context, n int, _ []string) ([]byte, error) {
			if n == 0 {
				// The attach completes, then the caller gives up.
				cancel()
				return out, nil
			}
			detachCtxErr = c.Err()
			return nil, nil
		}}

		a, err := attachWith(ctx, r, "/e/mac.dmg", FormatDMG)
		if !errors.Is(err, context.Canceled) || a != nil {
			t.Fatalf("attachWith = %v, %v; want nil, context.Canceled", a, err)
		}
		if got := r.commands(); len(got) != 2 || got[1] != "hdiutil detach /dev/disk4" {
			t.Errorf("commands = %q", got)
		}
		if detachCtxErr != nil {
			t.Errorf("cleanup detach ran on a done context: %v", detachCtxErr)
		}
	})

	t.Run("already cancelled runs nothing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		r := &fakeRunner{}
		if _, err := attachWith(ctx, r, "/e/x.dmg", FormatDMG); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if len(r.commands()) != 0 {
			t.Errorf("commands = %q, want none", r.commands())
		}
	})
}

func TestDetach(t *testing.T) {
	busy := errors.New("hdiutil detach: exit status 16: hdiutil: couldn't unmount \"disk4\" - Resource busy")

	t.Run("idempotent", func(t *testing.T) {
		r := &fakeRunner{}
		a := &attachment{runner: r, devNode: "/dev/disk4"}

		for i := range 2 {
			if err := a.detach(context.Background()); err != nil {
				t.Fatalf("detach #%d: %v", i+1, err)
			}
		}
		if got := r.commands(); len(got) != 1 || got[0] != "hdiutil detach /dev/disk4" {
			t.Errorf("commands = %q, want one detach", got)
		}
	})

	t.Run("busy volume retried with force", func(t *testing.T) {
		r := &fakeRunner{respond: func(_ context.Context, n int, _ []string) ([]byte, error) {
			if n == 0 {
				return nil, busy
			}
			return nil, nil
		}}
		a := &attachment{runner: r, devNode: "/dev/disk4"}

		if err := a.detach(context.Background()); err != nil {
			t.Fatalf("detach: %v", err)
		}
		want := []string{"hdiutil detach /dev/disk4", "hdiutil detach -force /dev/disk4"}
		if got := r.commands(); !reflect.DeepEqual(got, want) {
			t.Errorf("commands = %q, want %q", got, want)
		}
	})

	t.Run("force failure reports both errors and can be retried", func(t *testing.T) {
		fail := true
		r := &fakeRunner{respond: func(context.Context, int, []string) ([]byte, error) {
			if fail {
				return nil, busy
			}
			return nil, nil
		}}
		a := &attachment{runner: r, devNode: "/dev/disk4"}

		err := a.detach(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Resource busy") || !strings.Contains(err.Error(), "forced retry") {
			t.Fatalf("err = %v, want both attempts with stderr", err)
		}

		fail = false
		if err := a.detach(context.Background()); err != nil {
			t.Fatalf("second detach: %v", err)
		}
		if got := len(r.commands()); got != 3 {
			t.Errorf("got %d commands, want 3", got)
		}
	})

	t.Run("no device node", func(t *testing.T) {
		a := &attachment{runner: &fakeRunner{}}
		if err := a.detach(context.Background()); err == nil {
			t.Fatal("want error without a device node")
		}
	})
}

func TestExecRunnerIncludesStderr(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}

	out, err := execRunner{}.Run(context.Background(), "/bin/sh", "-c", "echo partial; echo 'attach failed - no mountable file systems' >&2; exit 3")
	if err == nil {
		t.Fatal("want error from failing command")
	}
	if !strings.Contains(err.Error(), "no mountable file systems") || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("err = %v, want stderr and exit status", err)
	}
	if strings.TrimSpace(string(out)) != "partial" {
		t.Errorf("stdout = %q", out)
	}

	out, err = execRunner{}.Run(context.Background(), "/bin/sh", "-c", "echo ok")
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("Run = %q, %v", out, err)
	}
}
