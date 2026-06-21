package safari

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/timestamp"
)

// Note: Tests use fileSource from history_test.go and mockCollector from safari_test.go.

func TestParseBinaryCookies(t *testing.T) {
	t.Run("valid cookies file", func(t *testing.T) {
		data := mustLoadFixture(FixtureCookiesBinary)
		cookies, err := parseBinaryCookies(data)
		if err != nil {
			t.Fatalf("parseBinaryCookies() error = %v", err)
		}

		if len(cookies) != 3 {
			t.Errorf("expected 3 cookies, got %d", len(cookies))
		}

		// Verify first cookie (github.com).
		github := findCookieByDomain(cookies, ".github.com")
		if github == nil {
			t.Fatal("expected to find .github.com cookie")
		}
		if github.Name != "_gh_sess" {
			t.Errorf("github cookie name = %q, want %q", github.Name, "_gh_sess")
		}
		if github.Path != "/" {
			t.Errorf("github cookie path = %q, want %q", github.Path, "/")
		}
		if !github.IsSecure() {
			t.Error("github cookie should have Secure flag")
		}
		if !github.IsHTTPOnly() {
			t.Error("github cookie should have HttpOnly flag")
		}

		// Verify second cookie (google.com).
		google := findCookieByDomain(cookies, ".google.com")
		if google == nil {
			t.Fatal("expected to find .google.com cookie")
		}
		if google.Name != "NID" {
			t.Errorf("google cookie name = %q, want %q", google.Name, "NID")
		}
		if !google.IsSecure() {
			t.Error("google cookie should have Secure flag")
		}
		if google.IsHTTPOnly() {
			t.Error("google cookie should not have HttpOnly flag")
		}

		// Verify third cookie (stackoverflow.com).
		stackoverflow := findCookieByDomain(cookies, "stackoverflow.com")
		if stackoverflow == nil {
			t.Fatal("expected to find stackoverflow.com cookie")
		}
		if stackoverflow.Name != "prov" {
			t.Errorf("stackoverflow cookie name = %q, want %q", stackoverflow.Name, "prov")
		}
		if stackoverflow.IsSecure() {
			t.Error("stackoverflow cookie should not have Secure flag")
		}
		if stackoverflow.IsHTTPOnly() {
			t.Error("stackoverflow cookie should not have HttpOnly flag")
		}
	})

	t.Run("empty cookies file", func(t *testing.T) {
		data := mustLoadFixture(FixtureCookiesBinaryEmpty)
		cookies, err := parseBinaryCookies(data)
		if err != nil {
			t.Fatalf("parseBinaryCookies() error = %v", err)
		}

		if len(cookies) != 0 {
			t.Errorf("expected 0 cookies, got %d", len(cookies))
		}
	})

	t.Run("invalid magic header", func(t *testing.T) {
		data := []byte("notcook")
		_, err := parseBinaryCookies(data)
		if err == nil || err.Error() != ErrInvalidCookieMagic.Error() {
			t.Errorf("expected ErrInvalidCookieMagic, got %v", err)
		}
	})

	t.Run("truncated header", func(t *testing.T) {
		data := []byte("cook")
		_, err := parseBinaryCookies(data)
		if err == nil || err.Error() != ErrInvalidCookieMagic.Error() {
			t.Errorf("expected ErrInvalidCookieMagic for truncated header, got %v", err)
		}
	})
}

func TestParsedCookieFlags(t *testing.T) {
	tests := []struct {
		name     string
		flags    uint32
		secure   bool
		httpOnly bool
	}{
		{"no flags", 0x0000, false, false},
		{"secure only", 0x0001, true, false},
		{"httponly only", 0x0004, false, true},
		{"both flags", 0x0005, true, true},
		{"secure with other bits", 0x0011, true, false},
		{"httponly with other bits", 0x0014, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cookie := parsedCookie{Flags: tt.flags}

			if got := cookie.IsSecure(); got != tt.secure {
				t.Errorf("IsSecure() = %v, want %v", got, tt.secure)
			}

			if got := cookie.IsHTTPOnly(); got != tt.httpOnly {
				t.Errorf("IsHTTPOnly() = %v, want %v", got, tt.httpOnly)
			}
		})
	}
}

