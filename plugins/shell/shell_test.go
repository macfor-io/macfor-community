package shell

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// mockSource is a test implementation of macfor.Source.
type mockSource struct {
	osVersion string
	osErr     error
	hostname  string
	hostErr   error
	users     []macfor.User
	usersErr  error
	files     map[string]mockFileInfo
}

type mockFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (m mockFileInfo) Name() string          { return m.name }
func (m mockFileInfo) Size() int64           { return m.size }
func (m mockFileInfo) Mode() fs.FileMode     { return m.mode }
func (m mockFileInfo) ModTime() time.Time    { return m.modTime }
func (m mockFileInfo) IsDir() bool           { return m.isDir }
func (m mockFileInfo) Sys() any              { return nil }
func (m mockFileInfo) AccessTime() time.Time { return m.modTime }
func (m mockFileInfo) ChangeTime() time.Time { return m.modTime }
func (m mockFileInfo) BirthTime() time.Time  { return m.modTime }

func newMockSource() *mockSource {
	return &mockSource{
		osVersion: "14.2.1",
		hostname:  "test-mac.local",
		users:     []macfor.User{},
		files:     make(map[string]mockFileInfo),
	}
}

func (m *mockSource) Type() macfor.SourceType {
	return macfor.SourceTypeLive
}

func (m *mockSource) OSVersion() (string, error) {
	return m.osVersion, m.osErr
}

func (m *mockSource) Hostname() (string, error) {
	return m.hostname, m.hostErr
}

func (m *mockSource) Open(_ string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented in mock")
}

func (m *mockSource) Stat(path string) (macfor.FileInfo, error) {
	if info, ok := m.files[path]; ok {
		return info, nil
	}
	return nil, fs.ErrNotExist
}

func (m *mockSource) ReadDir(_ string) ([]macfor.DirEntry, error) {
	return nil, errors.New("not implemented in mock")
}

func (m *mockSource) Glob(_ string) ([]string, error) {
	return nil, errors.New("not implemented in mock")
}

func (m *mockSource) Walk(_ string, _ macfor.WalkFunc) error {
	return errors.New("not implemented in mock")
}

func (m *mockSource) Users() ([]macfor.User, error) {
	return m.users, m.usersErr
}

func (m *mockSource) ResolvePath(path string) string {
	return path
}

func (m *mockSource) ReadPlist(_ string, _ any) error {
	return errors.New("not implemented in mock")
}

func (m *mockSource) SystemInfo() (*macfor.SystemInfo, error) {
	return &macfor.SystemInfo{
		OSName:       "macOS",
		OSVersion:    m.osVersion,
		Architecture: "arm64",
		Hostname:     m.hostname,
		SourceType:   macfor.SourceTypeLive,
	}, nil
}

// addUser adds a test user to the mock source.
func (m *mockSource) addUser(username, homeDir string) {
	m.users = append(m.users, macfor.User{
		UID:      500 + len(m.users),
		GID:      20,
		Username: username,
		Name:     username,
		HomeDir:  homeDir,
		Shell:    "/bin/zsh",
	})
}

// addFile adds a test file to the mock source.
func (m *mockSource) addFile(path string) {
	m.files[path] = mockFileInfo{
		name:    path,
		size:    1024,
		mode:    0644,
		modTime: time.Now(),
		isDir:   false,
	}
}

func TestPlugin_Info(t *testing.T) {
	p := &Plugin{}
	info := p.Info()

	if info.ID != "shell.history" {
		t.Errorf("expected ID 'shell.history', got %q", info.ID)
	}

	if info.Name != "Shell History" {
		t.Errorf("expected Name 'Shell History', got %q", info.Name)
	}

	if info.Version != "1.0.0" {
		t.Errorf("expected Version '1.0.0', got %q", info.Version)
	}

	// Check categories
	expectedCats := map[string]bool{"user": true, "command": true, "shell": true}
	for _, cat := range info.Categories {
		if !expectedCats[cat] {
			t.Errorf("unexpected category: %q", cat)
		}
		delete(expectedCats, cat)
	}
	if len(expectedCats) > 0 {
		t.Errorf("missing categories: %v", expectedCats)
	}

	// Check platforms
	if len(info.Platforms) != 1 {
		t.Errorf("expected 1 platform, got %d", len(info.Platforms))
	} else {
		plat := info.Platforms[0]
		if plat.OS != "darwin" {
			t.Errorf("expected OS 'darwin', got %q", plat.OS)
		}
		if plat.MinVersion != "10.12" {
			t.Errorf("expected MinVersion '10.12', got %q", plat.MinVersion)
		}
	}
}

