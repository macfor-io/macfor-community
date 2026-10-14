package image

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/testdata/macostree"
)

const fakeDevNode = "/dev/disk9"

// writeRawImage writes a minimal raw APFS image whose content DetectFormat
// recognises, and returns its path.
func writeRawImage(t *testing.T) string {
	t.Helper()
	return writeImage(t, "evidence.raw", rawWithMagic(apfsMagicOffset, apfsMagic))
}

// writeE01Image writes a minimal E01 segment and returns its path.
func writeE01Image(t *testing.T) string {
	t.Helper()
	return writeImage(t, "evidence.E01", padTo(ewfMagic, minImageSize))
}

// splitTree builds a synthetic 10.15+ volume group: a Data root holding users,
// preferences, and dslocal, and a System root holding SystemVersion.plist plus
// a system-only file. It returns the data and system roots.
func splitTree(t *testing.T) (data, system string) {
	t.Helper()

	opts := macostree.Defaults()
	opts.Users[0].ZshHistory = ": 1700000000:0;ls -la\n"
	data = macostree.MustBuild(t, opts)

	system = t.TempDir()
	if err := os.Rename(filepath.Join(data, "System"), filepath.Join(system, "System")); err != nil {
		t.Fatalf("move System to system root: %v", err)
	}

	writeFile(t, filepath.Join(system, "System", "Library", "Kernels", "kernel"), "kernel")
	// The System volume carries an empty Users placeholder, as on a real group.
	if err := os.MkdirAll(filepath.Join(system, "Users"), 0o755); err != nil {
		t.Fatalf("mkdir Users placeholder: %v", err)
	}
	return data, system
}

// writeFile writes content to path, creating parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fakeAttacher records the attach call and returns an attachment over the
// given roots whose detach runs through runner.
type fakeAttacher struct {
	runner *fakeRunner
	data   string
	system string
	err    error

	called     bool
	gotPath    string
	gotFormat  Format
	attachment *attachment
}

func (f *fakeAttacher) attach(_ context.Context, path string, format Format) (*attachment, error) {
	f.called = true
	f.gotPath = path
	f.gotFormat = format
	if f.err != nil {
		return nil, f.err
	}
	f.attachment = &attachment{
		runner:  f.runner,
		devNode: fakeDevNode,
		system:  f.system,
		data:    f.data,
	}
	return f.attachment, nil
}