func TestConvertCookie(t *testing.T) {
	creation := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	expiry := time.Date(2024, 2, 15, 10, 0, 0, 0, time.UTC)

	cookie := parsedCookie{
		Domain:   ".example.com",
		Name:     "session",
		Path:     "/app",
		Value:    "secret_session_token_12345",
		Expiry:   expiry,
		Creation: creation,
		Flags:    cookieFlagSecure | cookieFlagHTTPOnly,
	}

	record := convertCookie(cookie, "testuser", "/Users/testuser/Library/Cookies/Cookies.binarycookies")

	if record.Type != "browser_cookie" {
		t.Errorf("Type = %q, want %q", record.Type, "browser_cookie")
	}
	if record.Browser != testBrowser {
		t.Errorf("Browser = %q, want %q", record.Browser, testBrowser)
	}
	if record.User != "testuser" {
		t.Errorf("User = %q, want %q", record.User, "testuser")
	}
	if record.Domain != ".example.com" {
		t.Errorf("Domain = %q, want %q", record.Domain, ".example.com")
	}
	if record.Name != "session" {
		t.Errorf("Name = %q, want %q", record.Name, "session")
	}
	if record.Path != "/app" {
		t.Errorf("Path = %q, want %q", record.Path, "/app")
	}
	if record.ValuePreview != "[REDACTED - 26 chars]" {
		t.Errorf("ValuePreview = %q, want %q", record.ValuePreview, "[REDACTED - 26 chars]")
	}
	if record.ValueLength != 26 {
		t.Errorf("ValueLength = %d, want %d", record.ValueLength, 26)
	}
	if !record.CreationTime.Equal(creation) {
		t.Errorf("CreationTime = %v, want %v", record.CreationTime, creation)
	}
	if !record.ExpiryTime.Equal(expiry) {
		t.Errorf("ExpiryTime = %v, want %v", record.ExpiryTime, expiry)
	}
	if !record.Secure {
		t.Error("Secure should be true")
	}
	if !record.HTTPOnly {
		t.Error("HTTPOnly should be true")
	}
}

func TestConvertCookieRedaction(t *testing.T) {
	t.Run("empty value", func(t *testing.T) {
		cookie := parsedCookie{Value: ""}
		record := convertCookie(cookie, "user", "/path")
		if record.ValuePreview != "[REDACTED - 0 chars]" {
			t.Errorf("ValuePreview = %q, want %q", record.ValuePreview, "[REDACTED - 0 chars]")
		}
		if record.ValueLength != 0 {
			t.Errorf("ValueLength = %d, want 0", record.ValueLength)
		}
	})

	t.Run("long value", func(t *testing.T) {
		longValue := "this_is_a_very_long_cookie_value_that_should_be_redacted_completely"
		cookie := parsedCookie{Value: longValue}
		record := convertCookie(cookie, "user", "/path")
		expectedLen := len(longValue)
		expectedPreview := fmt.Sprintf("[REDACTED - %d chars]", expectedLen)
		if record.ValuePreview != expectedPreview {
			t.Errorf("ValuePreview = %q, want %q", record.ValuePreview, expectedPreview)
		}
		if record.ValueLength != expectedLen {
			t.Errorf("ValueLength = %d, want %d", record.ValueLength, expectedLen)
		}
	})
}