func TestPlugin_Detect_NoUsers(t *testing.T) {
	src := newMockSource()
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Applicable {
		t.Error("expected not applicable when no users")
	}

	if result.OSVersion != "14.2.1" {
		t.Errorf("expected OSVersion '14.2.1', got %q", result.OSVersion)
	}

	if result.Reason != "no users found on system" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestPlugin_Detect_NoHistoryFiles(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Applicable {
		t.Error("expected not applicable when no history files exist")
	}

	if result.Reason != "no shell history files found for any user" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestPlugin_Detect_WithZshHistory(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/.zsh_history")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable when zsh history exists")
	}

	if result.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", result.Confidence)
	}

	shells, ok := result.Metadata["shells_detected"].([]string)
	if !ok {
		t.Fatal("expected shells_detected in metadata")
	}

	found := false
	for _, s := range shells {
		if s == "zsh" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'zsh' in shells_detected")
	}

	if result.Metadata["users_with_history"] != 1 {
		t.Errorf("expected users_with_history=1, got %v", result.Metadata["users_with_history"])
	}
}

func TestPlugin_Detect_WithMultipleShells(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/.zsh_history")
	src.addFile("/Users/admin/.bash_history")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable")
	}

	shells, ok := result.Metadata["shells_detected"].([]string)
	if !ok {
		t.Fatal("expected shells_detected in metadata")
	}

	if len(shells) != 2 {
		t.Errorf("expected 2 shells detected, got %d", len(shells))
	}
}

func TestPlugin_Detect_MultipleUsers(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addUser("developer", "/Users/developer")
	src.addFile("/Users/admin/.zsh_history")
	src.addFile("/Users/developer/.bash_history")
	src.addFile("/Users/developer/.zsh_history")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable")
	}

	if result.Metadata["users_with_history"] != 2 {
		t.Errorf("expected users_with_history=2, got %v", result.Metadata["users_with_history"])
	}

	if result.Metadata["total_users"] != 2 {
		t.Errorf("expected total_users=2, got %v", result.Metadata["total_users"])
	}

	// Check counts
	if result.Metadata["zsh_count"] != 2 {
		t.Errorf("expected zsh_count=2, got %v", result.Metadata["zsh_count"])
	}

	if result.Metadata["bash_count"] != 1 {
		t.Errorf("expected bash_count=1, got %v", result.Metadata["bash_count"])
	}
}

func TestPlugin_Detect_FishHistory(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/.local/share/fish/fish_history")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable when fish history exists")
	}

	shells, ok := result.Metadata["shells_detected"].([]string)
	if !ok {
		t.Fatal("expected shells_detected in metadata")
	}

	found := false
	for _, s := range shells {
		if s == "fish" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'fish' in shells_detected")
	}
}

func TestPlugin_Detect_OldMacOSVersion(t *testing.T) {
	src := newMockSource()
	src.osVersion = "10.11.6" // El Capitan - below minimum
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/.zsh_history")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Applicable {
		t.Error("expected not applicable for old macOS version")
	}

	if result.Reason == "" {
		t.Error("expected reason to be set for old version")
	}
}

func TestPlugin_Detect_OSVersionError(t *testing.T) {
	src := newMockSource()
	src.osErr = errors.New("cannot read version")
	p := &Plugin{}

	_, err := p.Detect(context.Background(), src)
	if err == nil {
		t.Error("expected error when OSVersion fails")
	}
}

func TestPlugin_Detect_UsersError(t *testing.T) {
	src := newMockSource()
	src.usersErr = errors.New("cannot enumerate users")
	p := &Plugin{}

	_, err := p.Detect(context.Background(), src)
	if err == nil {
		t.Error("expected error when Users fails")
	}
}

func TestIsVersionSupported(t *testing.T) {
	tests := []struct {
		current  string
		minimum  string
		expected bool
	}{
		// Equal versions
		{"10.12", "10.12", true},
		{"10.12.0", "10.12", true},
		{"10.12", "10.12.0", true},

		// Current is newer
		{"14.2.1", "10.12", true},
		{"10.13", "10.12", true},
		{"10.12.1", "10.12", true},
		{"11.0", "10.15", true},

		// Current is older
		{"10.11", "10.12", false},
		{"10.11.6", "10.12", false},
		{"9.0", "10.12", false},

		// Edge cases
		{"10.12.6", "10.12.5", true},
		{"10.12.5", "10.12.6", false},

		// Versions with suffixes
		{"14.2.1-beta", "10.12", true},
	}

	for _, tt := range tests {
		t.Run(tt.current+"_vs_"+tt.minimum, func(t *testing.T) {
			result := artifacts.VersionAtLeast(tt.current, tt.minimum)
			if result != tt.expected {
				t.Errorf("artifacts.VersionAtLeast(%q, %q) = %v, want %v",
					tt.current, tt.minimum, result, tt.expected)
			}
		})
	}
}