// newFakeSource builds a Source over the given roots with a fake attacher. An
// empty data root means a single-volume image.
func newFakeSource(t *testing.T, data, system string) (*Source, *fakeAttacher) {
	t.Helper()

	fa := &fakeAttacher{runner: &fakeRunner{}, data: data, system: system}
	src, err := newWith(context.Background(), writeRawImage(t), fa.attach)
	if err != nil {
		t.Fatalf("newWith: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })
	return src, fa
}

func TestSourceType(t *testing.T) {
	data, system := splitTree(t)
	src, _ := newFakeSource(t, data, system)

	if got := src.Type(); got != macfor.SourceTypeImage {
		t.Errorf("Type() = %q, want %q", got, macfor.SourceTypeImage)
	}
}

func TestNewPassesAbsolutePathAndFormat(t *testing.T) {
	data, system := splitTree(t)
	img := writeRawImage(t)

	// Pass a relative path; the attacher and provenance must see it absolute.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, img)
	if err != nil {
		t.Skipf("no relative path to temp image: %v", err)
	}

	fa := &fakeAttacher{runner: &fakeRunner{}, data: data, system: system}
	src, err := newWith(context.Background(), rel, fa.attach)
	if err != nil {
		t.Fatalf("newWith: %v", err)
	}
	defer func() { _ = src.Close() }()

	if fa.gotPath != img {
		t.Errorf("attach path = %q, want absolute %q", fa.gotPath, img)
	}
	if fa.gotFormat != FormatRaw {
		t.Errorf("attach format = %q, want %q", fa.gotFormat, FormatRaw)
	}
	if src.ImagePath() != img {
		t.Errorf("ImagePath() = %q, want %q", src.ImagePath(), img)
	}
	if src.Format() != FormatRaw {
		t.Errorf("Format() = %q, want %q", src.Format(), FormatRaw)
	}
}

func TestNewDetectsE01(t *testing.T) {
	data, system := splitTree(t)

	fa := &fakeAttacher{runner: &fakeRunner{}, data: data, system: system}
	src, err := newWith(context.Background(), writeE01Image(t), fa.attach)
	if err != nil {
		t.Fatalf("newWith: %v", err)
	}
	defer func() { _ = src.Close() }()

	if fa.gotFormat != FormatE01 || src.Format() != FormatE01 {
		t.Errorf("format = attach %q / source %q, want %q", fa.gotFormat, src.Format(), FormatE01)
	}
}

func TestAccessors(t *testing.T) {
	t.Run("split volume group", func(t *testing.T) {
		data, system := splitTree(t)
		src, _ := newFakeSource(t, data, system)

		if got := src.MountPoint(); got != system {
			t.Errorf("MountPoint() = %q, want System root %q", got, system)
		}
		if got, want := src.Roots(), []string{data, system}; !reflect.DeepEqual(got, want) {
			t.Errorf("Roots() = %v, want %v", got, want)
		}

		// Roots returns a copy.
		roots := src.Roots()
		roots[0] = "mutated"
		if src.Roots()[0] != data {
			t.Error("Roots() exposed internal slice")
		}
	})

	t.Run("single volume", func(t *testing.T) {
		root := macostree.MustBuild(t, macostree.Defaults())
		src, _ := newFakeSource(t, "", root)

		if got := src.MountPoint(); got != root {
			t.Errorf("MountPoint() = %q, want %q", got, root)
		}
		if got, want := src.Roots(), []string{root}; !reflect.DeepEqual(got, want) {
			t.Errorf("Roots() = %v, want %v", got, want)
		}
	})
}

func TestDelegationMergedView(t *testing.T) {
	data, system := splitTree(t)
	src, _ := newFakeSource(t, data, system)

	t.Run("Open data-side file", func(t *testing.T) {
		rc, err := src.Open("/Users/alice/.zsh_history")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = rc.Close() }()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "ls -la") {
			t.Errorf("Open content = %q", b)
		}
	})

	t.Run("Open system-side file", func(t *testing.T) {
		rc, err := src.Open("/System/Library/Kernels/kernel")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		_ = rc.Close()
	})

	t.Run("Open missing", func(t *testing.T) {
		if _, err := src.Open("/nope"); !errors.Is(err, macfor.ErrNotFound) {
			t.Errorf("Open missing err = %v, want ErrNotFound", err)
		}
	})

	t.Run("Stat", func(t *testing.T) {
		fi, err := src.Stat("/System/Library/CoreServices/SystemVersion.plist")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if fi.IsDir() {
			t.Error("Stat reported plist as directory")
		}
	})

	t.Run("ReadDir merges roots", func(t *testing.T) {
		entries, err := src.ReadDir("/")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		names := map[string]bool{}
		for _, e := range entries {
			names[e.Name()] = true
		}
		for _, want := range []string{"System", "Users", "Library", "private"} {
			if !names[want] {
				t.Errorf("ReadDir(/) missing %q; got %v", want, names)
			}
		}
	})

	t.Run("Glob", func(t *testing.T) {
		got, err := src.Glob("/Users/*/.zsh_history")
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		if want := []string{"/Users/alice/.zsh_history"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Glob = %v, want %v", got, want)
		}
	})

	t.Run("Walk crosses both roots", func(t *testing.T) {
		var seen []string
		err := src.Walk("/", func(path string, _ macfor.FileInfo, err error) error {
			if err != nil {
				return err
			}
			seen = append(seen, path)
			return nil
		})
		if err != nil {
			t.Fatalf("Walk: %v", err)
		}
		sort.Strings(seen)
		for _, want := range []string{"/Users/alice/.zsh_history", "/System/Library/Kernels/kernel"} {
			if i := sort.SearchStrings(seen, want); i >= len(seen) || seen[i] != want {
				t.Errorf("Walk did not visit %q", want)
			}
		}
	})

	t.Run("Users", func(t *testing.T) {
		users, err := src.Users()
		if err != nil {
			t.Fatalf("Users: %v", err)
		}
		if len(users) != 1 || users[0].Username != "alice" || users[0].UID != 501 {
			t.Errorf("Users = %+v, want alice/501", users)
		}
	})

	t.Run("ResolvePath", func(t *testing.T) {
		if got, want := src.ResolvePath("~/Library"), "/Users/alice/Library"; got != want {
			t.Errorf("ResolvePath = %q, want %q", got, want)
		}
	})

	t.Run("ReadPlist", func(t *testing.T) {
		var sv struct {
			ProductVersion string `plist:"ProductVersion"`
		}
		if err := src.ReadPlist("/System/Library/CoreServices/SystemVersion.plist", &sv); err != nil {
			t.Fatalf("ReadPlist: %v", err)
		}
		if sv.ProductVersion != "14.2.1" {
			t.Errorf("ProductVersion = %q", sv.ProductVersion)
		}
	})

	t.Run("OSVersion from System root", func(t *testing.T) {
		v, err := src.OSVersion()
		if err != nil || v != "14.2.1" {
			t.Errorf("OSVersion = %q, %v; want 14.2.1", v, err)
		}
	})

	t.Run("Hostname from Data root", func(t *testing.T) {
		// The directory source prefers ComputerName, read from the Data root.
		h, err := src.Hostname()
		if err != nil || h != "Alice's MacBook Pro" {
			t.Errorf("Hostname = %q, %v; want Alice's MacBook Pro", h, err)
		}
	})
}

