package safari

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/timestamp"
)

// typeBrowserCookie is the record type for a Safari cookie.
const typeBrowserCookie = "browser_cookie"

// CookieRecord represents a Safari cookie from Cookies.binarycookies.
// Each record corresponds to a single cookie stored by Safari.
type CookieRecord struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who owns this cookie.
	User string `json:"user"`

	// Domain is the cookie's domain (e.g., ".github.com").
	Domain string `json:"domain"`

	// Name is the cookie name.
	Name string `json:"name"`

	// Path is the cookie path (e.g., "/").
	Path string `json:"path"`

	// ValuePreview is a redacted preview of the cookie value.
	// For security, only metadata is preserved by default.
	// Format: "[REDACTED - N chars]" where N is the actual length.
	ValuePreview string `json:"value_preview"`

	// ValueLength is the length of the original cookie value in bytes.
	ValueLength int `json:"value_length"`

	// CreationTime is when the cookie was created.
	CreationTime time.Time `json:"creation_time"`

	// ExpiryTime is when the cookie expires.
	ExpiryTime time.Time `json:"expiry_time"`

	// Secure indicates if the cookie has the Secure flag.
	// Secure cookies are only sent over HTTPS.
	Secure bool `json:"secure"`

	// HTTPOnly indicates if the cookie has the HttpOnly flag.
	// HttpOnly cookies are not accessible to JavaScript.
	HTTPOnly bool `json:"http_only"`

	// SourceFile is the path to the Cookies.binarycookies file.
	SourceFile string `json:"source_file"`
}

// Cookie flag bit positions.
const (
	// cookieFlagSecure indicates the Secure flag (bit 0).
	cookieFlagSecure uint32 = 0x0001

	// cookieFlagHTTPOnly indicates the HttpOnly flag (bit 2).
	cookieFlagHTTPOnly uint32 = 0x0004
)

// Binary cookies format constants.
const (
	// cookieMagic is the file header magic bytes "cook".
	cookieMagic = "cook"
)

// Binary format errors.
var (
	// ErrInvalidCookieMagic indicates the file doesn't have the "cook" magic header.
	ErrInvalidCookieMagic = errors.New("invalid cookie file: missing 'cook' magic header")

	// ErrInvalidCookiePage indicates a malformed page structure.
	ErrInvalidCookiePage = errors.New("invalid cookie page structure")

	// ErrInvalidCookieRecord indicates a malformed cookie record.
	ErrInvalidCookieRecord = errors.New("invalid cookie record")
)

// collectCookiesForUser collects Safari cookies from Cookies.binarycookies for a single user.
// It reads the binary cookies file, parses all cookie records, and adds them to the collector
// along with the raw file. Cookie values are redacted for security.
//
// Safari stores cookies in different locations depending on macOS version:
//   - Legacy (pre-sandbox): ~/Library/Cookies/Cookies.binarycookies
//   - Sandboxed (10.14+): ~/Library/Containers/com.apple.Safari/Data/Library/Cookies/Cookies.binarycookies
func collectCookiesForUser(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	// Resolve candidate cookie locations from the manifest (sandboxed first,
	// then legacy) and use the first that exists.
	cookiesPaths, err := locator.Paths(src, user, string(ArtifactCookies))
	if err != nil {
		return err
	}

	var cookiesPath string
	var fileInfo macfor.FileInfo
	var lastErr error

	for _, rp := range cookiesPaths {
		info, err := src.Stat(rp.Path)
		if err == nil {
			cookiesPath = rp.Path
			fileInfo = info
			break
		}
		lastErr = err
	}

	// If no cookies file found, check if it's a permission error or truly not found.
	if cookiesPath == "" {
		if lastErr != nil && errors.Is(lastErr, macfor.ErrPermissionDenied) {
			return fmt.Errorf("checking Cookies.binarycookies: %w", lastErr)
		}
		out.Log(macfor.LogDebug, "Cookies.binarycookies not found for user %s", user.Username)
		return nil
	}

	// Create artifact for the raw file and parsed records.
	artifact, err := macfor.NewArtifact(PluginID, "browser", "cookies")
	if err != nil {
		return fmt.Errorf("creating artifact: %w", err)
	}
	artifact.SourcePath = cookiesPath
	artifact.Username = user.Username
	artifact.Description = "Safari browser cookies"
	artifact.OriginalModTime = fileInfo.ModTime()
	artifact.OriginalSize = fileInfo.Size()

	// Read the cookies file.
	rawFile, err := src.Open(cookiesPath)
	if err != nil {
		return fmt.Errorf("opening Cookies.binarycookies: %w", err)
	}

	// Read all content for parsing and container storage.
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rawFile); err != nil {
		_ = rawFile.Close()
		return fmt.Errorf("reading Cookies.binarycookies: %w", err)
	}
	_ = rawFile.Close()
	cookieData := buf.Bytes()

	// Add raw file to container from the already-read buffer.
	if err := out.AddFile(ctx, *artifact, bytes.NewReader(cookieData)); err != nil {
		return fmt.Errorf("adding Cookies.binarycookies file: %w", err)
	}

	// Parse the binary cookies.
	cookies, err := parseBinaryCookies(cookieData)
	if err != nil {
		out.Log(macfor.LogWarn, "Error parsing Cookies.binarycookies for user %s: %v", user.Username, err)
		// Still return nil since we collected the raw file.
		return nil
	}

	// Handle empty cookies.
	if len(cookies) == 0 {
		out.Log(macfor.LogInfo, "No cookies found for user %s", user.Username)
		return nil
	}

	// Convert and output each cookie record.
	count := 0
	for _, cookie := range cookies {
		// Check for context cancellation.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		record := convertCookie(cookie, user.Username, cookiesPath)

		if err := out.AddRecord(ctx, *artifact, record); err != nil {
			out.Log(macfor.LogWarn, "Error adding cookie record: %v", err)
			continue
		}
		count++
	}

	out.Log(macfor.LogInfo, "Collected %d cookies for user %s", count, user.Username)
	return nil
}