func TestReadNullTerminatedString(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		offset uint32
		want   string
	}{
		{
			name:   "simple string",
			data:   []byte("hello\x00world"),
			offset: 0,
			want:   "hello",
		},
		{
			name:   "offset to middle",
			data:   []byte("hello\x00world\x00"),
			offset: 6,
			want:   "world",
		},
		{
			name:   "offset beyond data",
			data:   []byte("hello"),
			offset: 10,
			want:   "",
		},
		{
			name:   "no null terminator",
			data:   []byte("hello"),
			offset: 0,
			want:   "hello",
		},
		{
			name:   "empty string",
			data:   []byte("\x00after"),
			offset: 0,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readNullTerminatedString(tt.data, tt.offset)
			if got != tt.want {
				t.Errorf("readNullTerminatedString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollectCookiesForUser(t *testing.T) {
	// Skip if fixtures don't exist.
	if !fixtureExists(FixtureCookiesBinary) {
		t.Skip("Cookies.binarycookies fixture not found")
	}

	t.Run("collect from fixture", func(t *testing.T) {
		// Create a file source with fixture data.
		tempDir := t.TempDir()
		userHome := tempDir
		cookiesDir := filepath.Join(userHome, "Library", "Cookies")
		if err := os.MkdirAll(cookiesDir, 0750); err != nil {
			t.Fatalf("creating test directory: %v", err)
		}

		// Copy fixture to expected location.
		fixtureData := mustLoadFixture(FixtureCookiesBinary)
		cookiesPath := filepath.Join(cookiesDir, "Cookies.binarycookies")
		if err := os.WriteFile(cookiesPath, fixtureData, 0600); err != nil {
			t.Fatalf("writing test cookies file: %v", err)
		}

		// Create file source.
		src := newFileSource(tempDir)
		out := newMockCollector()

		user := macfor.User{
			Username: testUsername,
			HomeDir:  userHome,
		}

		err := collectCookiesForUser(context.Background(), user, src, out)
		if err != nil {
			t.Fatalf("collectCookiesForUser() error = %v", err)
		}

		// Verify file was added.
		if len(out.files) != 1 {
			t.Errorf("expected 1 file, got %d", len(out.files))
		}

		// Verify records were added.
		if len(out.records) != 3 {
			t.Errorf("expected 3 records, got %d", len(out.records))
		}

		// Check that records are CookieRecords.
		for i, rec := range out.records {
			cr, ok := rec.record.(*CookieRecord)
			if !ok {
				t.Errorf("record %d is not a *CookieRecord", i)
				continue
			}
			if cr.Type != "browser_cookie" {
				t.Errorf("record %d Type = %q, want %q", i, cr.Type, "browser_cookie")
			}
			if cr.Browser != "safari" {
				t.Errorf("record %d Browser = %q, want %q", i, cr.Browser, "safari")
			}
			if cr.User != testUsername {
				t.Errorf("record %d User = %q, want %q", i, cr.User, testUsername)
			}
		}
	})

	t.Run("missing file", func(t *testing.T) {
		tempDir := t.TempDir()
		userHome := tempDir
		if err := os.MkdirAll(userHome, 0750); err != nil {
			t.Fatalf("creating test directory: %v", err)
		}

		src := newFileSource(tempDir)
		out := newMockCollector()

		user := macfor.User{
			Username: testUsername,
			HomeDir:  userHome,
		}

		err := collectCookiesForUser(context.Background(), user, src, out)
		if err != nil {
			t.Fatalf("collectCookiesForUser() error = %v", err)
		}

		// Should not fail, just skip.
		if len(out.files) != 0 {
			t.Errorf("expected 0 files for missing file, got %d", len(out.files))
		}
		if len(out.records) != 0 {
			t.Errorf("expected 0 records for missing file, got %d", len(out.records))
		}
	})
}

func TestParseCookieRecordTimestamps(t *testing.T) {
	// Create a minimal valid cookie record for testing timestamp parsing.
	baseTime := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	expiryTime := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	// Build a minimal cookie record.
	var buf bytes.Buffer

	// Record size (will update later).
	sizePlaceholder := buf.Len()
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))

	// Flags.
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0x0005)) // Secure + HttpOnly

	// String offsets (relative to record start).
	urlOffset := uint32(48)   // After fixed header.
	nameOffset := uint32(60)  // domain (11 bytes) + null.
	pathOffset := uint32(65)  // name (4 bytes) + null.
	valueOffset := uint32(67) // path (1 byte) + null.

	_ = binary.Write(&buf, binary.LittleEndian, urlOffset)
	_ = binary.Write(&buf, binary.LittleEndian, nameOffset)
	_ = binary.Write(&buf, binary.LittleEndian, pathOffset)
	_ = binary.Write(&buf, binary.LittleEndian, valueOffset)

	// End header marker (8 bytes).
	_ = binary.Write(&buf, binary.LittleEndian, uint64(0))

	// Expiry time.
	expiryFloat := timestamp.TimeToCoreData(expiryTime)
	_ = binary.Write(&buf, binary.LittleEndian, math.Float64bits(expiryFloat))

	// Creation time.
	creationFloat := timestamp.TimeToCoreData(baseTime)
	_ = binary.Write(&buf, binary.LittleEndian, math.Float64bits(creationFloat))

	// Strings.
	buf.WriteString("example.com")
	buf.WriteByte(0)
	buf.WriteString("test")
	buf.WriteByte(0)
	buf.WriteString("/")
	buf.WriteByte(0)
	buf.WriteString("value123")
	buf.WriteByte(0)

	// Update record size.
	data := buf.Bytes()
	binary.LittleEndian.PutUint32(data[sizePlaceholder:], uint32(len(data))) //nolint:gosec // Test code with known small buffer

	cookie, err := parseCookieRecord(data)
	if err != nil {
		t.Fatalf("parseCookieRecord() error = %v", err)
	}

	// Check timestamps are correctly converted.
	if !cookie.Creation.Equal(baseTime) {
		t.Errorf("Creation = %v, want %v", cookie.Creation, baseTime)
	}

	// Allow 1 second difference due to floating-point precision.
	if cookie.Expiry.Sub(expiryTime).Abs() > time.Second {
		t.Errorf("Expiry = %v, want %v", cookie.Expiry, expiryTime)
	}

	// Check other fields.
	if cookie.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", cookie.Domain, "example.com")
	}
	if cookie.Name != "test" {
		t.Errorf("Name = %q, want %q", cookie.Name, "test")
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want %q", cookie.Path, "/")
	}
	if cookie.Value != "value123" {
		t.Errorf("Value = %q, want %q", cookie.Value, "value123")
	}
	if !cookie.IsSecure() || !cookie.IsHTTPOnly() {
		t.Error("expected Secure and HttpOnly flags to be set")
	}
}

// findCookieByDomain finds a cookie by its domain in the list.
func findCookieByDomain(cookies []parsedCookie, domain string) *parsedCookie {
	for i := range cookies {
		if cookies[i].Domain == domain {
			return &cookies[i]
		}
	}
	return nil
}
