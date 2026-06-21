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

func TestCollectFishHistory_Success(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.local/share/fish/fish_history", `- cmd: ls -la
  when: 1706012400
- cmd: cd /var/log
  when: 1706012410
- cmd: pwd
  when: 1706012420
`)

	collector := newMockCollector()
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, src, collector)
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
		rec, ok := collector.records[0].(FishRecord)
		if !ok {
			t.Fatalf("expected FishRecord, got %T", collector.records[0])
		}
		if rec.Command != "ls -la" {
			t.Errorf("expected command 'ls -la', got %q", rec.Command)
		}
		if rec.User != "admin" {
			t.Errorf("expected user 'admin', got %q", rec.User)
		}
		if rec.Shell != string(TypeFish) {
			t.Errorf("expected shell %q, got %q", TypeFish, rec.Shell)
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
}

func TestCollectFishHistory_WithPaths(t *testing.T) {
	// Fish history with paths
	content := `- cmd: cd /var/log
  when: 1706012400
  paths:
    - /var/log
- cmd: vim /etc/hosts
  when: 1706012410
  paths:
    - /etc/hosts
    - /etc
`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.local/share/fish/fish_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(collector.records))
	}

	// Verify first record has one path
	rec0 := collector.records[0].(FishRecord)
	if len(rec0.Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(rec0.Paths))
	} else if rec0.Paths[0] != "/var/log" {
		t.Errorf("expected path '/var/log', got %q", rec0.Paths[0])
	}

	// Verify second record has two paths
	rec1 := collector.records[1].(FishRecord)
	if len(rec1.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(rec1.Paths))
	}
}

func TestCollectFishHistory_NoTimestamp(t *testing.T) {
	// Fish history without timestamps
	content := `- cmd: ls -la
- cmd: pwd
`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.local/share/fish/fish_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(collector.records))
	}

	// No timestamps should have nil timestamp
	rec := collector.records[0].(FishRecord)
	if rec.Timestamp != nil {
		t.Error("expected no timestamp")
	}
	if rec.Command != "ls -la" {
		t.Errorf("expected command 'ls -la', got %q", rec.Command)
	}
}

func TestCollectFishHistory_MixedFormat(t *testing.T) {
	// Mix of entries with and without timestamps/paths
	content := `- cmd: ls -la
  when: 1706012400
- cmd: cd /tmp
- cmd: pwd
  when: 1706012410
  paths:
    - /tmp
`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.local/share/fish/fish_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(collector.records))
	}

	// First has timestamp
	rec0 := collector.records[0].(FishRecord)
	if rec0.Timestamp == nil {
		t.Error("expected timestamp for first record")
	}

	// Second has no timestamp
	rec1 := collector.records[1].(FishRecord)
	if rec1.Timestamp != nil {
		t.Error("expected no timestamp for second record")
	}
	if rec1.Command != "cd /tmp" {
		t.Errorf("expected command 'cd /tmp', got %q", rec1.Command)
	}

	// Third has timestamp and paths
	rec2 := collector.records[2].(FishRecord)
	if rec2.Timestamp == nil {
		t.Error("expected timestamp for third record")
	}
	if len(rec2.Paths) != 1 {
		t.Errorf("expected 1 path for third record, got %d", len(rec2.Paths))
	}
}

func TestCollectFishHistory_FileNotFound(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	// No history file added

	collector := newMockCollector()
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, src, collector)
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

func TestCollectFishHistory_PermissionDenied_Stat(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("root", "/var/root")
	// File exists but stat returns permission denied
	src.files["/var/root/.local/share/fish/fish_history"] = mockFileInfo{
		name:    "fish_history",
		size:    100,
		mode:    0600,
		modTime: time.Now(),
		isDir:   false,
	}

	// Override Stat to return permission denied
	permDeniedSrc := &permDeniedStatSource{
		mockSourceWithContent: src,
		permDeniedPaths:       map[string]bool{"/var/root/.local/share/fish/fish_history": true},
	}

	collector := newMockCollector()
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, permDeniedSrc, collector)
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

func TestCollectFishHistory_PermissionDenied_Open(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("root", "/var/root")
	src.addFileWithContent("/var/root/.local/share/fish/fish_history", "- cmd: secret command\n  when: 1706012400\n")
	src.openErr["/var/root/.local/share/fish/fish_history"] = fs.ErrPermission

	collector := newMockCollector()
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, src, collector)
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

func TestCollectFishHistory_EmptyFile(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.local/share/fish/fish_history", "")

	collector := newMockCollector()
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, src, collector)
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

func TestCollectFishHistory_AddFileError(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.local/share/fish/fish_history", "- cmd: ls\n  when: 1706012400\n- cmd: pwd\n  when: 1706012410\n")

	collector := newMockCollector()
	collector.addFileErr = errors.New("disk full")
	user := src.users[0]

	err := collectFishHistory(context.Background(), user, src, collector)
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

func TestParseFishHistory_Basic(t *testing.T) {
	content := `- cmd: ls -la
  when: 1706012400
- cmd: cd /var/log
  when: 1706012410
- cmd: grep error system.log
  when: 1706012420
`

	records, warnings := parseFishHistory(content, "admin", "/Users/admin/.local/share/fish/fish_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	expected := []struct {
		cmd  string
		line int
		unix int64
	}{
		{"ls -la", 1, 1706012400},
		{"cd /var/log", 3, 1706012410},
		{"grep error system.log", 5, 1706012420},
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
		if rec.Shell != string(TypeFish) {
			t.Errorf("record %d: expected shell %q, got %q", i, TypeFish, rec.Shell)
		}
		if rec.Timestamp == nil {
			t.Errorf("record %d: expected timestamp", i)
		} else if rec.Timestamp.Unix() != exp.unix {
			t.Errorf("record %d: expected timestamp %d, got %d", i, exp.unix, rec.Timestamp.Unix())
		}
	}
}

func TestParseFishHistory_WithPaths(t *testing.T) {
	content := `- cmd: cd /var/log
  when: 1706012400
  paths:
    - /var/log
- cmd: vim /etc/hosts
  when: 1706012410
  paths:
    - /etc/hosts
    - /etc
`

	records, warnings := parseFishHistory(content, "admin", "/test")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// First has one path
	if len(records[0].Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(records[0].Paths))
	}
	if records[0].Paths[0] != "/var/log" {
		t.Errorf("expected path '/var/log', got %q", records[0].Paths[0])
	}

	// Second has two paths
	if len(records[1].Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(records[1].Paths))
	}
	if records[1].Paths[0] != "/etc/hosts" {
		t.Errorf("expected path '/etc/hosts', got %q", records[1].Paths[0])
	}
	if records[1].Paths[1] != "/etc" {
		t.Errorf("expected path '/etc', got %q", records[1].Paths[1])
	}
}

func TestParseFishHistory_EmptyLines(t *testing.T) {
	content := `- cmd: ls
  when: 1706012400

- cmd: cd /tmp
  when: 1706012410

- cmd: pwd
  when: 1706012420
`

	records, _ := parseFishHistory(content, "admin", "/test")

	if len(records) != 3 {
		t.Errorf("expected 3 records (empty lines skipped), got %d", len(records))
	}
}

func TestParseFishTimestamp(t *testing.T) {
	tests := []struct {
		input     string
		expectTS  bool
		expectVal int64
	}{
		{"1706012400", true, 1706012400},
		{"1706012410", true, 1706012410},
		{"  1706012400  ", true, 1706012400},
		// Not timestamps
		{"abc", false, 0},
		{"", false, 0},
		{"-1", false, 0},            // Negative
		{"100", false, 0},           // Too old (before 1990)
		{"9999999999999", false, 0}, // Too far in future
		{"1706012400 extra", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ts := parseFishTimestamp(strings.TrimSpace(tt.input))
			if tt.expectTS && ts == nil {
				t.Errorf("parseFishTimestamp(%q) returned nil, expected timestamp", tt.input)
			}
			if !tt.expectTS && ts != nil {
				t.Errorf("parseFishTimestamp(%q) returned %v, expected nil", tt.input, ts)
			}
			if tt.expectTS && ts != nil {
				if ts.Unix() != tt.expectVal {
					t.Errorf("parseFishTimestamp(%q) = %d, want %d", tt.input, ts.Unix(), tt.expectVal)
				}
			}
		})
	}
}

func TestParseFishHistory_SpecialCharacters(t *testing.T) {
	content := `- cmd: echo "hello world"
  when: 1706012400
- cmd: ls | grep foo
  when: 1706012410
- cmd: cat file.txt >> output.log
  when: 1706012420
- cmd: export PATH=$PATH:/usr/local/bin
  when: 1706012430
- cmd: echo $HOME
  when: 1706012440
- cmd: find . -name "*.go" -exec wc -l {} \;
  when: 1706012450
`

	records, warnings := parseFishHistory(content, "admin", "/test")

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

func TestParseFishHistory_Unicode(t *testing.T) {
	content := `- cmd: echo "日本語"
  when: 1706012400
- cmd: echo "émojis 🎉"
  when: 1706012410
- cmd: echo "中文测试"
  when: 1706012420
`

	records, warnings := parseFishHistory(content, "admin", "/test")

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

func TestParseFishHistory_CommandsWithColons(t *testing.T) {
	// Commands containing colons should be preserved
	content := `- cmd: docker run -p 8080:80 nginx
  when: 1706012400
- cmd: echo "time: now"
  when: 1706012410
`

	records, _ := parseFishHistory(content, "admin", "/test")

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if records[0].Command != "docker run -p 8080:80 nginx" {
		t.Errorf("colon in command not preserved: %q", records[0].Command)
	}
	if records[1].Command != `echo "time: now"` {
		t.Errorf("colon in command not preserved: %q", records[1].Command)
	}
}

func TestParseFishHistory_NoEntries(t *testing.T) {
	content := ``

	records, warnings := parseFishHistory(content, "admin", "/test")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records for empty content, got %d", len(records))
	}
}

func TestParseFishHistory_OnlyEmptyLines(t *testing.T) {
	content := `



`

	records, warnings := parseFishHistory(content, "admin", "/test")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records for whitespace-only content, got %d", len(records))
	}
}

func TestParseFishHistory_InvalidTimestamp(t *testing.T) {
	// Invalid timestamps should result in nil timestamp
	content := `- cmd: ls -la
  when: invalid_timestamp
`

	records, warnings := parseFishHistory(content, "admin", "/test")

	// Should not generate warnings for invalid timestamps - just ignore them
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	if records[0].Timestamp != nil {
		t.Error("expected nil timestamp for invalid when value")
	}
	if records[0].Command != "ls -la" {
		t.Errorf("expected command 'ls -la', got %q", records[0].Command)
	}
}

func TestFishRecord_Fields(t *testing.T) {
	ts := time.Unix(1706012400, 0).UTC()
	record := FishRecord{
		Type:       "shell_command",
		User:       "admin",
		Shell:      string(TypeFish),
		Command:    "ls -la",
		Timestamp:  &ts,
		Paths:      []string{"/var/log", "/etc"},
		LineNumber: 42,
		SourceFile: "/Users/admin/.local/share/fish/fish_history",
	}

	if record.Type != "shell_command" {
		t.Errorf("expected Type 'shell_command', got %q", record.Type)
	}
	if record.Shell != string(TypeFish) {
		t.Errorf("expected Shell %q, got %q", TypeFish, record.Shell)
	}
	if record.LineNumber != 42 {
		t.Errorf("expected LineNumber 42, got %d", record.LineNumber)
	}
	if len(record.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(record.Paths))
	}
}
