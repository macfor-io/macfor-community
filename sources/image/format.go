// Package image provides a Source implementation that collects from a disk image
// file. Unlike the directory source, which requires the investigator to mount an
// image beforehand, this package attaches the image read-only itself, serves
// collection from the resulting mount point, and detaches when collection ends.
//
// DMG and raw images are attached with hdiutil, which ships with macOS. EnCase
// E01/Ex01 images are first exposed as a raw device by the external ewfmount
// binary (libewf); ewfmount is discovered on PATH at runtime, so E01 support is an
// optional runtime prerequisite rather than a build-time dependency and the
// collector stays CGO-free.
package image

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// Format identifies the container format of a disk image file.
type Format string

const (
	// FormatDMG is an Apple UDIF disk image, identified by its koly trailer.
	FormatDMG Format = "dmg"

	// FormatE01 is an EnCase/EWF image (E01 or Ex01), possibly segmented.
	FormatE01 Format = "e01"

	// FormatRaw is an unwrapped block-for-block image (dd, img, raw, 001).
	FormatRaw Format = "raw"

	// FormatUnknown is returned alongside an error when no format matches.
	FormatUnknown Format = "unknown"
)

// ErrUnknownFormat indicates the file is not a disk image macfor can attach.
var ErrUnknownFormat = errors.New("unrecognised disk image format")

// ErrImageTooSmall indicates the file is too small to be any supported image.
var ErrImageTooSmall = errors.New("file is too small to be a disk image")

var (
	// ewfMagic is the 8-byte signature at offset 0 of an EnCase E01 segment.
	ewfMagic = []byte{'E', 'V', 'F', 0x09, 0x0D, 0x0A, 0xFF, 0x00}

	// ewf2Magic is the 8-byte signature at offset 0 of an EnCase Ex01 segment.
	ewf2Magic = []byte{'E', 'V', 'F', '2', 0x0D, 0x0A, 0x81, 0x00}

	// kolyMagic opens the 512-byte UDIF trailer at the end of a DMG.
	kolyMagic = []byte("koly")

	// apfsMagic is the APFS container superblock signature.
	apfsMagic = []byte("NXSB")

	// hfsPlusMagic and hfsxMagic are the HFS+ and HFSX volume header signatures.
	hfsPlusMagic = []byte("H+")
	hfsxMagic    = []byte("HX")

	// gptMagic is the GUID Partition Table header signature, found in the
	// second logical block of a whole-disk image.
	gptMagic = []byte("EFI PART")
)

const (
	// udifTrailerSize is the size of the koly trailer at the end of a DMG.
	udifTrailerSize = 512

	// apfsMagicOffset is the offset of NXSB within the APFS container superblock.
	apfsMagicOffset = 0x20

	// hfsMagicOffset is the offset of the HFS+/HFSX volume header signature.
	hfsMagicOffset = 0x400

	// gptOffset512 and gptOffset4K are where the GPT header sits on a disk
	// with 512-byte and 4096-byte logical blocks.
	gptOffset512 = 0x200
	gptOffset4K  = 0x1000

	// headerSize is how much of the file start we need to test every signature.
	headerSize = gptOffset4K + 8

	// minImageSize is the smallest file that could carry any supported signature.
	minImageSize = udifTrailerSize
)

// String returns the format's lowercase identifier, as recorded in the evidence
// manifest.
func (f Format) String() string {
	return string(f)
}

// RequiresEWFMount reports whether attaching this format needs the external
// ewfmount binary to expose the image as a raw device first.
func (f Format) RequiresEWFMount() bool {
	return f == FormatE01
}

// DetectFormat identifies the container format of the disk image at path by
// examining its content rather than its file extension, which is frequently
// wrong on forensic images. For a segmented E01 set, pass the first segment.
//
// It returns FormatUnknown wrapped with ErrUnknownFormat when no signature
// matches, and reads only the handful of bytes needed to decide.
func DetectFormat(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown, fmt.Errorf("open image: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return FormatUnknown, fmt.Errorf("stat image: %w", err)
	}
	if info.IsDir() {
		return FormatUnknown, fmt.Errorf("%w: %s is a directory", ErrUnknownFormat, path)
	}

	size := info.Size()
	if size < minImageSize {
		return FormatUnknown, fmt.Errorf("%w: %s is %d bytes", ErrImageTooSmall, path, size)
	}

	header, err := readHeader(f, size)
	if err != nil {
		return FormatUnknown, err
	}

	// EWF signatures sit at offset 0 and are the cheapest to test.
	if bytes.HasPrefix(header, ewfMagic) || bytes.HasPrefix(header, ewf2Magic) {
		return FormatE01, nil
	}

	// A DMG is identified by its trailer, so this needs a seek to the end.
	isDMG, err := hasKolyTrailer(f, size)
	if err != nil {
		return FormatUnknown, err
	}
	if isDMG {
		return FormatDMG, nil
	}

	// A raw image has no container wrapper, so look for a partition table (a
	// whole-disk image, as dd of a Mac's disk produces) or a filesystem (an
	// image of a single volume) instead.
	if hasGPTHeader(header) || hasFilesystemMagic(header) {
		return FormatRaw, nil
	}

	return FormatUnknown, fmt.Errorf("%w: %s", ErrUnknownFormat, path)
}

// readHeader reads the leading bytes needed for signature tests, returning fewer
// than headerSize bytes when the file is shorter.
func readHeader(r io.ReaderAt, size int64) ([]byte, error) {
	n := int64(headerSize)
	if size < n {
		n = size
	}

	header := make([]byte, n)
	if _, err := r.ReadAt(header, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read image header: %w", err)
	}
	return header, nil
}

// hasKolyTrailer reports whether the file ends with a UDIF koly trailer.
func hasKolyTrailer(r io.ReaderAt, size int64) (bool, error) {
	trailer := make([]byte, udifTrailerSize)
	if _, err := r.ReadAt(trailer, size-udifTrailerSize); err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read image trailer: %w", err)
	}
	return bytes.HasPrefix(trailer, kolyMagic), nil
}

// hasGPTHeader reports whether the header carries a GUID Partition Table header
// at the second logical block, for either 512-byte or 4096-byte blocks.
func hasGPTHeader(header []byte) bool {
	return matchesAt(header, gptOffset512, gptMagic) || matchesAt(header, gptOffset4K, gptMagic)
}

// hasFilesystemMagic reports whether the header carries an APFS or HFS+/HFSX
// signature at its documented offset, which marks an unwrapped raw image.
func hasFilesystemMagic(header []byte) bool {
	if matchesAt(header, apfsMagicOffset, apfsMagic) {
		return true
	}
	return matchesAt(header, hfsMagicOffset, hfsPlusMagic) ||
		matchesAt(header, hfsMagicOffset, hfsxMagic)
}

// matchesAt reports whether want appears at offset in header, tolerating a
// header shorter than the offset.
func matchesAt(header []byte, offset int, want []byte) bool {
	end := offset + len(want)
	if len(header) < end {
		return false
	}
	return bytes.Equal(header[offset:end], want)
}
