package asl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"
)

// buildASLFile creates a minimal valid ASL binary file for testing.
func buildASLFile(records []testRecord) []byte {
	var buf bytes.Buffer

	// Write magic (12 bytes).
	buf.WriteString("ASL DB\x00\x00\x00\x00\x00\x00")

	// Version.
	writeU32(&buf, 2)

	// Placeholder offsets — will be filled in after we know record positions.
	firstRecordOffPos := buf.Len()
	writeU64(&buf, 0) // FirstRecordOff.
	writeU64(&buf, 0) // FirstRecordTime.

	// String cache size.
	writeU32(&buf, 0)

	lastRecordOffPos := buf.Len()
	writeU64(&buf, 0) // LastRecordOff.

	// Reserved bytes to reach 80 total.
	buf.Write(make([]byte, 36))

	if len(records) == 0 {
		return buf.Bytes()
	}

	// Write cached strings first, collecting their offsets.
	stringOffsets := make(map[string]uint64)
	for _, rec := range records {
		for _, s := range rec.cachedStrings {
			if _, exists := stringOffsets[s]; exists {
				continue
			}
			stringOffsets[s] = uint64(buf.Len()) //nolint:gosec // Test buffer length is small.
			writeU16(&buf, 0)
			writeU32(&buf, uint32(len(s)+1)) //nolint:gosec // Test string length is small.
			buf.WriteString(s)
			buf.WriteByte(0)
		}
	}

	// Build and write records.
	var recordOffsets []uint64
	for _, rec := range records {
		recordOffsets = append(recordOffsets, uint64(buf.Len())) //nolint:gosec // Test buffer length is small.

		var recBuf bytes.Buffer

		// Next offset placeholder (will be patched).
		writeU64(&recBuf, 0)

		// Message ID.
		writeU64(&recBuf, rec.messageID)

		// Timestamp.
		writeU64(&recBuf, uint64(rec.timestamp.Unix()))       //nolint:gosec // Test timestamps are positive.
		writeU32(&recBuf, uint32(rec.timestamp.Nanosecond())) //nolint:gosec // Nanoseconds fit in uint32.

		// Level, flags, PID, UID, GID, ReadUID, ReadGID.
		writeU16(&recBuf, rec.level)
		writeU16(&recBuf, rec.flags)
		writeU32(&recBuf, rec.pid)
		writeU32(&recBuf, rec.uid)
		writeU32(&recBuf, rec.gid)
		writeU32(&recBuf, rec.readUID)
		writeU32(&recBuf, rec.readGID)

		// RefPID.
		writeU64(&recBuf, uint64(rec.refPID))

		// Key-value count.
		writeU32(&recBuf, uint32(len(rec.keyValues))) //nolint:gosec // Test KV count is small.

		// String references.
		writeStringRef(&recBuf, rec.host, stringOffsets)
		writeStringRef(&recBuf, rec.sender, stringOffsets)
		writeStringRef(&recBuf, rec.facility, stringOffsets)
		writeStringRef(&recBuf, rec.message, stringOffsets)
		writeStringRef(&recBuf, rec.refProcess, stringOffsets)
		writeStringRef(&recBuf, rec.session, stringOffsets)

		// Key-value pairs.
		for _, kv := range rec.keyValues {
			writeStringRef(&recBuf, kv[0], stringOffsets)
			writeStringRef(&recBuf, kv[1], stringOffsets)
		}

		// Calculate record length (recBuf length + 6 for the length fields).
		recLen := uint64(recBuf.Len() + 6) //nolint:gosec // Test record length is small.

		// Write length (2-byte high + 4-byte low).
		writeU16(&buf, uint16(recLen>>32)) //nolint:gosec // High bits of small record length.
		writeU32(&buf, uint32(recLen))     //nolint:gosec // Small record length fits.

		buf.Write(recBuf.Bytes())
	}

	// Patch next offsets in records.
	data := buf.Bytes()
	for i, off := range recordOffsets {
		nextOff := uint64(0)
		if i+1 < len(recordOffsets) {
			nextOff = recordOffsets[i+1]
		}
		binary.BigEndian.PutUint64(data[off+6:off+14], nextOff)
	}

	// Patch header.
	binary.BigEndian.PutUint64(data[firstRecordOffPos:firstRecordOffPos+8], recordOffsets[0])
	binary.BigEndian.PutUint64(data[firstRecordOffPos+8:firstRecordOffPos+16],
		uint64(records[0].timestamp.Unix())) //nolint:gosec // Test timestamps are positive.
	binary.BigEndian.PutUint64(data[lastRecordOffPos:lastRecordOffPos+8], recordOffsets[len(recordOffsets)-1])

	return data
}

// writeU16 writes a big-endian uint16 to a buffer.
func writeU16(buf *bytes.Buffer, v uint16) {
	_ = binary.Write(buf, binary.BigEndian, v)
}

// writeU32 writes a big-endian uint32 to a buffer.
func writeU32(buf *bytes.Buffer, v uint32) {
	_ = binary.Write(buf, binary.BigEndian, v)
}