func TestDelegationSingleVolume(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())
	src, _ := newFakeSource(t, "", root)

	if v, err := src.OSVersion(); err != nil || v != "14.2.1" {
		t.Errorf("OSVersion = %q, %v", v, err)
	}
	users, err := src.Users()
	if err != nil || len(users) != 1 || users[0].Username != "alice" {
		t.Errorf("Users = %+v, %v", users, err)
	}
}

func TestSystemInfoProvenance(t *testing.T) {
	tests := []struct {
		name   string
		image  func(t *testing.T) string
		format Format
	}{
		{name: "raw", image: writeRawImage, format: FormatRaw},
		{name: "e01", image: writeE01Image, format: FormatE01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, system := splitTree(t)
			img := tt.image(t)

			fa := &fakeAttacher{runner: &fakeRunner{}, data: data, system: system}
			src, err := newWith(context.Background(), img, fa.attach)
			if err != nil {
				t.Fatalf("newWith: %v", err)
			}
			defer func() { _ = src.Close() }()

			info, err := src.SystemInfo()
			if err != nil {
				t.Fatalf("SystemInfo error: %v", err)
			}
			if info == nil {
				t.Fatal("SystemInfo returned nil")
			}
			if info.SourceType != macfor.SourceTypeImage {
				t.Errorf("SourceType = %q, want image", info.SourceType)
			}
			if info.ImagePath != img {
				t.Errorf("ImagePath = %q, want image file %q", info.ImagePath, img)
			}
			if info.ImagePath == data || info.ImagePath == system {
				t.Errorf("ImagePath %q is a mount point", info.ImagePath)
			}
			if info.ImageFormat != string(tt.format) {
				t.Errorf("ImageFormat = %q, want %q", info.ImageFormat, tt.format)
			}

			// Delegated fields come through the merged view.
			if info.OSVersion != "14.2.1" || info.OSCodename == "" {
				t.Errorf("OSVersion/Codename = %q/%q", info.OSVersion, info.OSCodename)
			}
			if info.Hostname != "alices-mbp.local" || info.ComputerName != "Alice's MacBook Pro" {
				t.Errorf("Hostname/ComputerName = %q/%q", info.Hostname, info.ComputerName)
			}
		})
	}
}

func TestSystemInfoEmptyTree(t *testing.T) {
	src, _ := newFakeSource(t, "", t.TempDir())

	info, err := src.SystemInfo()
	if err != nil || info == nil {
		t.Fatalf("SystemInfo = %v, %v; want populated struct, nil", info, err)
	}
	if info.SourceType != macfor.SourceTypeImage || info.ImageFormat != string(FormatRaw) {
		t.Errorf("provenance = %q/%q", info.SourceType, info.ImageFormat)
	}
	if info.OSVersion != "unknown" || info.Hostname != "unknown" {
		t.Errorf("OSVersion/Hostname = %q/%q, want unknown", info.OSVersion, info.Hostname)
	}
}