// parsedCookie represents a parsed cookie from the binary format.
type parsedCookie struct {
	Domain   string
	Name     string
	Path     string
	Value    string
	Expiry   time.Time
	Creation time.Time
	Flags    uint32
}

// IsSecure returns true if the cookie has the Secure flag set.
func (c *parsedCookie) IsSecure() bool {
	return c.Flags&cookieFlagSecure != 0
}

// IsHTTPOnly returns true if the cookie has the HttpOnly flag set.
func (c *parsedCookie) IsHTTPOnly() bool {
	return c.Flags&cookieFlagHTTPOnly != 0
}

// parseBinaryCookies parses Safari's binary cookies format.
// The format consists of:
// - 4 bytes: "cook" magic header
// - 4 bytes: number of pages (big-endian)
// - N*4 bytes: page sizes (big-endian)
// - Page data for each page
// - 8 bytes: checksum (ignored).
func parseBinaryCookies(data []byte) ([]parsedCookie, error) {
	if len(data) < 8 {
		return nil, ErrInvalidCookieMagic
	}

	// Check magic header.
	if string(data[0:4]) != cookieMagic {
		return nil, ErrInvalidCookieMagic
	}

	// Read number of pages (big-endian).
	numPages := binary.BigEndian.Uint32(data[4:8])

	// Handle empty file.
	if numPages == 0 {
		return nil, nil
	}

	// Verify we have enough data for page sizes.
	headerEnd := 8 + int(numPages)*4
	if len(data) < headerEnd {
		return nil, fmt.Errorf("%w: truncated page size array", ErrInvalidCookiePage)
	}

	// Read page sizes.
	pageSizes := make([]uint32, numPages)
	for i := uint32(0); i < numPages; i++ {
		offset := 8 + i*4
		pageSizes[i] = binary.BigEndian.Uint32(data[offset : offset+4])
	}

	// Parse each page.
	var cookies []parsedCookie
	pageOffset := headerEnd

	for i, pageSize := range pageSizes {
		if pageOffset+int(pageSize) > len(data) {
			return cookies, fmt.Errorf("%w: page %d extends beyond file", ErrInvalidCookiePage, i)
		}

		pageData := data[pageOffset : pageOffset+int(pageSize)]
		pageCookies, err := parseCookiePage(pageData)
		if err != nil {
			// Log warning but continue with other pages.
			continue
		}
		cookies = append(cookies, pageCookies...)

		pageOffset += int(pageSize)
	}

	return cookies, nil
}

// parseCookiePage parses a single page from the binary cookies format.
// Page structure:
// - 4 bytes: page header (00 00 01 00 in file = 0x00010000 little-endian)
// - 4 bytes: number of cookies (little-endian)
// - N*4 bytes: cookie offsets (little-endian)
// - 4 bytes: end marker (0x00000000)
// - Cookie records.
func parseCookiePage(data []byte) ([]parsedCookie, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("%w: page too short", ErrInvalidCookiePage)
	}

	// Verify page header magic. The bytes in file are 00 00 01 00 which
	// when read as little-endian uint32 is 0x00010000.
	pageHeader := binary.LittleEndian.Uint32(data[0:4])
	if pageHeader != 0x00010000 {
		return nil, fmt.Errorf("%w: invalid page header %x", ErrInvalidCookiePage, pageHeader)
	}

	// Read number of cookies (little-endian).
	numCookies := binary.LittleEndian.Uint32(data[4:8])
	if numCookies == 0 {
		return nil, nil
	}

	// Calculate expected header size.
	headerSize := 8 + int(numCookies)*4 + 4 // header + offsets + end marker
	if len(data) < headerSize {
		return nil, fmt.Errorf("%w: truncated cookie offset array", ErrInvalidCookiePage)
	}

	// Read cookie offsets.
	cookieOffsets := make([]uint32, numCookies)
	for i := uint32(0); i < numCookies; i++ {
		offset := 8 + i*4
		cookieOffsets[i] = binary.LittleEndian.Uint32(data[offset : offset+4])
	}

	// Parse each cookie.
	var cookies []parsedCookie
	for _, offset := range cookieOffsets {
		if int(offset) >= len(data) {
			continue
		}

		cookie, err := parseCookieRecord(data[offset:])
		if err != nil {
			// Log and continue with other cookies.
			continue
		}

		// Validate we got a reasonable cookie.
		if cookie.Name == "" && cookie.Domain == "" {
			continue
		}

		cookies = append(cookies, cookie)
	}

	return cookies, nil
}