// writeU64 writes a big-endian uint64 to a buffer.
func writeU64(buf *bytes.Buffer, v uint64) {
	_ = binary.Write(buf, binary.BigEndian, v)
}

// writeStringRef writes a string reference to a buffer.
// Short strings (<=7 bytes) are written inline; longer strings use cached offsets.
func writeStringRef(buf *bytes.Buffer, s string, offsets map[string]uint64) {
	if s == "" {
		writeU64(buf, 0)
		return
	}

	// Check for inline (<=7 bytes).
	if len(s) <= 7 {
		val := inlineHighBit
		for i := 0; i < len(s); i++ {
			shift := uint(8 * (6 - i)) //nolint:gosec // i is always 0-6.
			val |= uint64(s[i]) << shift
		}
		writeU64(buf, val)
		return
	}

	// Use cached offset.
	off, ok := offsets[s]
	if !ok {
		writeU64(buf, 0)
		return
	}
	writeU64(buf, off)
}

// testRecord defines test record data for fixture building.
type testRecord struct {
	messageID     uint64
	timestamp     time.Time
	level         uint16
	flags         uint16
	pid           uint32
	uid           uint32
	gid           uint32
	readUID       uint32
	readGID       uint32
	refPID        uint32
	host          string
	sender        string
	facility      string
	message       string
	refProcess    string
	session       string
	keyValues     [][2]string
	cachedStrings []string
}

func TestNewReader_ValidFile(t *testing.T) {
	ts := time.Date(2026, 2, 20, 15, 30, 0, 0, time.UTC)
	data := buildASLFile([]testRecord{
		{
			messageID: 1,
			timestamp: ts,
			level:     LevelNotice,
			pid:       1234,
			readGID:   80,
			host:      "mac",
			sender:    "test",
			facility:  "testfac",
			message:   "hello",
		},
	})

	r, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if r.header.Version != 2 {
		t.Errorf("version = %d, want 2", r.header.Version)
	}
}

func TestNewReader_InvalidMagic(t *testing.T) {
	data := []byte("NOT ASL DB\x00\x00")
	data = append(data, make([]byte, 80)...)
	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for invalid magic")
	}
}

func TestNewReader_EmptyFile(t *testing.T) {
	_, err := NewReader(bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestReadAll_SingleRecord(t *testing.T) {
	ts := time.Date(2026, 2, 20, 15, 30, 0, 0, time.UTC)
	data := buildASLFile([]testRecord{
		{
			messageID: 42,
			timestamp: ts,
			level:     LevelInfo,
			pid:       5678,
			uid:       501,
			gid:       20,
			readGID:   80,
			host:      "mac",
			sender:    "snd",
			facility:  "fac",
			message:   "msg",
		},
	})

	reader, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	rec := records[0]
	if rec.MessageID != 42 {
		t.Errorf("MessageID = %d, want 42", rec.MessageID)
	}
	if !rec.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", rec.Timestamp, ts)
	}
	if rec.Level != LevelInfo {
		t.Errorf("Level = %d, want %d", rec.Level, LevelInfo)
	}
	if rec.PID != 5678 {
		t.Errorf("PID = %d, want 5678", rec.PID)
	}
	if rec.UID != 501 {
		t.Errorf("UID = %d, want 501", rec.UID)
	}
	if rec.GID != 20 {
		t.Errorf("GID = %d, want 20", rec.GID)
	}
	if rec.ReadGID != 80 {
		t.Errorf("ReadGID = %d, want 80", rec.ReadGID)
	}
	if rec.Host != "mac" {
		t.Errorf("Host = %q, want %q", rec.Host, "mac")
	}
	if rec.Sender != "snd" {
		t.Errorf("Sender = %q, want %q", rec.Sender, "snd")
	}
}

func TestReadAll_MultipleRecords(t *testing.T) {
	ts1 := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC)
	ts3 := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)

	data := buildASLFile([]testRecord{
		{messageID: 1, timestamp: ts1, level: LevelInfo, host: "mac", sender: "a", message: "first"},
		{messageID: 2, timestamp: ts2, level: LevelWarning, host: "mac", sender: "b", message: "second"},
		{messageID: 3, timestamp: ts3, level: LevelError, host: "mac", sender: "c", message: "third"},
	})

	reader, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}

	if records[0].MessageID != 1 || records[1].MessageID != 2 || records[2].MessageID != 3 {
		t.Error("records not in expected order")
	}
	if records[0].Message != "first" || records[1].Message != "second" || records[2].Message != "third" {
		t.Error("message content mismatch")
	}
}

