//nolint:goconst // Test files use repeated string literals for clarity
package shell

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

func TestCollectZshHistory_Success(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", `: 1706012400:0;ls -la
: 1706012410:0;cd /var/log
: 1706012420:5;pwd
`)

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check file was collected
	if len(collector.files) != 1 {
		t.Errorf("expected 1 file collected, got %d", len(collector.files))
	}

	// Check records were parsed
	if len(collector.records) != 3 {
		t.Errorf("expected 3 records, got %d", len(collector.records))
	}

	// Verify first record
	if len(collector.records) > 0 {
		rec, ok := collector.records[0].(ZshRecord)
		if !ok {
			t.Fatalf("expected ZshRecord, got %T", collector.records[0])
		}
		if rec.Command != "ls -la" {
			t.Errorf("expected command 'ls -la', got %q", rec.Command)
		}
		if rec.User != "admin" {
			t.Errorf("expected user 'admin', got %q", rec.User)
		}
		if rec.Shell != string(TypeZsh) {
			t.Errorf("expected shell %q, got %q", TypeZsh, rec.Shell)
		}
		if rec.LineNumber != 1 {
			t.Errorf("expected line number 1, got %d", rec.LineNumber)
		}
		if rec.Timestamp == nil {
			t.Error("expected timestamp to be set")
		} else {
			expected := time.Unix(1706012400, 0).UTC()
			if !rec.Timestamp.Equal(expected) {
				t.Errorf("expected timestamp %v, got %v", expected, *rec.Timestamp)
			}
		}
	}

	// Verify third record has duration
	if len(collector.records) > 2 {
		rec := collector.records[2].(ZshRecord)
		if rec.Duration != 5 {
			t.Errorf("expected duration 5, got %d", rec.Duration)
		}
	}
}

func TestCollectZshHistory_BasicFormat(t *testing.T) {
	// Zsh history without timestamps (basic format)
	content := `ls -la
cd /var/log
grep error syslog`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(collector.records))
	}

	// Basic format should have no timestamps
	rec := collector.records[0].(ZshRecord)
	if rec.Timestamp != nil {
		t.Error("expected no timestamp for basic format")
	}
	if rec.Command != "ls -la" {
		t.Errorf("expected command 'ls -la', got %q", rec.Command)
	}
}

func TestCollectZshHistory_MixedFormat(t *testing.T) {
	// Mix of extended and basic format
	content := `: 1706012400:0;ls -la
cd /tmp
: 1706012410:0;pwd`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(collector.records))
	}

	// First has timestamp
	rec0 := collector.records[0].(ZshRecord)
	if rec0.Timestamp == nil {
		t.Error("expected timestamp for first record")
	}

	// Second has no timestamp
	rec1 := collector.records[1].(ZshRecord)
	if rec1.Timestamp != nil {
		t.Error("expected no timestamp for second record")
	}
	if rec1.Command != "cd /tmp" {
		t.Errorf("expected command 'cd /tmp', got %q", rec1.Command)
	}

	// Third has timestamp
	rec2 := collector.records[2].(ZshRecord)
	if rec2.Timestamp == nil {
		t.Error("expected timestamp for third record")
	}
}

func TestCollectZshHistory_MultilineCommand(t *testing.T) {
	// Multi-line command with backslash continuation
	content := `: 1706012400:0;echo "line1" \
line2 \
line3
: 1706012410:0;pwd`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(collector.records))
	}

	// First command should span multiple lines (backslashes preserved)
	rec0 := collector.records[0].(ZshRecord)
	expected := "echo \"line1\" \\\nline2 \\\nline3"
	if rec0.Command != expected {
		t.Errorf("expected multi-line command %q, got %q", expected, rec0.Command)
	}
	if rec0.LineNumber != 1 {
		t.Errorf("expected line number 1 (start of multi-line), got %d", rec0.LineNumber)
	}

	// Second command should be normal
	rec1 := collector.records[1].(ZshRecord)
	if rec1.Command != "pwd" {
		t.Errorf("expected command 'pwd', got %q", rec1.Command)
	}
}

func TestCollectZshHistory_MultilineAtEndOfFile(t *testing.T) {
	// Multi-line command at end of file without final newline
	content := `: 1706012400:0;echo "line1" \
line2`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(collector.records))
	}

	rec := collector.records[0].(ZshRecord)
	expected := "echo \"line1\" \\\nline2"
	if rec.Command != expected {
		t.Errorf("expected command %q, got %q", expected, rec.Command)
	}
}

func TestCollectZshHistory_EscapedBackslash(t *testing.T) {
	// Line ending with escaped backslash (not continuation)
	content := `: 1706012400:0;echo "path\\"`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(collector.records))
	}

	// Even number of backslashes = escaped, not continuation
	rec := collector.records[0].(ZshRecord)
	expected := `echo "path\\"`
	if rec.Command != expected {
		t.Errorf("expected command %q, got %q", expected, rec.Command)
	}
}

func TestCollectZshHistory_FileNotFound(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	// No history file added

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}

	// No files or records should be collected
	if len(collector.files) != 0 {
		t.Errorf("expected no files collected, got %d", len(collector.files))
	}
	if len(collector.records) != 0 {
		t.Errorf("expected no records, got %d", len(collector.records))
	}
}

func TestCollectZshHistory_PermissionDenied_Stat(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("root", "/var/root")
	// File exists but stat returns permission denied
	src.files["/var/root/.zsh_history"] = mockFileInfo{
		name:    ".zsh_history",
		size:    100,
		mode:    0600,
		modTime: time.Now(),
		isDir:   false,
	}

	// Override Stat to return permission denied
	permDeniedSrc := &permDeniedStatSource{
		mockSourceWithContent: src,
		permDeniedPaths:       map[string]bool{"/var/root/.zsh_history": true},
	}

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, permDeniedSrc, collector)
	if err != nil {
		t.Fatalf("unexpected error for permission denied: %v", err)
	}

	// Should have logged a warning
	hasWarn := false
	for _, log := range collector.logs {
		if log.level == macfor.LogWarn && strings.Contains(log.message, "permission denied") {
			hasWarn = true
			break
		}
	}
	if !hasWarn {
		t.Error("expected warning log for permission denied")
	}
}

func TestCollectZshHistory_PermissionDenied_Open(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("root", "/var/root")
	src.addFileWithContent("/var/root/.zsh_history", ": 1706012400:0;secret command")
	src.openErr["/var/root/.zsh_history"] = fs.ErrPermission

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error for permission denied: %v", err)
	}

	// Should have logged a warning
	hasWarn := false
	for _, log := range collector.logs {
		if log.level == macfor.LogWarn && strings.Contains(log.message, "permission denied") {
			hasWarn = true
			break
		}
	}
	if !hasWarn {
		t.Error("expected warning log for permission denied on open")
	}
}

func TestCollectZshHistory_EmptyFile(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", "")

	collector := newMockCollector()
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should be collected but no records parsed
	if len(collector.files) != 1 {
		t.Errorf("expected 1 file collected, got %d", len(collector.files))
	}
	if len(collector.records) != 0 {
		t.Errorf("expected 0 records for empty file, got %d", len(collector.records))
	}
}

func TestCollectZshHistory_AddFileError(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", ": 1706012400:0;ls\n: 1706012410:0;pwd\n")

	collector := newMockCollector()
	collector.addFileErr = errors.New("disk full")
	user := src.users[0]

	err := collectZshHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have logged an error and recorded it
	if len(collector.errors) != 1 {
		t.Errorf("expected 1 error recorded, got %d", len(collector.errors))
	}

	// Parsing should still continue
	if len(collector.records) != 2 {
		t.Errorf("expected 2 records even with file add error, got %d", len(collector.records))
	}
}

func TestParseZshHistory_ExtendedFormat(t *testing.T) {
	content := `: 1706012400:0;ls -la
: 1706012410:5;cd /var/log
: 1706012420:10;grep error system.log`

	records, warnings := parseZshHistory(content, "admin", "/Users/admin/.zsh_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	expected := []struct {
		cmd      string
		line     int
		unix     int64
		duration int
	}{
		{"ls -la", 1, 1706012400, 0},
		{"cd /var/log", 2, 1706012410, 5},
		{"grep error system.log", 3, 1706012420, 10},
	}

	for i, exp := range expected {
		rec := records[i]
		if rec.Command != exp.cmd {
			t.Errorf("record %d: expected command %q, got %q", i, exp.cmd, rec.Command)
		}
		if rec.LineNumber != exp.line {
			t.Errorf("record %d: expected line %d, got %d", i, exp.line, rec.LineNumber)
		}
		if rec.Type != "shell_command" {
			t.Errorf("record %d: expected type 'shell_command', got %q", i, rec.Type)
		}
		if rec.Shell != string(TypeZsh) {
			t.Errorf("record %d: expected shell %q, got %q", i, TypeZsh, rec.Shell)
		}
		if rec.Timestamp == nil {
			t.Errorf("record %d: expected timestamp", i)
		} else if rec.Timestamp.Unix() != exp.unix {
			t.Errorf("record %d: expected timestamp %d, got %d", i, exp.unix, rec.Timestamp.Unix())
		}
		if rec.Duration != exp.duration {
			t.Errorf("record %d: expected duration %d, got %d", i, exp.duration, rec.Duration)
		}
	}
}

func TestParseZshHistory_EmptyLines(t *testing.T) {
	content := `: 1706012400:0;ls

: 1706012410:0;cd /tmp

: 1706012420:0;pwd
`

	records, _ := parseZshHistory(content, "admin", "/test")

	if len(records) != 3 {
		t.Errorf("expected 3 records (empty lines skipped), got %d", len(records))
	}
}

func TestParseZshHistory_WhitespaceLines(t *testing.T) {
	content := `: 1706012400:0;ls

: 1706012410:0;pwd`

	records, _ := parseZshHistory(content, "admin", "/test")

	if len(records) != 2 {
		t.Errorf("expected 2 records (whitespace lines skipped), got %d", len(records))
	}
}

func TestParseZshExtendedLine(t *testing.T) {
	tests := []struct {
		line       string
		expectExt  bool
		expectUnix int64
		expectDur  int
		expectCmd  string
	}{
		{": 1706012400:0;ls -la", true, 1706012400, 0, "ls -la"},
		{": 1706012410:5;cd /tmp", true, 1706012410, 5, "cd /tmp"},
		{": 1706012420:100;long running command", true, 1706012420, 100, "long running command"},
		// Not extended format
		{"ls -la", false, 0, 0, ""},
		{": 1706012400:0", false, 0, 0, ""},       // Missing command
		{": abc:0;ls", false, 0, 0, ""},           // Invalid timestamp
		{": 1706012400:abc;ls", false, 0, 0, ""},  // Invalid duration - but still parses
		{": 100:0;ls", false, 0, 0, ""},           // Timestamp too old
		{": 9999999999999:0;ls", false, 0, 0, ""}, // Timestamp too far in future
		{":1706012400:0;ls", false, 0, 0, ""},     // Missing space after colon
		{" : 1706012400:0;ls", false, 0, 0, ""},   // Leading space
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			ts, dur, cmd, isExt := parseZshExtendedLine(tt.line)
			if isExt != tt.expectExt {
				t.Errorf("parseZshExtendedLine(%q) isExtended = %v, want %v", tt.line, isExt, tt.expectExt)
			}
			if isExt {
				if ts == nil {
					t.Errorf("parseZshExtendedLine(%q) timestamp is nil", tt.line)
				} else if ts.Unix() != tt.expectUnix {
					t.Errorf("parseZshExtendedLine(%q) timestamp = %d, want %d", tt.line, ts.Unix(), tt.expectUnix)
				}
				if dur != tt.expectDur {
					t.Errorf("parseZshExtendedLine(%q) duration = %d, want %d", tt.line, dur, tt.expectDur)
				}
				if cmd != tt.expectCmd {
					t.Errorf("parseZshExtendedLine(%q) cmd = %q, want %q", tt.line, cmd, tt.expectCmd)
				}
			}
		})
	}
}

func TestParseZshHistory_SpecialCharacters(t *testing.T) {
	content := `: 1706012400:0;echo "hello world"
: 1706012410:0;ls | grep foo
: 1706012420:0;cat file.txt >> output.log
: 1706012430:0;export PATH=$PATH:/usr/local/bin
: 1706012440:0;echo $HOME
: 1706012450:0;find . -name "*.go" -exec wc -l {} \;`

	records, warnings := parseZshHistory(content, "admin", "/test")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 6 {
		t.Errorf("expected 6 records, got %d", len(records))
	}

	// Verify commands are preserved exactly
	if records[0].Command != `echo "hello world"` {
		t.Errorf("quotes not preserved: %q", records[0].Command)
	}
	if records[1].Command != "ls | grep foo" {
		t.Errorf("pipes not preserved: %q", records[1].Command)
	}
}

func TestParseZshHistory_Unicode(t *testing.T) {
	content := `: 1706012400:0;echo "日本語"
: 1706012410:0;echo "émojis 🎉"
: 1706012420:0;echo "中文测试"`

	records, warnings := parseZshHistory(content, "admin", "/test")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}

	if records[0].Command != `echo "日本語"` {
		t.Errorf("unicode not preserved: %q", records[0].Command)
	}
}

func TestParseZshHistory_CommandWithSemicolon(t *testing.T) {
	// Commands containing semicolons should be preserved
	content := `: 1706012400:0;echo "a"; echo "b"
: 1706012410:0;for i in 1 2 3; do echo $i; done`

	records, _ := parseZshHistory(content, "admin", "/test")

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if records[0].Command != `echo "a"; echo "b"` {
		t.Errorf("semicolon command not preserved: %q", records[0].Command)
	}
	if records[1].Command != "for i in 1 2 3; do echo $i; done" {
		t.Errorf("for loop not preserved: %q", records[1].Command)
	}
}

func TestIsLineContinuation(t *testing.T) {
	tests := []struct {
		line   string
		expect bool
	}{
		{`echo hello \`, true},     // Single backslash - continuation
		{`echo hello \\`, false},   // Two backslashes - escaped
		{`echo hello \\\`, true},   // Three backslashes - continuation
		{`echo hello \\\\`, false}, // Four backslashes - escaped
		{`echo hello`, false},      // No backslash
		{``, false},                // Empty
		{`\`, true},                // Just backslash
		{`\\`, false},              // Just escaped backslash
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			result := isLineContinuation(tt.line)
			if result != tt.expect {
				t.Errorf("isLineContinuation(%q) = %v, want %v", tt.line, result, tt.expect)
			}
		})
	}
}

func TestTrimContinuationBackslash(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{`echo hello \`, "echo hello "},     // Remove continuation backslash
		{`echo hello \\`, `echo hello \\`},  // Keep escaped backslash
		{`echo hello \\\`, `echo hello \\`}, // Remove continuation, keep escaped
		{`echo hello`, "echo hello"},        // No backslash
		{``, ""},                            // Empty
		{`\`, ""},                           // Just backslash
		{`\\`, `\\`},                        // Just escaped backslash
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := trimContinuationBackslash(tt.input)
			if result != tt.expect {
				t.Errorf("trimContinuationBackslash(%q) = %q, want %q", tt.input, result, tt.expect)
			}
		})
	}
}

func TestZshRecord_Fields(t *testing.T) {
	ts := time.Unix(1706012400, 0).UTC()
	record := ZshRecord{
		Type:       "shell_command",
		User:       "admin",
		Shell:      string(TypeZsh),
		Command:    "ls -la",
		Timestamp:  &ts,
		Duration:   5,
		LineNumber: 42,
		SourceFile: "/Users/admin/.zsh_history",
	}

	if record.Type != "shell_command" {
		t.Errorf("expected Type 'shell_command', got %q", record.Type)
	}
	if record.Shell != string(TypeZsh) {
		t.Errorf("expected Shell %q, got %q", TypeZsh, record.Shell)
	}
	if record.LineNumber != 42 {
		t.Errorf("expected LineNumber 42, got %d", record.LineNumber)
	}
	if record.Duration != 5 {
		t.Errorf("expected Duration 5, got %d", record.Duration)
	}
}
