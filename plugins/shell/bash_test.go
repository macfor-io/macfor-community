package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// mockSourceWithContent extends mockSource with content for Open().
type mockSourceWithContent struct {
	mockSource
	fileContents map[string]string
	openErr      map[string]error
}

func newMockSourceWithContent() *mockSourceWithContent {
	return &mockSourceWithContent{
		mockSource: mockSource{
			osVersion: "14.2.1",
			hostname:  "test-mac.local",
			users:     []macfor.User{},
			files:     make(map[string]mockFileInfo),
		},
		fileContents: make(map[string]string),
		openErr:      make(map[string]error),
	}
}

func (m *mockSourceWithContent) Open(path string) (io.ReadCloser, error) {
	if err, ok := m.openErr[path]; ok {
		return nil, err
	}
	if content, ok := m.fileContents[path]; ok {
		return io.NopCloser(strings.NewReader(content)), nil
	}
	return nil, fs.ErrNotExist
}

func (m *mockSourceWithContent) addFileWithContent(path, content string) {
	m.files[path] = mockFileInfo{
		name:    path,
		size:    int64(len(content)),
		mode:    0644,
		modTime: time.Now(),
		isDir:   false,
	}
	m.fileContents[path] = content
}

// mockCollector implements macfor.Collector for testing.
type mockCollector struct {
	files      map[string][]byte
	records    []any
	errors     []mockError
	logs       []mockLog
	addFileErr error
	addRecErr  error
}

type mockError struct {
	artifact macfor.Artifact
	err      error
}

type mockLog struct {
	level   macfor.LogLevel
	message string
}

func newMockCollector() *mockCollector {
	return &mockCollector{
		files:   make(map[string][]byte),
		records: []any{},
		errors:  []mockError{},
		logs:    []mockLog{},
	}
}

func (c *mockCollector) AddFile(_ context.Context, artifact macfor.Artifact, src io.Reader) error {
	if c.addFileErr != nil {
		return c.addFileErr
	}
	content, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	c.files[artifact.ContainerPath()] = content
	return nil
}

func (c *mockCollector) AddRecord(_ context.Context, _ macfor.Artifact, record any) error {
	if c.addRecErr != nil {
		return c.addRecErr
	}
	c.records = append(c.records, record)
	return nil
}

func (c *mockCollector) AddError(artifact macfor.Artifact, err error) {
	c.errors = append(c.errors, mockError{artifact: artifact, err: err})
}

//nolint:goprintffuncname // Implements macfor.Collector interface which uses Log not Logf.
func (c *mockCollector) Log(level macfor.LogLevel, format string, args ...any) {
	c.logs = append(c.logs, mockLog{level: level, message: fmt.Sprintf(format, args...)})
}

func TestCollectBashHistory_Success(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.bash_history", "ls -la\ncd /var/log\npwd\n")

	collector := newMockCollector()
	user := src.users[0]

	err := collectBashHistory(context.Background(), user, src, collector)
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
		rec, ok := collector.records[0].(BashRecord)
		if !ok {
			t.Fatalf("expected BashRecord, got %T", collector.records[0])
		}
		if rec.Command != "ls -la" {
			t.Errorf("expected command 'ls -la', got %q", rec.Command)
		}
		if rec.User != "admin" {
			t.Errorf("expected user 'admin', got %q", rec.User)
		}
		if rec.Shell != string(TypeBash) {
			t.Errorf("expected shell %q, got %q", TypeBash, rec.Shell)
		}
		if rec.LineNumber != 1 {
			t.Errorf("expected line number 1, got %d", rec.LineNumber)
		}
	}
}