func TestCloseDetachesOnce(t *testing.T) {
	data, system := splitTree(t)
	src, fa := newFakeSource(t, data, system)

	if err := src.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	want := []string{"hdiutil detach " + fakeDevNode}
	if got := fa.runner.commands(); !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestCloseRetriesAfterFailure(t *testing.T) {
	data, system := splitTree(t)

	fail := true
	r := &fakeRunner{respond: func(context.Context, int, []string) ([]byte, error) {
		if fail {
			return nil, errors.New("resource busy")
		}
		return nil, nil
	}}
	fa := &fakeAttacher{runner: r, data: data, system: system}
	src, err := newWith(context.Background(), writeRawImage(t), fa.attach)
	if err != nil {
		t.Fatalf("newWith: %v", err)
	}

	if err := src.Close(); err == nil {
		t.Fatal("Close succeeded while detach fails")
	}
	fail = false
	if err := src.Close(); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("third Close: %v", err)
	}

	// Plain + forced on the failure, then one plain detach on the retry.
	want := []string{
		"hdiutil detach " + fakeDevNode,
		"hdiutil detach -force " + fakeDevNode,
		"hdiutil detach " + fakeDevNode,
	}
	if got := r.commands(); !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestCloseReleasesEWFMount(t *testing.T) {
	data, system := splitTree(t)
	r := &fakeRunner{}

	ewfDir := t.TempDir()
	var removed []string
	attacher := func(_ context.Context, _ string, _ Format) (*attachment, error) {
		return &attachment{
			runner:  r,
			devNode: fakeDevNode,
			system:  system,
			data:    data,
			ewf: &ewfMount{
				runner:    r,
				removeAll: func(p string) error { removed = append(removed, p); return nil },
				dir:       ewfDir,
				mounted:   true,
			},
		}, nil
	}

	src, err := newWith(context.Background(), writeE01Image(t), attacher)
	if err != nil {
		t.Fatalf("newWith: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	want := []string{"hdiutil detach " + fakeDevNode, "umount " + ewfDir}
	if got := r.commands(); !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(removed, []string{ewfDir}) {
		t.Errorf("removed = %v, want [%s]", removed, ewfDir)
	}
}

func TestNewDetachesWhenDirectorySourceFails(t *testing.T) {
	// The System mount point does not exist, so NewLayered fails.
	missing := filepath.Join(t.TempDir(), "gone")
	fa := &fakeAttacher{runner: &fakeRunner{}, system: missing}

	src, err := newWith(context.Background(), writeRawImage(t), fa.attach)
	if err == nil {
		_ = src.Close()
		t.Fatal("newWith succeeded over a missing mount point")
	}
	if !errors.Is(err, macfor.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}

	want := []string{"hdiutil detach " + fakeDevNode}
	if got := fa.runner.commands(); !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v, want detach %v", got, want)
	}
}

func TestNewDetachesEvenWithCancelledContext(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	r := &fakeRunner{respond: func(ctx context.Context, _ int, _ []string) ([]byte, error) {
		return nil, ctx.Err()
	}}
	fa := &fakeAttacher{runner: r, system: missing}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := newWith(ctx, writeRawImage(t), fa.attach); err == nil {
		t.Fatal("newWith succeeded over a missing mount point")
	}
	if !fa.attachment.detached {
		t.Error("attachment not detached: cleanup used the cancelled context")
	}
}

func TestNewPropagatesErrors(t *testing.T) {
	t.Run("DetectFormat unknown", func(t *testing.T) {
		img := writeImage(t, "garbage.bin", make([]byte, headerSize+udifTrailerSize))
		fa := &fakeAttacher{runner: &fakeRunner{}}

		_, err := newWith(context.Background(), img, fa.attach)
		if !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("err = %v, want ErrUnknownFormat", err)
		}
		if fa.called {
			t.Error("attach called for an undetectable image")
		}
	})

	t.Run("DetectFormat missing file", func(t *testing.T) {
		fa := &fakeAttacher{runner: &fakeRunner{}}
		_, err := newWith(context.Background(), filepath.Join(t.TempDir(), "absent.dmg"), fa.attach)
		if err == nil {
			t.Fatal("newWith succeeded for a missing image")
		}
		if fa.called {
			t.Error("attach called for a missing image")
		}
	})

	t.Run("attach failure", func(t *testing.T) {
		fa := &fakeAttacher{runner: &fakeRunner{}, err: ErrEWFMountNotFound}
		_, err := newWith(context.Background(), writeE01Image(t), fa.attach)
		if !errors.Is(err, ErrEWFMountNotFound) {
			t.Errorf("err = %v, want ErrEWFMountNotFound", err)
		}
	})

	t.Run("attach returns nil attachment", func(t *testing.T) {
		attacher := func(context.Context, string, Format) (*attachment, error) { return nil, nil }
		_, err := newWith(context.Background(), writeRawImage(t), attacher)
		if !errors.Is(err, ErrNoMountedVolume) {
			t.Errorf("err = %v, want ErrNoMountedVolume", err)
		}
	})
}
