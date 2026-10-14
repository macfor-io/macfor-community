package image

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeImage writes b to a temp file and returns its path.
func writeImage(t *testing.T, name string, b []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// padTo returns b extended with zeros to at least n bytes.
func padTo(b []byte, n int) []byte {
	if len(b) >= n {
		return b
	}
	out := make([]byte, n)
	copy(out, b)
	return out
}

// rawWithMagic returns a buffer carrying want at offset, padded to a plausible
// image size.
func rawWithMagic(offset int, want []byte) []byte {
	buf := make([]byte, headerSize+udifTrailerSize)
	copy(buf[offset:], want)
	return buf
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content []byte
		want    Format
	}{
		{
			name:    "E01 segment",
			file:    "evidence.E01",
			content: padTo(ewfMagic, minImageSize),
			want:    FormatE01,
		},
		{
			name:    "Ex01 segment",
			file:    "evidence.Ex01",
			content: padTo(ewf2Magic, minImageSize),
			want:    FormatE01,
		},
		{
			name: "raw GPT disk, 512-byte blocks",
			file: "disk.dd",
			content: func() []byte {
				buf := make([]byte, headerSize)
				copy(buf[gptOffset512:], gptMagic)
				return buf
			}(),
			want: FormatRaw,
		},
		{
			name: "raw GPT disk, 4096-byte blocks",
			file: "disk.img",
			content: func() []byte {
				buf := make([]byte, headerSize)
				copy(buf[gptOffset4K:], gptMagic)
				return buf
			}(),
			want: FormatRaw,
		},
		{
			name: "read-write DMG holds a GPT disk but is still a DMG",
			file: "evidence.dmg",
			content: func() []byte {
				buf := make([]byte, headerSize+udifTrailerSize)
				copy(buf[gptOffset512:], gptMagic)
				copy(buf[len(buf)-udifTrailerSize:], kolyMagic)
				return buf
			}(),
			want: FormatDMG,
		},
		{
			name: "raw GPT disk shorter than the 4K header",
			file: "tiny.dd",
			content: func() []byte {
				buf := make([]byte, minImageSize+16)
				copy(buf[gptOffset512:], gptMagic)
				return buf
			}(),
			want: FormatRaw,
		},
		{
			name:    "E01 with misleading extension",
			file:    "evidence.dd",
			content: padTo(ewfMagic, minImageSize),
			want:    FormatE01,
		},
		{
			name: "DMG koly trailer",
			file: "evidence.dmg",
			content: func() []byte {
				buf := make([]byte, headerSize+udifTrailerSize)
				copy(buf[len(buf)-udifTrailerSize:], kolyMagic)
				return buf
			}(),
			want: FormatDMG,
		},
		{
			name:    "raw APFS container",
			file:    "evidence.raw",
			content: rawWithMagic(apfsMagicOffset, apfsMagic),
			want:    FormatRaw,
		},
		{
			name:    "raw HFS+ volume",
			file:    "evidence.img",
			content: rawWithMagic(hfsMagicOffset, hfsPlusMagic),
			want:    FormatRaw,
		},
		{
			name:    "raw HFSX volume",
			file:    "evidence.001",
			content: rawWithMagic(hfsMagicOffset, hfsxMagic),
			want:    FormatRaw,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectFormat(writeImage(t, tt.file, tt.content))
			if err != nil {
				t.Fatalf("DetectFormat() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("DetectFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectFormatUnknown(t *testing.T) {
	// Large enough to be an image, but carrying no recognisable signature.
	path := writeImage(t, "garbage.bin", make([]byte, headerSize+udifTrailerSize))

	got, err := DetectFormat(path)
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("DetectFormat() error = %v, want ErrUnknownFormat", err)
	}
	if got != FormatUnknown {
		t.Errorf("DetectFormat() = %q, want %q", got, FormatUnknown)
	}
}

func TestDetectFormatTooSmall(t *testing.T) {
	for _, size := range []int{0, 1, minImageSize - 1} {
		path := writeImage(t, "tiny.bin", make([]byte, size))

		got, err := DetectFormat(path)
		if !errors.Is(err, ErrImageTooSmall) {
			t.Errorf("DetectFormat(%d bytes) error = %v, want ErrImageTooSmall", size, err)
		}
		if got != FormatUnknown {
			t.Errorf("DetectFormat(%d bytes) = %q, want %q", size, got, FormatUnknown)
		}
	}
}

func TestDetectFormatMissingFile(t *testing.T) {
	got, err := DetectFormat(filepath.Join(t.TempDir(), "absent.dmg"))
	if err == nil {
		t.Fatal("DetectFormat() error = nil, want error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("DetectFormat() error = %v, want os.ErrNotExist", err)
	}
	if got != FormatUnknown {
		t.Errorf("DetectFormat() = %q, want %q", got, FormatUnknown)
	}
}

func TestDetectFormatDirectory(t *testing.T) {
	got, err := DetectFormat(t.TempDir())
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("DetectFormat(dir) error = %v, want ErrUnknownFormat", err)
	}
	if got != FormatUnknown {
		t.Errorf("DetectFormat(dir) = %q, want %q", got, FormatUnknown)
	}
}

// TestDetectFormatPrefersContainerOverFilesystem guards the detection order: an
// E01 whose payload happens to contain filesystem bytes must still read as E01.
func TestDetectFormatPrefersContainerOverFilesystem(t *testing.T) {
	buf := rawWithMagic(apfsMagicOffset, apfsMagic)
	copy(buf, ewfMagic)

	got, err := DetectFormat(writeImage(t, "ambiguous.E01", buf))
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if got != FormatE01 {
		t.Errorf("DetectFormat() = %q, want %q", got, FormatE01)
	}
}

func TestFormatString(t *testing.T) {
	tests := map[Format]string{
		FormatDMG:     "dmg",
		FormatE01:     "e01",
		FormatRaw:     "raw",
		FormatUnknown: "unknown",
	}
	for f, want := range tests {
		if got := f.String(); got != want {
			t.Errorf("Format(%q).String() = %q, want %q", string(f), got, want)
		}
	}
}

func TestFormatRequiresEWFMount(t *testing.T) {
	tests := map[Format]bool{
		FormatE01:     true,
		FormatDMG:     false,
		FormatRaw:     false,
		FormatUnknown: false,
	}
	for f, want := range tests {
		if got := f.RequiresEWFMount(); got != want {
			t.Errorf("Format(%q).RequiresEWFMount() = %v, want %v", string(f), got, want)
		}
	}
}