// parseCookieRecord parses a single cookie record from the binary format.
// Cookie record structure (all offsets are from the start of this record):
// - 4 bytes: record size (little-endian)
// - 4 bytes: flags (little-endian)
// - 4 bytes: URL/domain offset (little-endian)
// - 4 bytes: name offset (little-endian)
// - 4 bytes: path offset (little-endian)
// - 4 bytes: value offset (little-endian)
// - 8 bytes: end header marker (zeros)
// - 8 bytes: expiry time (little-endian float64, Core Data timestamp)
// - 8 bytes: creation time (little-endian float64, Core Data timestamp)
// - Null-terminated strings: domain, name, path, value.
func parseCookieRecord(data []byte) (parsedCookie, error) {
	if len(data) < 48 {
		return parsedCookie{}, fmt.Errorf("%w: record too short", ErrInvalidCookieRecord)
	}

	// Read record size.
	recordSize := binary.LittleEndian.Uint32(data[0:4])
	if recordSize > uint32(len(data)) { //nolint:gosec // Intentional: capping recordSize to available data
		recordSize = uint32(len(data)) //nolint:gosec // Intentional: len(data) is always small in cookie files
	}

	// Read flags.
	flags := binary.LittleEndian.Uint32(data[4:8])

	// Read string offsets.
	urlOffset := binary.LittleEndian.Uint32(data[8:12])
	nameOffset := binary.LittleEndian.Uint32(data[12:16])
	pathOffset := binary.LittleEndian.Uint32(data[16:20])
	valueOffset := binary.LittleEndian.Uint32(data[20:24])

	// Skip end header marker (8 bytes at offset 24-32).

	// Read expiry time (offset 32-40).
	expiryBits := binary.LittleEndian.Uint64(data[32:40])
	expiryFloat := math.Float64frombits(expiryBits)
	expiry := timestamp.CoreDataToTime(expiryFloat)

	// Read creation time (offset 40-48).
	creationBits := binary.LittleEndian.Uint64(data[40:48])
	creationFloat := math.Float64frombits(creationBits)
	creation := timestamp.CoreDataToTime(creationFloat)

	// Read null-terminated strings.
	recordData := data[:recordSize]

	domain := readNullTerminatedString(recordData, urlOffset)
	name := readNullTerminatedString(recordData, nameOffset)
	path := readNullTerminatedString(recordData, pathOffset)
	value := readNullTerminatedString(recordData, valueOffset)

	return parsedCookie{
		Domain:   domain,
		Name:     name,
		Path:     path,
		Value:    value,
		Expiry:   expiry,
		Creation: creation,
		Flags:    flags,
	}, nil
}

// readNullTerminatedString reads a null-terminated string from data starting at offset.
// Returns empty string if offset is invalid.
func readNullTerminatedString(data []byte, offset uint32) string {
	if int(offset) >= len(data) {
		return ""
	}

	// Find null terminator.
	end := int(offset)
	for end < len(data) && data[end] != 0 {
		end++
	}

	return string(data[offset:end])
}

// convertCookie converts a parsed cookie to a CookieRecord.
// Cookie values are redacted for security - only metadata is preserved.
func convertCookie(cookie parsedCookie, username, sourcePath string) *CookieRecord {
	// Create redacted value preview.
	valueLen := len(cookie.Value)
	valuePreview := fmt.Sprintf("[REDACTED - %d chars]", valueLen)

	return &CookieRecord{
		Type:         typeBrowserCookie,
		Browser:      browserSafari,
		User:         username,
		Domain:       cookie.Domain,
		Name:         cookie.Name,
		Path:         cookie.Path,
		ValuePreview: valuePreview,
		ValueLength:  valueLen,
		CreationTime: cookie.Creation,
		ExpiryTime:   cookie.Expiry,
		Secure:       cookie.IsSecure(),
		HTTPOnly:     cookie.IsHTTPOnly(),
		SourceFile:   sourcePath,
	}
}