func TestPluginRegistration(t *testing.T) {
	// The plugin should be registered via init().
	p, found := macfor.ByID("shell.history")
	if !found {
		t.Fatal("shell.history plugin not registered")
	}

	if p.Info().ID != "shell.history" {
		t.Errorf("unexpected plugin ID: %s", p.Info().ID)
	}
}

func TestPlugin_Collect_Success(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", `: 1706012400:0;ls -la
: 1706012410:1;cd /var/log
`)
	src.addFileWithContent("/Users/admin/.bash_history", `pwd
echo "hello"
`)

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have collected files for both zsh and bash
	if len(collector.files) != 2 {
		t.Errorf("expected 2 files collected, got %d", len(collector.files))
	}

	// Should have parsed records from both
	if len(collector.records) != 4 {
		t.Errorf("expected 4 records (2 zsh + 2 bash), got %d", len(collector.records))
	}

	// Verify we have zsh and bash records
	var zshCount, bashCount int
	for _, rec := range collector.records {
		switch r := rec.(type) {
		case ZshRecord:
			zshCount++
			if r.User != testUserAdmin {
				t.Errorf("expected user %q, got %q", testUserAdmin, r.User)
			}
		case BashRecord:
			bashCount++
			if r.User != testUserAdmin {
				t.Errorf("expected user %q, got %q", testUserAdmin, r.User)
			}
		}
	}

	if zshCount != 2 {
		t.Errorf("expected 2 zsh records, got %d", zshCount)
	}
	if bashCount != 2 {
		t.Errorf("expected 2 bash records, got %d", bashCount)
	}
}

func TestPlugin_Collect_MultipleUsers(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addUser("developer", "/Users/developer")
	src.addFileWithContent("/Users/admin/.zsh_history", `: 1706012400:0;admin-cmd1
`)
	src.addFileWithContent("/Users/developer/.bash_history", `dev-cmd1
dev-cmd2
`)

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have collected files for both users
	if len(collector.files) != 2 {
		t.Errorf("expected 2 files collected, got %d", len(collector.files))
	}

	// Should have 3 total records
	if len(collector.records) != 3 {
		t.Errorf("expected 3 records, got %d", len(collector.records))
	}

	// Check admin's zsh record
	foundAdminZsh := false
	foundDevBash := false
	for _, rec := range collector.records {
		switch r := rec.(type) {
		case ZshRecord:
			if r.User == testUserAdmin && r.Command == "admin-cmd1" {
				foundAdminZsh = true
			}
		case BashRecord:
			if r.User == "developer" {
				foundDevBash = true
			}
		}
	}

	if !foundAdminZsh {
		t.Error("expected admin's zsh record")
	}
	if !foundDevBash {
		t.Error("expected developer's bash record")
	}
}

func TestPlugin_Collect_NoUsers(t *testing.T) {
	src := newMockSourceWithContent()
	// No users added

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have logged a warning
	hasWarn := false
	for _, log := range collector.logs {
		if log.level == macfor.LogWarn && strings.Contains(log.message, "No users found") {
			hasWarn = true
			break
		}
	}
	if !hasWarn {
		t.Error("expected warning about no users")
	}
}

func TestPlugin_Collect_UsersError(t *testing.T) {
	src := newMockSourceWithContent()
	src.usersErr = errors.New("cannot enumerate users")

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)
	if err == nil {
		t.Error("expected error when Users() fails")
	}

	if !strings.Contains(err.Error(), "enumerating users") {
		t.Errorf("expected 'enumerating users' in error, got: %v", err)
	}
}

func TestPlugin_Collect_NoHistoryFiles(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	// No history files added

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No files should be collected
	if len(collector.files) != 0 {
		t.Errorf("expected 0 files collected, got %d", len(collector.files))
	}

	// No records should be parsed
	if len(collector.records) != 0 {
		t.Errorf("expected 0 records, got %d", len(collector.records))
	}
}

func TestPlugin_Collect_AllShells(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", `: 1706012400:0;zsh-cmd
`)
	src.addFileWithContent("/Users/admin/.bash_history", `bash-cmd
`)
	src.addFileWithContent("/Users/admin/.local/share/fish/fish_history", `- cmd: fish-cmd
  when: 1706012400
`)
	src.addFileWithContent("/Users/admin/.sh_history", `sh-cmd
`)

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have collected all 4 history files
	if len(collector.files) != 4 {
		t.Errorf("expected 4 files collected, got %d", len(collector.files))
	}

	// Should have 4 records (1 per shell)
	if len(collector.records) != 4 {
		t.Errorf("expected 4 records, got %d", len(collector.records))
	}

	// Verify all shell types are present
	shells := make(map[string]bool)
	for _, rec := range collector.records {
		switch r := rec.(type) {
		case ZshRecord:
			shells["zsh"] = true
		case BashRecord:
			shells["bash"] = true
		case FishRecord:
			shells["fish"] = true
		case ShRecord:
			shells["sh"] = true
		default:
			t.Errorf("unexpected record type: %T", r)
		}
	}

	for _, shell := range []string{"zsh", "bash", "fish", "sh"} {
		if !shells[shell] {
			t.Errorf("expected record for shell %q", shell)
		}
	}
}

func TestPlugin_Collect_CollectionOrder(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", `: 1706012400:0;zsh-cmd
`)
	src.addFileWithContent("/Users/admin/.bash_history", `bash-cmd
`)
	src.addFileWithContent("/Users/admin/.local/share/fish/fish_history", `- cmd: fish-cmd
  when: 1706012400
`)
	src.addFileWithContent("/Users/admin/.sh_history", `sh-cmd
`)

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify collection order: zsh → bash → fish → sh
	expectedOrder := []string{"zsh", "bash", "fish", "sh"}
	if len(collector.records) != 4 {
		t.Fatalf("expected 4 records, got %d", len(collector.records))
	}

	for i, exp := range expectedOrder {
		var shell string
		switch r := collector.records[i].(type) {
		case ZshRecord:
			shell = r.Shell
		case BashRecord:
			shell = r.Shell
		case FishRecord:
			shell = r.Shell
		case ShRecord:
			shell = r.Shell
		}

		if shell != exp {
			t.Errorf("record %d: expected shell %q, got %q", i, exp, shell)
		}
	}
}

func TestPlugin_Collect_ContextCancellation(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", `: 1706012400:0;zsh-cmd
`)

	p := &Plugin{}
	collector := newMockCollector()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Collect(ctx, src, collector)
	if err == nil {
		t.Error("expected context cancellation error")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestPlugin_Collect_PartialErrors(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addUser("locked", "/Users/locked")

	// Admin has accessible history
	src.addFileWithContent("/Users/admin/.zsh_history", `: 1706012400:0;admin-cmd
`)

	// Locked user has file but it's readable (Stat works)
	src.files["/Users/locked/.zsh_history"] = mockFileInfo{
		name:    ".zsh_history",
		size:    100,
		mode:    0644,
		modTime: time.Now(),
		isDir:   false,
	}
	// But Open fails with a generic error (not permission denied)
	src.openErr["/Users/locked/.zsh_history"] = errors.New("disk read error")

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)

	// Should have errors aggregated
	if err == nil {
		t.Error("expected aggregated errors")
	}

	// But admin's collection should have succeeded
	if len(collector.files) != 1 {
		t.Errorf("expected 1 file collected (admin's), got %d", len(collector.files))
	}

	if len(collector.records) != 1 {
		t.Errorf("expected 1 record (admin's), got %d", len(collector.records))
	}
}

func TestPlugin_Collect_Logging(t *testing.T) {
	src := newMockSourceWithContent()
	src.addUser("admin", "/Users/admin")
	src.addFileWithContent("/Users/admin/.zsh_history", `: 1706012400:0;cmd1
`)

	p := &Plugin{}
	collector := newMockCollector()

	err := p.Collect(context.Background(), src, collector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for expected log messages
	hasStartLog := false
	hasUserLog := false
	hasCompletedLog := false

	for _, log := range collector.logs {
		if strings.Contains(log.message, "Starting shell history collection") {
			hasStartLog = true
		}
		if strings.Contains(log.message, "Collecting shell history for user") {
			hasUserLog = true
		}
		if strings.Contains(log.message, "collection completed") {
			hasCompletedLog = true
		}
	}

	if !hasStartLog {
		t.Error("expected 'Starting shell history collection' log")
	}
	if !hasUserLog {
		t.Error("expected 'Collecting shell history for user' log")
	}
	if !hasCompletedLog {
		t.Error("expected 'collection completed' log")
	}
}
