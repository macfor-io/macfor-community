package safari

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
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// mockSource is a test implementation of macfor.Source.
type mockSource struct {
	osVersion  string
	osErr      error
	hostname   string
	hostErr    error
	users      []macfor.User
	usersErr   error
	files      map[string]mockFileInfo
	dirs       map[string][]mockDirEntry
	globs      map[string][]string
	plistData  map[string]any
	plistErr   map[string]error
	readDirErr map[string]error
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

type mockDirEntry struct {
	name  string
	isDir bool
	mode  fs.FileMode
}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                { return m.isDir }
func (m mockDirEntry) Type() fs.FileMode          { return m.mode }
func (m mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func newMockSource() *mockSource {
	return &mockSource{
		osVersion:  "14.2.1",
		hostname:   "test-mac.local",
		users:      []macfor.User{},
		files:      make(map[string]mockFileInfo),
		dirs:       make(map[string][]mockDirEntry),
		globs:      make(map[string][]string),
		plistData:  make(map[string]any),
		plistErr:   make(map[string]error),
		readDirErr: make(map[string]error),
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

func (m *mockSource) ReadDir(path string) ([]macfor.DirEntry, error) {
	if err, ok := m.readDirErr[path]; ok {
		return nil, err
	}
	if entries, ok := m.dirs[path]; ok {
		result := make([]macfor.DirEntry, len(entries))
		for i, e := range entries {
			result[i] = e
		}
		return result, nil
	}
	return nil, fs.ErrNotExist
}

func (m *mockSource) Glob(pattern string) ([]string, error) {
	return m.globs[pattern], nil
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

func (m *mockSource) ReadPlist(path string, v any) error {
	if err, ok := m.plistErr[path]; ok {
		return err
	}
	if data, ok := m.plistData[path]; ok {
		// For Safari Info.plist, we need to handle the struct assignment
		if safariInfo, ok := v.(*struct {
			CFBundleShortVersionString string `plist:"CFBundleShortVersionString"`
			CFBundleVersion            string `plist:"CFBundleVersion"`
		}); ok {
			if infoData, ok := data.(map[string]string); ok {
				safariInfo.CFBundleShortVersionString = infoData["CFBundleShortVersionString"]
				safariInfo.CFBundleVersion = infoData["CFBundleVersion"]
				return nil
			}
		}
	}
	return errors.New("plist not found or invalid type")
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

// addGlob registers the matches returned by Glob for the given pattern.
func (m *mockSource) addGlob(pattern string, matches []string) {
	m.globs[pattern] = matches
}

// setSafariVersion sets the Safari version returned by ReadPlist.
func (m *mockSource) setSafariVersion(version string) {
	m.plistData[SafariAppInfoPlist] = map[string]string{
		"CFBundleShortVersionString": version,
	}
}

func TestPlugin_Info(t *testing.T) {
	p := &Plugin{}
	info := p.Info()

	if info.ID != "browser.safari" {
		t.Errorf("expected ID 'browser.safari', got %q", info.ID)
	}

	if info.Name != "Safari Browser" {
		t.Errorf("expected Name 'Safari Browser', got %q", info.Name)
	}

	if info.Version != "1.0.0" {
		t.Errorf("expected Version '1.0.0', got %q", info.Version)
	}

	// Check categories
	expectedCats := map[string]bool{"browser": true, "user": true, "web": true}
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

func TestPlugin_Detect_NoArtifacts(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Applicable {
		t.Error("expected not applicable when no Safari artifacts exist")
	}

	if result.Reason != "no Safari artifacts found for any user" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestPlugin_Detect_WithHistoryDB(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/Library/Safari/History.db")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable when History.db exists")
	}

	if result.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", result.Confidence)
	}

	artifacts, ok := result.Metadata["artifacts_detected"].([]string)
	if !ok {
		t.Fatal("expected artifacts_detected in metadata")
	}

	found := false
	for _, a := range artifacts {
		if a == "history" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'history' in artifacts_detected")
	}

	if result.Metadata["users_with_safari"] != 1 {
		t.Errorf("expected users_with_safari=1, got %v", result.Metadata["users_with_safari"])
	}
}

func TestPlugin_Detect_WithMultipleArtifacts(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/Library/Safari/History.db")
	src.addFile("/Users/admin/Library/Safari/Downloads.plist")
	src.addFile("/Users/admin/Library/Safari/Bookmarks.plist")
	src.addFile("/Users/admin/Library/Cookies/Cookies.binarycookies")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable")
	}

	artifacts, ok := result.Metadata["artifacts_detected"].([]string)
	if !ok {
		t.Fatal("expected artifacts_detected in metadata")
	}

	if len(artifacts) != 4 {
		t.Errorf("expected 4 artifacts detected, got %d", len(artifacts))
	}
}

func TestPlugin_Detect_MultipleUsers(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addUser("developer", "/Users/developer")
	src.addFile("/Users/admin/Library/Safari/History.db")
	src.addFile("/Users/developer/Library/Safari/History.db")
	src.addFile("/Users/developer/Library/Safari/Bookmarks.plist")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable")
	}

	if result.Metadata["users_with_safari"] != 2 {
		t.Errorf("expected users_with_safari=2, got %v", result.Metadata["users_with_safari"])
	}

	if result.Metadata["total_users"] != 2 {
		t.Errorf("expected total_users=2, got %v", result.Metadata["total_users"])
	}

	// Check history count (2 users have it)
	if result.Metadata["history_count"] != 2 {
		t.Errorf("expected history_count=2, got %v", result.Metadata["history_count"])
	}
}

func TestPlugin_Detect_LocalStorage(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	// Register .localstorage databases matching the manifest's Big Sur+ glob.
	src.addGlob("/Users/admin/Library/WebKit/WebsiteData/LocalStorage/*.localstorage", []string{
		"/Users/admin/Library/WebKit/WebsiteData/LocalStorage/https_example.com_0.localstorage",
		"/Users/admin/Library/WebKit/WebsiteData/LocalStorage/https_other.com_0.localstorage",
	})
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable when local storage exists")
	}

	artifacts, ok := result.Metadata["artifacts_detected"].([]string)
	if !ok {
		t.Fatal("expected artifacts_detected in metadata")
	}

	found := false
	for _, a := range artifacts {
		if a == "local_storage" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'local_storage' in artifacts_detected")
	}
}

func TestPlugin_Detect_EmptyLocalStorageDir(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	// Register an empty glob result (directory present but no databases).
	src.addGlob("/Users/admin/Library/WebKit/WebsiteData/LocalStorage/*.localstorage", []string{})
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty directory should not be considered an artifact
	if result.Applicable {
		t.Error("expected not applicable when local storage directory is empty")
	}
}

func TestPlugin_Detect_SafariVersion(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/Library/Safari/History.db")
	src.setSafariVersion("17.2.1")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AppVersion != "17.2.1" {
		t.Errorf("expected AppVersion '17.2.1', got %q", result.AppVersion)
	}

	if result.Metadata["safari_version"] != "17.2.1" {
		t.Errorf("expected safari_version='17.2.1' in metadata, got %v", result.Metadata["safari_version"])
	}
}

func TestPlugin_Detect_OldMacOSVersion(t *testing.T) {
	src := newMockSource()
	src.osVersion = "10.11.6" // El Capitan - below minimum
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/Library/Safari/History.db")
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

func TestPlugin_Detect_ContextCancellation(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addFile("/Users/admin/Library/Safari/History.db")
	p := &Plugin{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Detect(ctx, src)
	if err == nil {
		t.Error("expected context cancellation error")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestPlugin_Detect_AllArtifactTypes(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")

	// Add all artifact types
	src.addFile("/Users/admin/Library/Safari/History.db")
	src.addFile("/Users/admin/Library/Safari/Downloads.plist")
	src.addFile("/Users/admin/Library/Safari/Bookmarks.plist")
	src.addFile("/Users/admin/Library/Safari/LastSession.plist")
	src.addFile("/Users/admin/Library/Safari/TopSites.plist")
	src.addFile("/Users/admin/Library/Safari/Extensions/Extensions.plist")
	src.addFile("/Users/admin/Library/Cookies/Cookies.binarycookies")
	src.addGlob("/Users/admin/Library/WebKit/WebsiteData/LocalStorage/*.localstorage", []string{
		"/Users/admin/Library/WebKit/WebsiteData/LocalStorage/https_example.com_0.localstorage",
	})

	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable")
	}

	artifacts, ok := result.Metadata["artifacts_detected"].([]string)
	if !ok {
		t.Fatal("expected artifacts_detected in metadata")
	}

	// Should have 7 artifact types detected (TopSites excluded from detection
	// since collection is not implemented).
	if len(artifacts) != 7 {
		t.Errorf("expected 7 artifacts detected, got %d: %v", len(artifacts), artifacts)
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
	p, found := macfor.ByID("browser.safari")
	if !found {
		t.Fatal("browser.safari plugin not registered")
	}

	if p.Info().ID != "browser.safari" {
		t.Errorf("unexpected plugin ID: %s", p.Info().ID)
	}
}

func TestDetectSafariVersion(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*mockSource)
		expected string
	}{
		{
			name: "safari installed with short version",
			setup: func(src *mockSource) {
				src.plistData[SafariAppInfoPlist] = map[string]string{
					"CFBundleShortVersionString": "17.2.1",
				}
			},
			expected: "17.2.1",
		},
		{
			name: "safari installed with only bundle version",
			setup: func(src *mockSource) {
				src.plistData[SafariAppInfoPlist] = map[string]string{
					"CFBundleVersion": "17619.1.26.31.7",
				}
			},
			expected: "17619.1.26.31.7",
		},
		{
			name: "safari not installed",
			setup: func(src *mockSource) {
				src.plistErr[SafariAppInfoPlist] = errors.New("file not found")
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newMockSource()
			tt.setup(src)

			version := detectSafariVersion(src)

			if version != tt.expected {
				t.Errorf("detectSafariVersion() = %q, want %q", version, tt.expected)
			}
		})
	}
}

func TestPlugin_Detect_CookiesOutsideSafariDir(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	// Cookies are in Library/Cookies, not Library/Safari
	src.addFile("/Users/admin/Library/Cookies/Cookies.binarycookies")
	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applicable {
		t.Error("expected applicable when cookies exist")
	}

	artifacts, ok := result.Metadata["artifacts_detected"].([]string)
	if !ok {
		t.Fatal("expected artifacts_detected in metadata")
	}

	found := false
	for _, a := range artifacts {
		if a == "cookies" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'cookies' in artifacts_detected")
	}
}

// mockCollector is a test implementation of macfor.Collector.
type mockCollector struct {
	files      []collectedFile
	records    []collectedRecord
	errors     []collectedError
	logs       []logEntry
	addFileErr error
}

type collectedFile struct {
	artifact macfor.Artifact
	content  []byte
}

type collectedRecord struct {
	artifact macfor.Artifact
	record   any
}

type collectedError struct {
	artifact macfor.Artifact
	err      error
}

type logEntry struct {
	level   macfor.LogLevel
	message string
}

func newMockCollector() *mockCollector {
	return &mockCollector{}
}

func (c *mockCollector) AddFile(_ context.Context, artifact macfor.Artifact, src io.Reader) error {
	if c.addFileErr != nil {
		return c.addFileErr
	}
	content, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	c.files = append(c.files, collectedFile{artifact: artifact, content: content})
	return nil
}

func (c *mockCollector) AddRecord(_ context.Context, artifact macfor.Artifact, record any) error {
	c.records = append(c.records, collectedRecord{artifact: artifact, record: record})
	return nil
}

func (c *mockCollector) AddError(artifact macfor.Artifact, err error) {
	c.errors = append(c.errors, collectedError{artifact: artifact, err: err})
}

//nolint:goprintffuncname // Implementing macfor.Collector interface which uses Log.
func (c *mockCollector) Log(level macfor.LogLevel, format string, args ...any) {
	c.logs = append(c.logs, logEntry{level: level, message: fmt.Sprintf(format, args...)})
}

// hasLogContaining checks if any log message contains the given substring.
func (c *mockCollector) hasLogContaining(substr string) bool {
	for _, log := range c.logs {
		if strings.Contains(log.message, substr) {
			return true
		}
	}
	return false
}

// hasLogAtLevel checks if any log at the given level contains the substring.
func (c *mockCollector) hasLogAtLevel(level macfor.LogLevel, substr string) bool {
	for _, log := range c.logs {
		if log.level == level && strings.Contains(log.message, substr) {
			return true
		}
	}
	return false
}

func TestPlugin_Collect_NoUsers(t *testing.T) {
	src := newMockSource()
	coll := newMockCollector()
	p := &Plugin{}

	err := p.Collect(context.Background(), src, coll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should log warning about no users
	if !coll.hasLogAtLevel(macfor.LogWarn, "No users found") {
		t.Error("expected warning about no users found")
	}
}

func TestPlugin_Collect_UsersError(t *testing.T) {
	src := newMockSource()
	src.usersErr = errors.New("cannot enumerate users")
	coll := newMockCollector()
	p := &Plugin{}

	err := p.Collect(context.Background(), src, coll)
	if err == nil {
		t.Fatal("expected error when Users() fails")
	}

	if !strings.Contains(err.Error(), "enumerating users") {
		t.Errorf("expected error to mention 'enumerating users', got: %v", err)
	}
}

func TestPlugin_Collect_SingleUser(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	coll := newMockCollector()
	p := &Plugin{}

	err := p.Collect(context.Background(), src, coll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should log info about starting collection
	if !coll.hasLogAtLevel(macfor.LogInfo, "Starting Safari collection for 1 user") {
		t.Error("expected info log about starting collection")
	}

	// Should log info about collecting for the user
	if !coll.hasLogAtLevel(macfor.LogInfo, "Collecting Safari data for user: admin") {
		t.Error("expected info log about collecting for admin")
	}

	// Should log completion
	if !coll.hasLogAtLevel(macfor.LogInfo, "Safari collection completed") {
		t.Error("expected info log about collection completion")
	}
}

func TestPlugin_Collect_MultipleUsers(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	src.addUser("developer", "/Users/developer")
	coll := newMockCollector()
	p := &Plugin{}

	err := p.Collect(context.Background(), src, coll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should log starting for 2 users
	if !coll.hasLogAtLevel(macfor.LogInfo, "Starting Safari collection for 2 user") {
		t.Error("expected info log about starting collection for 2 users")
	}

	// Should log for both users
	if !coll.hasLogContaining("Collecting Safari data for user: admin") {
		t.Error("expected log for admin user")
	}
	if !coll.hasLogContaining("Collecting Safari data for user: developer") {
		t.Error("expected log for developer user")
	}
}

func TestPlugin_Collect_ContextCancellation(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	coll := newMockCollector()
	p := &Plugin{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Collect(ctx, src, coll)
	if err == nil {
		t.Error("expected error from context cancellation")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestPlugin_Collect_HistoryOnlyOption(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	coll := newMockCollector()
	p := &Plugin{
		Options: &CollectionOptions{
			HistoryOnly: true,
		},
	}

	err := p.Collect(context.Background(), src, coll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should log skipping for non-history artifacts
	if !coll.hasLogContaining("Skipping downloads for user admin") {
		t.Error("expected log about skipping downloads")
	}
	if !coll.hasLogContaining("Skipping bookmarks for user admin") {
		t.Error("expected log about skipping bookmarks")
	}
	if !coll.hasLogContaining("Skipping cookies for user admin") {
		t.Error("expected log about skipping cookies")
	}

	// Should NOT skip history
	if coll.hasLogContaining("Skipping history for user admin") {
		t.Error("should not skip history when HistoryOnly is set")
	}
}

func TestPlugin_Collect_SkipCookiesOption(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	coll := newMockCollector()
	p := &Plugin{
		Options: &CollectionOptions{
			SkipCookies: true,
		},
	}

	err := p.Collect(context.Background(), src, coll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should skip cookies
	if !coll.hasLogContaining("Skipping cookies for user admin") {
		t.Error("expected log about skipping cookies")
	}

	// Should NOT skip other artifacts
	if coll.hasLogContaining("Skipping history") {
		t.Error("should not skip history")
	}
	if coll.hasLogContaining("Skipping downloads") {
		t.Error("should not skip downloads")
	}
}

func TestPlugin_Collect_SkipLocalStorageOption(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	coll := newMockCollector()
	p := &Plugin{
		Options: &CollectionOptions{
			SkipLocalStorage: true,
		},
	}

	err := p.Collect(context.Background(), src, coll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should skip local storage
	if !coll.hasLogContaining("Skipping local_storage for user admin") {
		t.Error("expected log about skipping local_storage")
	}

	// Should NOT skip other artifacts
	if coll.hasLogContaining("Skipping history") {
		t.Error("should not skip history")
	}
	if coll.hasLogContaining("Skipping cookies") {
		t.Error("should not skip cookies")
	}
}

func TestPlugin_Collect_CombinedOptions(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	coll := newMockCollector()
	p := &Plugin{
		Options: &CollectionOptions{
			SkipCookies:      true,
			SkipLocalStorage: true,
		},
	}

	err := p.Collect(context.Background(), src, coll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should skip both cookies and local storage
	if !coll.hasLogContaining("Skipping cookies") {
		t.Error("expected log about skipping cookies")
	}
	if !coll.hasLogContaining("Skipping local_storage") {
		t.Error("expected log about skipping local_storage")
	}
}

func TestPlugin_Collect_DefaultOptions(t *testing.T) {
	src := newMockSource()
	src.addUser("admin", "/Users/admin")
	coll := newMockCollector()
	p := &Plugin{} // nil Options should use defaults

	err := p.Collect(context.Background(), src, coll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not skip any artifacts with default options
	for _, artifactType := range CollectionOrder {
		skipMsg := fmt.Sprintf("Skipping %s", artifactType)
		if coll.hasLogContaining(skipMsg) {
			t.Errorf("should not skip %s with default options", artifactType)
		}
	}
}

func TestDefaultCollectionOptions(t *testing.T) {
	opts := DefaultCollectionOptions()

	if opts.SkipCookies {
		t.Error("default SkipCookies should be false")
	}
	if opts.SkipLocalStorage {
		t.Error("default SkipLocalStorage should be false")
	}
	if opts.HistoryOnly {
		t.Error("default HistoryOnly should be false")
	}
}

func TestPlugin_shouldSkipArtifact(t *testing.T) {
	tests := []struct {
		name         string
		artifactType ArtifactType
		opts         CollectionOptions
		expectSkip   bool
	}{
		// Default options - skip nothing
		{"default_history", ArtifactHistory, CollectionOptions{}, false},
		{"default_cookies", ArtifactCookies, CollectionOptions{}, false},
		{"default_localStorage", ArtifactLocalStorage, CollectionOptions{}, false},

		// HistoryOnly - skip everything except history
		{"historyOnly_history", ArtifactHistory, CollectionOptions{HistoryOnly: true}, false},
		{"historyOnly_downloads", ArtifactDownloads, CollectionOptions{HistoryOnly: true}, true},
		{"historyOnly_bookmarks", ArtifactBookmarks, CollectionOptions{HistoryOnly: true}, true},
		{"historyOnly_cookies", ArtifactCookies, CollectionOptions{HistoryOnly: true}, true},
		{"historyOnly_extensions", ArtifactExtensions, CollectionOptions{HistoryOnly: true}, true},
		{"historyOnly_localStorage", ArtifactLocalStorage, CollectionOptions{HistoryOnly: true}, true},
		{"historyOnly_lastSession", ArtifactLastSession, CollectionOptions{HistoryOnly: true}, true},

		// SkipCookies - only skip cookies
		{"skipCookies_history", ArtifactHistory, CollectionOptions{SkipCookies: true}, false},
		{"skipCookies_cookies", ArtifactCookies, CollectionOptions{SkipCookies: true}, true},
		{"skipCookies_downloads", ArtifactDownloads, CollectionOptions{SkipCookies: true}, false},

		// SkipLocalStorage - only skip local storage
		{"skipLocalStorage_history", ArtifactHistory, CollectionOptions{SkipLocalStorage: true}, false},
		{"skipLocalStorage_localStorage", ArtifactLocalStorage, CollectionOptions{SkipLocalStorage: true}, true},
		{"skipLocalStorage_cookies", ArtifactCookies, CollectionOptions{SkipLocalStorage: true}, false},

		// Combined options
		{"combined_cookies", ArtifactCookies, CollectionOptions{SkipCookies: true, SkipLocalStorage: true}, true},
		{"combined_localStorage", ArtifactLocalStorage, CollectionOptions{SkipCookies: true, SkipLocalStorage: true}, true},
		{"combined_history", ArtifactHistory, CollectionOptions{SkipCookies: true, SkipLocalStorage: true}, false},
	}

	p := &Plugin{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.shouldSkipArtifact(tt.artifactType, tt.opts)
			if result != tt.expectSkip {
				t.Errorf("shouldSkipArtifact(%s, %+v) = %v, want %v",
					tt.artifactType, tt.opts, result, tt.expectSkip)
			}
		})
	}
}

func TestPlugin_getOptions(t *testing.T) {
	// Test nil options returns defaults
	p := &Plugin{}
	opts := p.getOptions()
	if opts.SkipCookies || opts.SkipLocalStorage || opts.HistoryOnly {
		t.Error("nil Options should return default options")
	}

	// Test custom options are returned
	customOpts := &CollectionOptions{
		SkipCookies:      true,
		SkipLocalStorage: true,
		HistoryOnly:      true,
	}
	p.Options = customOpts
	opts = p.getOptions()
	if !opts.SkipCookies || !opts.SkipLocalStorage || !opts.HistoryOnly {
		t.Error("custom Options should be returned")
	}
}

func TestCollectionOrder(t *testing.T) {
	// Verify collection order is as expected
	expected := []ArtifactType{
		ArtifactHistory,
		ArtifactDownloads,
		ArtifactBookmarks,
		ArtifactCookies,
		ArtifactExtensions,
		ArtifactLocalStorage,
		ArtifactLastSession,
	}

	if len(CollectionOrder) != len(expected) {
		t.Errorf("CollectionOrder has %d items, expected %d", len(CollectionOrder), len(expected))
	}

	for i, artifact := range expected {
		if i >= len(CollectionOrder) {
			break
		}
		if CollectionOrder[i] != artifact {
			t.Errorf("CollectionOrder[%d] = %s, expected %s", i, CollectionOrder[i], artifact)
		}
	}

	// History must be first
	if CollectionOrder[0] != ArtifactHistory {
		t.Error("History must be first in CollectionOrder")
	}
}
