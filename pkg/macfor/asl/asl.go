package asl

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
)

// ASL file format constants.
const (
	aslMagic   = "ASL DB\x00\x00\x00\x00\x00\x00" // 12-byte magic.
	headerSize = 80                               // File header size in bytes.
	// inlineHighBit indicates an inline string when set in a string reference.
	inlineHighBit = uint64(1) << 63
	// maxStringLen is the maximum allowed cached string length.
	maxStringLen = 1 << 20
)

// fileHeader represents the ASL file header (80 bytes).
type fileHeader struct {
	Version         uint32
	FirstRecordOff  uint64
	FirstRecordTime uint64
	StringCacheSize uint32
	LastRecordOff   uint64
}

// rawRecord holds the raw binary field values before conversion.
type rawRecord struct {
	nextOff    uint64
	messageID  uint64
	tsSec      uint64
	tsNano     uint32
	level      uint16
	flags      uint16
	pid        uint32
	uid        uint32
	gid        uint32
	readUID    uint32
	readGID    uint32
	refPID     uint64
	kvCount    uint32
	stringRefs [6]uint64 // host, sender, facility, message, refProcess, session.
}

// parseHeader reads and validates the ASL file header.
func parseHeader(r io.ReadSeeker) (*fileHeader, error) {
	magic := make([]byte, 12)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, fmt.Errorf("reading magic: %w", err)
	}
	if string(magic) != aslMagic {
		return nil, fmt.Errorf("invalid ASL magic: %q", magic)
	}

	var h fileHeader
	if err := binary.Read(r, binary.BigEndian, &h.Version); err != nil {
		return nil, fmt.Errorf("reading version: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &h.FirstRecordOff); err != nil {
		return nil, fmt.Errorf("reading first record offset: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &h.FirstRecordTime); err != nil {
		return nil, fmt.Errorf("reading first record time: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &h.StringCacheSize); err != nil {
		return nil, fmt.Errorf("reading string cache size: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &h.LastRecordOff); err != nil {
		return nil, fmt.Errorf("reading last record offset: %w", err)
	}

	// Skip remaining reserved bytes (36 bytes to reach 80 total).
	const reservedSize = headerSize - 12 - 4 - 8 - 8 - 4 - 8 // = 36.
	if _, err := io.CopyN(io.Discard, r, reservedSize); err != nil {
		return nil, fmt.Errorf("skipping reserved header bytes: %w", err)
	}

	return &h, nil
}

// parseRecord reads a single ASL record at the given file offset.
func parseRecord(r io.ReadSeeker, offset uint64) (*Record, uint64, error) {
	if _, err := r.Seek(int64(offset), io.SeekStart); err != nil { //nolint:gosec // ASL offsets are within file bounds.
		return nil, 0, fmt.Errorf("seeking to record at offset %d: %w", offset, err)
	}

	raw, err := readRawRecord(r)
	if err != nil {
		return nil, 0, err
	}

	kvRefs, err := readKVRefs(r, raw.kvCount)
	if err != nil {
		return nil, 0, err
	}

	rec, err := resolveRecord(r, raw, kvRefs)
	if err != nil {
		return nil, 0, err
	}

	return rec, raw.nextOff, nil
}

// readRawRecord reads the fixed-size fields of an ASL record.
func readRawRecord(r io.Reader) (*rawRecord, error) {
	var raw rawRecord

	// Record length (2-byte high + 4-byte low) — used for skipping, not needed here.
	var lengthHigh uint16
	var lengthLow uint32
	if err := binary.Read(r, binary.BigEndian, &lengthHigh); err != nil {
		return nil, fmt.Errorf("reading record length high: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &lengthLow); err != nil {
		return nil, fmt.Errorf("reading record length low: %w", err)
	}

	// Sequential fixed fields.
	fields := []any{
		&raw.nextOff, &raw.messageID, &raw.tsSec, &raw.tsNano,
		&raw.level, &raw.flags, &raw.pid, &raw.uid, &raw.gid,
		&raw.readUID, &raw.readGID, &raw.refPID, &raw.kvCount,
	}
	for _, f := range fields {
		if err := binary.Read(r, binary.BigEndian, f); err != nil {
			return nil, fmt.Errorf("reading record field: %w", err)
		}
	}

	// String references.
	for i := range raw.stringRefs {
		if err := binary.Read(r, binary.BigEndian, &raw.stringRefs[i]); err != nil {
			return nil, fmt.Errorf("reading string reference %d: %w", i, err)
		}
	}

	return &raw, nil
}

// readKVRefs reads key-value pair references from a record.
func readKVRefs(r io.Reader, count uint32) ([][2]uint64, error) {
	kvRefs := make([][2]uint64, count)
	for i := uint32(0); i < count; i++ {
		if err := binary.Read(r, binary.BigEndian, &kvRefs[i][0]); err != nil {
			return nil, fmt.Errorf("reading key reference %d: %w", i, err)
		}
		if err := binary.Read(r, binary.BigEndian, &kvRefs[i][1]); err != nil {
			return nil, fmt.Errorf("reading value reference %d: %w", i, err)
		}
	}
	return kvRefs, nil
}

// resolveRecord converts raw binary fields and string references into a Record.
func resolveRecord(r io.ReadSeeker, raw *rawRecord, kvRefs [][2]uint64) (*Record, error) {
	// Resolve standard string references.
	names := []string{"host", "sender", "facility", "message", "ref process", "session"}
	strs := make([]string, 6)
	for i, ref := range raw.stringRefs {
		s, err := resolveString(r, ref)
		if err != nil {
			return nil, fmt.Errorf("resolving %s: %w", names[i], err)
		}
		strs[i] = s
	}

	// Resolve key-value pairs.
	var kvMap map[string]string
	if len(kvRefs) > 0 {
		kvMap = make(map[string]string, len(kvRefs))
		for i, kv := range kvRefs {
			key, err := resolveString(r, kv[0])
			if err != nil {
				return nil, fmt.Errorf("resolving key %d: %w", i, err)
			}
			val, err := resolveString(r, kv[1])
			if err != nil {
				return nil, fmt.Errorf("resolving value %d: %w", i, err)
			}
			if key != "" {
				kvMap[key] = val
			}
		}
	}

	rec := &Record{
		MessageID:  raw.messageID,
		Timestamp:  time.Unix(int64(raw.tsSec), int64(raw.tsNano)), //nolint:gosec // ASL timestamps are within int64 range.
		Level:      int(raw.level),
		Flags:      raw.flags,
		PID:        int(raw.pid),
		UID:        int(raw.uid),
		GID:        int(raw.gid),
		ReadUID:    int(raw.readUID),
		ReadGID:    int(raw.readGID),
		RefPID:     int(raw.refPID), //nolint:gosec // RefPID values are within int range.
		Host:       strs[0],
		Sender:     strs[1],
		Facility:   strs[2],
		Message:    strs[3],
		RefProcess: strs[4],
		Session:    strs[5],
		KeyValues:  kvMap,
	}

	return rec, nil
}

// resolveString decodes an ASL string reference.
// If the high bit is set, the lower 7 bytes are an inline ASCII string.
// Otherwise, the value is a file offset to a length-prefixed string.
func resolveString(r io.ReadSeeker, ref uint64) (string, error) {
	if ref == 0 {
		return "", nil
	}

	// Check for inline string (high bit set).
	if ref&inlineHighBit != 0 {
		return decodeInlineString(ref), nil
	}

	return readCachedString(r, ref)
}

// decodeInlineString extracts a null-terminated string from the lower 7 bytes of a reference.
func decodeInlineString(ref uint64) string {
	var buf [7]byte
	val := ref &^ inlineHighBit
	for i := 6; i >= 0; i-- {
		buf[i] = byte(val & 0xFF)
		val >>= 8
	}
	s := string(buf[:])
	if idx := strings.IndexByte(s, 0); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// readCachedString reads a length-prefixed string from a file offset.
func readCachedString(r io.ReadSeeker, offset uint64) (string, error) {
	if _, err := r.Seek(int64(offset), io.SeekStart); err != nil { //nolint:gosec // String offsets are within file bounds.
		return "", fmt.Errorf("seeking to string at offset %d: %w", offset, err)
	}

	var unused uint16
	if err := binary.Read(r, binary.BigEndian, &unused); err != nil {
		return "", fmt.Errorf("reading string header: %w", err)
	}

	var strLen uint32
	if err := binary.Read(r, binary.BigEndian, &strLen); err != nil {
		return "", fmt.Errorf("reading string length: %w", err)
	}

	if strLen == 0 {
		return "", nil
	}

	if strLen > maxStringLen {
		return "", fmt.Errorf("string length %d exceeds maximum", strLen)
	}

	buf := make([]byte, strLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("reading string data: %w", err)
	}

	s := string(buf)
	if idx := strings.IndexByte(s, 0); idx >= 0 {
		s = s[:idx]
	}

	return s, nil
}