func TestNext_Iterator(t *testing.T) {
	ts := time.Date(2026, 2, 20, 15, 0, 0, 0, time.UTC)
	data := buildASLFile([]testRecord{
		{messageID: 1, timestamp: ts, host: "h", sender: "s"},
		{messageID: 2, timestamp: ts, host: "h", sender: "s"},
	})

	reader, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	rec1, err := reader.Next()
	if err != nil {
		t.Fatalf("Next 1: %v", err)
	}
	if rec1.MessageID != 1 {
		t.Errorf("rec1 MessageID = %d, want 1", rec1.MessageID)
	}

	rec2, err := reader.Next()
	if err != nil {
		t.Fatalf("Next 2: %v", err)
	}
	if rec2.MessageID != 2 {
		t.Errorf("rec2 MessageID = %d, want 2", rec2.MessageID)
	}

	_, err = reader.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestInlineStringDecoding(t *testing.T) {
	ts := time.Date(2026, 2, 20, 15, 0, 0, 0, time.UTC)
	data := buildASLFile([]testRecord{
		{
			messageID: 1,
			timestamp: ts,
			host:      "abc",
			sender:    "1234567",
			facility:  "x",
			message:   "hi",
		},
	})

	reader, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	if rec.Host != "abc" {
		t.Errorf("Host = %q, want %q", rec.Host, "abc")
	}
	if rec.Sender != "1234567" {
		t.Errorf("Sender = %q, want %q", rec.Sender, "1234567")
	}
	if rec.Facility != "x" {
		t.Errorf("Facility = %q, want %q", rec.Facility, "x")
	}
	if rec.Message != "hi" {
		t.Errorf("Message = %q, want %q", rec.Message, "hi")
	}
}

func TestCachedStringResolution(t *testing.T) {
	ts := time.Date(2026, 2, 20, 15, 0, 0, 0, time.UTC)
	longFacility := "com.apple.coretls.connection"
	longMessage := "TLS connection established successfully"

	data := buildASLFile([]testRecord{
		{
			messageID:     1,
			timestamp:     ts,
			host:          "mac",
			sender:        "test",
			facility:      longFacility,
			message:       longMessage,
			cachedStrings: []string{longFacility, longMessage},
		},
	})

	reader, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	if rec.Facility != longFacility {
		t.Errorf("Facility = %q, want %q", rec.Facility, longFacility)
	}
	if rec.Message != longMessage {
		t.Errorf("Message = %q, want %q", rec.Message, longMessage)
	}
}

func TestKeyValuePairs(t *testing.T) {
	ts := time.Date(2026, 2, 20, 15, 0, 0, 0, time.UTC)
	data := buildASLFile([]testRecord{
		{
			messageID: 1,
			timestamp: ts,
			host:      "mac",
			sender:    "test",
			keyValues: [][2]string{
				{"key1", "val1"},
				{"key2", "val2"},
			},
		},
	})

	reader, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	if len(rec.KeyValues) != 2 {
		t.Fatalf("got %d key-value pairs, want 2", len(rec.KeyValues))
	}
	if rec.KeyValues["key1"] != "val1" {
		t.Errorf("key1 = %q, want %q", rec.KeyValues["key1"], "val1")
	}
	if rec.KeyValues["key2"] != "val2" {
		t.Errorf("key2 = %q, want %q", rec.KeyValues["key2"], "val2")
	}
}

func TestEmptyFileNoRecords(t *testing.T) {
	data := buildASLFile(nil)

	reader, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("got %d records, want 0", len(records))
	}
}

func TestTruncatedFile(t *testing.T) {
	ts := time.Date(2026, 2, 20, 15, 0, 0, 0, time.UTC)
	data := buildASLFile([]testRecord{
		{messageID: 1, timestamp: ts, host: "mac", sender: "test", message: "hello"},
	})

	truncated := data[:headerSize+10]

	reader, err := NewReader(bytes.NewReader(truncated))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	_, err = reader.Next()
	if err == nil {
		t.Fatal("expected error for truncated record")
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level int
		want  string
	}{
		{LevelEmergency, "Emergency"},
		{LevelAlert, "Alert"},
		{LevelCritical, "Critical"},
		{LevelError, "Error"},
		{LevelWarning, "Warning"},
		{LevelNotice, "Notice"},
		{LevelInfo, "Info"},
		{LevelDebug, "Debug"},
		{99, "Unknown"},
	}
	for _, tt := range tests {
		got := LevelString(tt.level)
		if got != tt.want {
			t.Errorf("LevelString(%d) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestResolveString_Empty(t *testing.T) {
	r := bytes.NewReader(nil)
	s, err := resolveString(r, 0)
	if err != nil {
		t.Fatalf("resolveString(0): %v", err)
	}
	if s != "" {
		t.Errorf("resolveString(0) = %q, want %q", s, "")
	}
}

func TestTimestampNanoseconds(t *testing.T) {
	ts := time.Date(2026, 2, 20, 15, 30, 45, 123456789, time.UTC)
	data := buildASLFile([]testRecord{
		{messageID: 1, timestamp: ts, host: "mac"},
	})

	reader, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	rec, err := reader.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	if rec.Timestamp.Nanosecond() != ts.Nanosecond() {
		t.Errorf("Nanosecond = %d, want %d", rec.Timestamp.Nanosecond(), ts.Nanosecond())
	}
}