func TestCollectBashHistory_WithTimestamps(t *testing.T) {
	// HISTTIMEFORMAT produces timestamps on lines before commands
	content := `#1706012400
ls -la
#1706012410
cd /var/log
#1706012420
grep error syslog`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.bash_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectBashHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(collector.records))
	}

	// Verify timestamps were parsed
	rec0 := collector.records[0].(BashRecord)
	if rec0.Timestamp == nil {
		t.Error("expected timestamp for first record")
	} else {
		expected := time.Unix(1706012400, 0).UTC()
		if !rec0.Timestamp.Equal(expected) {
			t.Errorf("expected timestamp %v, got %v", expected, *rec0.Timestamp)
		}
	}

	rec1 := collector.records[1].(BashRecord)
	if rec1.Timestamp == nil {
		t.Error("expected timestamp for second record")
	}

	rec2 := collector.records[2].(BashRecord)
	if rec2.Timestamp == nil {
		t.Error("expected timestamp for third record")
	}
}

func TestCollectBashHistory_MixedTimestamps(t *testing.T) {
	// Some commands have timestamps, some don't
	content := `ls
#1706012400
cd /tmp
pwd`

	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.bash_history", content)

	collector := newMockCollector()
	user := src.users[0]

	err := collectBashHistory(context.Background(), user, src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collector.records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(collector.records))
	}

	// First command should have no timestamp
	rec0 := collector.records[0].(BashRecord)
	if rec0.Timestamp != nil {
		t.Error("expected no timestamp for first record")
	}

	// Second command should have timestamp
	rec1 := collector.records[1].(BashRecord)
	if rec1.Timestamp == nil {
		t.Error("expected timestamp for second record")
	}

	// Third command should have no timestamp (timestamp was consumed)
	rec2 := collector.records[2].(BashRecord)
	if rec2.Timestamp != nil {
		t.Error("expected no timestamp for third record")
	}
}

