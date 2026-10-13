package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

func TestParsePasswdLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    macfor.User
		wantErr bool
	}{
		{
			name: "valid user line",
			line: "jdoe:x:501:20:John Doe:/Users/jdoe:/bin/zsh",
			want: macfor.User{
				Username: "jdoe",
				UID:      501,
				GID:      20,
				Name:     "John Doe",
				HomeDir:  "/Users/jdoe",
				Shell:    "/bin/zsh",
			},
			wantErr: false,
		},
		{
			name: "root user",
			line: "root:*:0:0:System Administrator:/var/root:/bin/sh",
			want: macfor.User{
				Username: "root",
				UID:      0,
				GID:      0,
				Name:     "System Administrator",
				HomeDir:  "/var/root",
				Shell:    "/bin/sh",
			},
			wantErr: false,
		},
		{
			name: "daemon user",
			line: "daemon:*:1:1:System Services:/var/root:/usr/bin/false",
			want: macfor.User{
				Username: "daemon",
				UID:      1,
				GID:      1,
				Name:     "System Services",
				HomeDir:  "/var/root",
				Shell:    "/usr/bin/false",
			},
			wantErr: false,
		},
		{
			name: "empty gecos field",
			line: "testuser:x:502:20::/Users/testuser:/bin/bash",
			want: macfor.User{
				Username: "testuser",
				UID:      502,
				GID:      20,
				Name:     "",
				HomeDir:  "/Users/testuser",
				Shell:    "/bin/bash",
			},
			wantErr: false,
		},
		{
			name:    "too few fields",
			line:    "user:x:501:20",
			wantErr: true,
		},
		{
			name:    "invalid UID",
			line:    "user:x:notanumber:20:Name:/home/user:/bin/bash",
			wantErr: true,
		},
		{
			name:    "invalid GID",
			line:    "user:x:501:notanumber:Name:/home/user:/bin/bash",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePasswdLine(tt.line)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePasswdLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parsePasswdLine() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIsHumanUser(t *testing.T) {
	tests := []struct {
		name string
		user macfor.User
		want bool
	}{
		{
			name: "normal user with bash",
			user: macfor.User{
				Username: "jdoe",
				UID:      501,
				Shell:    "/bin/bash",
			},
			want: true,
		},
		{
			name: "normal user with zsh",
			user: macfor.User{
				Username: "admin",
				UID:      500,
				Shell:    "/bin/zsh",
			},
			want: true,
		},
		{
			name: "root user",
			user: macfor.User{
				Username: "root",
				UID:      0,
				Shell:    "/bin/sh",
			},
			want: false,
		},
		{
			name: "daemon user",
			user: macfor.User{
				Username: "daemon",
				UID:      1,
				Shell:    "/usr/bin/false",
			},
			want: false,
		},
		{
			name: "service account with false shell",
			user: macfor.User{
				Username: "_www",
				UID:      70,
				Shell:    "/usr/bin/false",
			},
			want: false,
		},
		{
			name: "service account with nologin",
			user: macfor.User{
				Username: "_sshd",
				UID:      75,
				Shell:    "/sbin/nologin",
			},
			want: false,
		},
		{
			name: "high UID but nologin shell",
			user: macfor.User{
				Username: "serviceacct",
				UID:      600,
				Shell:    "/sbin/nologin",
			},
			want: false,
		},
		{
			name: "nobody user",
			user: macfor.User{
				Username: "nobody",
				UID:      65534,
				Shell:    "/bin/sh",
			},
			want: false,
		},
		{
			name: "nfsnobody user",
			user: macfor.User{
				Username: "nfsnobody",
				UID:      65534,
				Shell:    "/bin/sh",
			},
			want: false,
		},
		{
			name: "UID just below threshold",
			user: macfor.User{
				Username: "testuser",
				UID:      499,
				Shell:    "/bin/bash",
			},
			want: false,
		},
		{
			name: "UID at threshold",
			user: macfor.User{
				Username: "testuser",
				UID:      500,
				Shell:    "/bin/bash",
			},
			want: true,
		},
		{
			name: "user with /bin/false shell",
			user: macfor.User{
				Username: "testuser",
				UID:      501,
				Shell:    "/bin/false",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHumanUser(tt.user); got != tt.want {
				t.Errorf("isHumanUser(%+v) = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}

func TestFirstString(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{
			name:  "single element",
			input: []string{"hello"},
			want:  "hello",
		},
		{
			name:  "multiple elements",
			input: []string{"first", "second", "third"},
			want:  "first",
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  "",
		},
		{
			name:  "nil slice",
			input: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstString(tt.input); got != tt.want {
				t.Errorf("firstString(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLiveSource_UsersFromPasswd(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("parse valid passwd file", func(t *testing.T) {
		// Parse using the helper to verify the parsing works
		lines := []string{
			"root:*:0:0:System Administrator:/var/root:/bin/sh",
			"jdoe:*:501:20:John Doe:/Users/jdoe:/bin/zsh",
			"admin:*:502:80:Admin User:/Users/admin:/bin/bash",
			"serviceuser:*:503:20:Service Account:/var/empty:/sbin/nologin",
		}

		var humanCount int
		for _, line := range lines {
			user, err := parsePasswdLine(line)
			if err != nil {
				t.Errorf("parsePasswdLine(%q) error = %v", line, err)
				continue
			}
			if isHumanUser(user) {
				humanCount++
			}
		}

		// Should find jdoe and admin as human users
		if humanCount != 2 {
			t.Errorf("expected 2 human users, got %d", humanCount)
		}

		_ = src // Use src to avoid unused variable warning
	})

	t.Run("skip empty and comment lines", func(t *testing.T) {
		lines := []string{
			"",
			"# This is a comment",
			"   ",
			"user:*:501:20:Test:/home/user:/bin/bash",
		}

		var parsedCount int
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			_, err := parsePasswdLine(trimmed)
			if err == nil {
				parsedCount++
			}
		}

		if parsedCount != 1 {
			t.Errorf("expected 1 parsed line, got %d", parsedCount)
		}
	})
}

func TestLiveSource_ParseUserPlist(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()

	t.Run("parse valid user plist", func(t *testing.T) {
		// Create a mock user plist matching macOS dslocal format
		userPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>name</key>
	<array>
		<string>jdoe</string>
	</array>
	<key>realname</key>
	<array>
		<string>John Doe</string>
	</array>
	<key>uid</key>
	<array>
		<string>501</string>
	</array>
	<key>gid</key>
	<array>
		<string>20</string>
	</array>
	<key>home</key>
	<array>
		<string>/Users/jdoe</string>
	</array>
	<key>shell</key>
	<array>
		<string>/bin/zsh</string>
	</array>
</dict>
</plist>`
		plistPath := filepath.Join(tmpDir, "jdoe.plist")
		if err := os.WriteFile(plistPath, []byte(userPlist), 0600); err != nil {
			t.Fatalf("failed to create mock plist: %v", err)
		}

		user, err := src.parseUserPlist(plistPath)
		if err != nil {
			t.Fatalf("parseUserPlist() error = %v", err)
		}

		if user.Username != "jdoe" {
			t.Errorf("Username = %q, want %q", user.Username, "jdoe")
		}
		if user.Name != "John Doe" {
			t.Errorf("Name = %q, want %q", user.Name, "John Doe")
		}
		if user.UID != 501 {
			t.Errorf("UID = %d, want %d", user.UID, 501)
		}
		if user.GID != 20 {
			t.Errorf("GID = %d, want %d", user.GID, 20)
		}
		if user.HomeDir != "/Users/jdoe" {
			t.Errorf("HomeDir = %q, want %q", user.HomeDir, "/Users/jdoe")
		}
		if user.Shell != "/bin/zsh" {
			t.Errorf("Shell = %q, want %q", user.Shell, "/bin/zsh")
		}
	})

	t.Run("parse plist with empty arrays", func(t *testing.T) {
		userPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>name</key>
	<array>
		<string>testuser</string>
	</array>
	<key>realname</key>
	<array/>
	<key>uid</key>
	<array>
		<string>502</string>
	</array>
	<key>gid</key>
	<array/>
	<key>home</key>
	<array>
		<string>/Users/testuser</string>
	</array>
	<key>shell</key>
	<array>
		<string>/bin/bash</string>
	</array>
</dict>
</plist>`
		plistPath := filepath.Join(tmpDir, "testuser.plist")
		if err := os.WriteFile(plistPath, []byte(userPlist), 0600); err != nil {
			t.Fatalf("failed to create mock plist: %v", err)
		}

		user, err := src.parseUserPlist(plistPath)
		if err != nil {
			t.Fatalf("parseUserPlist() error = %v", err)
		}

		if user.Username != "testuser" {
			t.Errorf("Username = %q, want %q", user.Username, "testuser")
		}
		if user.Name != "" {
			t.Errorf("Name = %q, want empty string", user.Name)
		}
		if user.GID != 0 {
			t.Errorf("GID = %d, want 0 (empty array)", user.GID)
		}
	})

	t.Run("parse system user plist", func(t *testing.T) {
		// System user like _www
		userPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>name</key>
	<array>
		<string>_www</string>
	</array>
	<key>realname</key>
	<array>
		<string>World Wide Web Server</string>
	</array>
	<key>uid</key>
	<array>
		<string>70</string>
	</array>
	<key>gid</key>
	<array>
		<string>70</string>
	</array>
	<key>home</key>
	<array>
		<string>/Library/WebServer</string>
	</array>
	<key>shell</key>
	<array>
		<string>/usr/bin/false</string>
	</array>
</dict>
</plist>`
		plistPath := filepath.Join(tmpDir, "_www.plist")
		if err := os.WriteFile(plistPath, []byte(userPlist), 0600); err != nil {
			t.Fatalf("failed to create mock plist: %v", err)
		}

		user, err := src.parseUserPlist(plistPath)
		if err != nil {
			t.Fatalf("parseUserPlist() error = %v", err)
		}

		// Should parse correctly but be filtered as non-human
		if user.Username != "_www" {
			t.Errorf("Username = %q, want %q", user.Username, "_www")
		}
		if isHumanUser(user) {
			t.Error("_www should not be classified as human user")
		}
	})

	t.Run("non-existent plist", func(t *testing.T) {
		_, err := src.parseUserPlist(filepath.Join(tmpDir, "nonexistent.plist"))
		if err == nil {
			t.Error("parseUserPlist() expected error for non-existent file")
		}
	})
}

// dsclReadallFixture mirrors "dscl -plist . -readall /Users ..." output.
const dsclReadallFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
	<dict>
		<key>dsAttrTypeStandard:NFSHomeDirectory</key><array><string>/var/empty</string></array>
		<key>dsAttrTypeStandard:PrimaryGroupID</key><array><string>-2</string></array>
		<key>dsAttrTypeStandard:RecordName</key><array><string>_spotlight</string></array>
		<key>dsAttrTypeStandard:UniqueID</key><array><string>89</string></array>
		<key>dsAttrTypeStandard:UserShell</key><array><string>/usr/bin/false</string></array>
	</dict>
	<dict>
		<key>dsAttrTypeStandard:NFSHomeDirectory</key><array><string>/Users/alice</string></array>
		<key>dsAttrTypeStandard:PrimaryGroupID</key><array><string>20</string></array>
		<key>dsAttrTypeStandard:RealName</key><array><string>Alice Example </string></array>
		<key>dsAttrTypeStandard:RecordName</key><array><string>alice</string><string>com.apple.idms.appleid.prd.0001</string></array>
		<key>dsAttrTypeStandard:UniqueID</key><array><string>501</string></array>
		<key>dsAttrTypeStandard:UserShell</key><array><string>/bin/zsh</string></array>
	</dict>
	<dict>
		<key>dsAttrTypeStandard:NFSHomeDirectory</key><array><string>/Users/bob</string></array>
		<key>dsAttrTypeStandard:RecordName</key><array><string>bob</string></array>
		<key>dsAttrTypeStandard:UniqueID</key><array><string>502</string></array>
	</dict>
	<dict>
		<key>dsAttrTypeStandard:RecordName</key><array><string>nobody</string></array>
		<key>dsAttrTypeStandard:UniqueID</key><array><string>-2</string></array>
	</dict>
</array>
</plist>`

func TestParseDSCLUsers(t *testing.T) {
	users, err := parseDSCLUsers([]byte(dsclReadallFixture))
	if err != nil {
		t.Fatalf("parseDSCLUsers() error = %v", err)
	}

	want := []macfor.User{
		{Username: "alice", UID: 501, GID: 20, Name: "Alice Example", HomeDir: "/Users/alice", Shell: "/bin/zsh"},
		{Username: "bob", UID: 502, HomeDir: "/Users/bob"},
	}
	if len(users) != len(want) {
		t.Fatalf("got %d users %+v, want %d", len(users), users, len(want))
	}
	for i := range want {
		if users[i] != want[i] {
			t.Errorf("users[%d] = %+v, want %+v", i, users[i], want[i])
		}
	}

	if _, err := parseDSCLUsers([]byte("not a plist")); err == nil {
		t.Error("parseDSCLUsers(invalid) error = nil, want error")
	}
}

func TestLiveSource_UsersCached(t *testing.T) {
	src := &Source{users: []macfor.User{{Username: "alice", UID: 501}}, usersLoaded: true}

	first, err := src.Users()
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}
	first[0].Username = "modified"

	second, err := src.Users()
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}
	if len(second) != 1 || second[0].Username != "alice" {
		t.Errorf("second Users() = %+v, want cached alice unaffected by caller changes", second)
	}
}