func TestCollectBashHistory_FileNotFound(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	// No history file added

	collector := newMockCollector()
	user := src.users[0]

	err := collectBashHistory(context.Background(), user, src, collector)
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

func TestCollectBashHistory_PermissionDenied_Stat(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("root", "/var/root")
	// File exists but stat returns permission denied
	src.files["/var/root/.bash_history"] = mockFileInfo{
		name:    ".bash_history",
		size:    100,
		mode:    0600,
		modTime: time.Now(),
		isDir:   false,
	}

	// Override Stat to return permission denied
	permDeniedSrc := &permDeniedStatSource{
		mockSourceWithContent: src,
		permDeniedPaths:       map[string]bool{"/var/root/.bash_history": true},
	}

	collector := newMockCollector()
	user := src.users[0]

	err := collectBashHistory(context.Background(), user, permDeniedSrc, collector)
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

type permDeniedStatSource struct {
	*mockSourceWithContent
	permDeniedPaths map[string]bool
}

func (s *permDeniedStatSource) Stat(path string) (macfor.FileInfo, error) {
	if s.permDeniedPaths[path] {
		return nil, fs.ErrPermission
	}
	return s.mockSourceWithContent.Stat(path)
}

func TestCollectBashHistory_PermissionDenied_Open(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("root", "/var/root")
	src.addFileWithContent("/var/root/.bash_history", "secret command")
	src.openErr["/var/root/.bash_history"] = fs.ErrPermission

	collector := newMockCollector()
	user := src.users[0]

	err := collectBashHistory(context.Background(), user, src, collector)
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

func TestCollectBashHistory_EmptyFile(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.bash_history", "")

	collector := newMockCollector()
	user := src.users[0]

	err := collectBashHistory(context.Background(), user, src, collector)
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

func TestCollectBashHistory_AddFileError(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.bash_history", "ls\npwd\n")

	collector := newMockCollector()
	collector.addFileErr = errors.New("disk full")
	user := src.users[0]

	err := collectBashHistory(context.Background(), user, src, collector)
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

func TestParseBashHistory_Basic(t *testing.T) {
	content := `ls -la
cd /var/log
grep error system.log`

	records, warnings := parseBashHistory(content, "admin", "/Users/admin/.bash_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	expected := []struct {
		cmd  string
		line int
	}{
		{"ls -la", 1},
		{"cd /var/log", 2},
		{"grep error system.log", 3},
	}

	for i, exp := range expected {
		if records[i].Command != exp.cmd {
			t.Errorf("record %d: expected command %q, got %q", i, exp.cmd, records[i].Command)
		}
		if records[i].LineNumber != exp.line {
			t.Errorf("record %d: expected line %d, got %d", i, exp.line, records[i].LineNumber)
		}
		if records[i].Type != "shell_command" {
			t.Errorf("record %d: expected type 'shell_command', got %q", i, records[i].Type)
		}
		if records[i].Shell != string(TypeBash) {
			t.Errorf("record %d: expected shell %q, got %q", i, TypeBash, records[i].Shell)
		}
	}
}

func TestParseBashHistory_EmptyLines(t *testing.T) {
	content := `ls

cd /tmp

pwd
`

	records, _ := parseBashHistory(content, "admin", "/test")

	if len(records) != 3 {
		t.Errorf("expected 3 records (empty lines skipped), got %d", len(records))
	}
}

func TestParseBashHistory_WhitespaceLines(t *testing.T) {
	content := "ls\n   \t  \npwd"

	records, _ := parseBashHistory(content, "admin", "/test")

	if len(records) != 2 {
		t.Errorf("expected 2 records (whitespace lines skipped), got %d", len(records))
	}
}

func TestParseBashTimestamp(t *testing.T) {
	tests := []struct {
		line      string
		expectTS  bool
		expectVal int64
	}{
		{"#1706012400", true, 1706012400},
		{"#1706012410", true, 1706012410},
		{"#  1706012400", true, 1706012400},
		{"#1706012400\t", true, 1706012400},
		// Not timestamps
		{"ls -la", false, 0},
		{"#", false, 0},
		{"#abc", false, 0},
		{"#-1", false, 0},            // Negative
		{"#100", false, 0},           // Too old (before 1990)
		{"#9999999999999", false, 0}, // Too far in future
		{"# comment", false, 0},      // Looks like comment
		{"#1706012400 extra", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			ts, ok := parseBashTimestamp(tt.line)
			if ok != tt.expectTS {
				t.Errorf("parseBashTimestamp(%q) ok = %v, want %v", tt.line, ok, tt.expectTS)
			}
			if ok && ts != nil {
				if ts.Unix() != tt.expectVal {
					t.Errorf("parseBashTimestamp(%q) = %d, want %d", tt.line, ts.Unix(), tt.expectVal)
				}
			}
		})
	}
}

func TestParseBashHistory_SpecialCharacters(t *testing.T) {
	content := `echo "hello world"
ls | grep foo
cat file.txt >> output.log
export PATH=$PATH:/usr/local/bin
echo $HOME
find . -name "*.go" -exec wc -l {} \;`

	records, warnings := parseBashHistory(content, "admin", "/test")

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

func TestParseBashHistory_Unicode(t *testing.T) {
	content := `echo "日本語"
echo "émojis 🎉"
echo "中文测试"`

	records, warnings := parseBashHistory(content, "admin", "/test")

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

func TestBashRecord_Fields(t *testing.T) {
	ts := time.Unix(1706012400, 0).UTC()
	record := BashRecord{
		Type:       "shell_command",
		User:       "admin",
		Shell:      string(TypeBash),
		Command:    "ls -la",
		Timestamp:  &ts,
		LineNumber: 42,
		SourceFile: "/Users/admin/.bash_history",
	}

	if record.Type != "shell_command" {
		t.Errorf("expected Type 'shell_command', got %q", record.Type)
	}
	if record.Shell != string(TypeBash) {
		t.Errorf("expected Shell %q, got %q", TypeBash, record.Shell)
	}
	if record.LineNumber != 42 {
		t.Errorf("expected LineNumber 42, got %d", record.LineNumber)
	}
}
