package macfor_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

func TestNewArtifact(t *testing.T) {
	tests := []struct {
		name         string
		pluginID     string
		category     string
		artifactType string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "valid artifact",
			pluginID:     "shell.history",
			category:     "shell",
			artifactType: "history",
			wantErr:      false,
		},
		{
			name:         "empty plugin ID",
			pluginID:     "",
			category:     "shell",
			artifactType: "history",
			wantErr:      true,
			errContains:  "PluginID is required",
		},
		{
			name:         "plugin ID without dot",
			pluginID:     "shellhistory",
			category:     "shell",
			artifactType: "history",
			wantErr:      true,
			errContains:  "category.name",
		},
		{
			name:         "empty category",
			pluginID:     "shell.history",
			category:     "",
			artifactType: "history",
			wantErr:      true,
			errContains:  "Category is required",
		},
		{
			name:         "empty type",
			pluginID:     "shell.history",
			category:     "shell",
			artifactType: "",
			wantErr:      true,
			errContains:  "Type is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := macfor.NewArtifact(tt.pluginID, tt.category, tt.artifactType)

			if tt.wantErr {
				if err == nil {
					t.Error("NewArtifact() error = nil, want error")
				} else if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("NewArtifact() error = %q, want error containing %q", err.Error(), tt.errContains)
				}
				if a != nil {
					t.Error("NewArtifact() returned non-nil artifact on error")
				}
				return
			}

			if err != nil {
				t.Errorf("NewArtifact() unexpected error = %v", err)
				return
			}

			if a.PluginID != tt.pluginID {
				t.Errorf("Artifact.PluginID = %q, want %q", a.PluginID, tt.pluginID)
			}
			if a.Category != tt.category {
				t.Errorf("Artifact.Category = %q, want %q", a.Category, tt.category)
			}
			if a.Type != tt.artifactType {
				t.Errorf("Artifact.Type = %q, want %q", a.Type, tt.artifactType)
			}
			if a.CollectedAt.IsZero() {
				t.Error("Artifact.CollectedAt should be set to current time")
			}
			if a.Metadata == nil {
				t.Error("Artifact.Metadata should be initialized")
			}
		})
	}
}

func TestArtifactValidate(t *testing.T) {
	tests := []struct {
		name        string
		artifact    macfor.Artifact
		wantErr     bool
		errContains string
	}{
		{
			name: "valid artifact",
			artifact: macfor.Artifact{
				PluginID: "shell.history",
				Category: "shell",
				Type:     "history",
			},
			wantErr: false,
		},
		{
			name: "missing PluginID",
			artifact: macfor.Artifact{
				Category: "shell",
				Type:     "history",
			},
			wantErr:     true,
			errContains: "PluginID is required",
		},
		{
			name: "PluginID without dot separator",
			artifact: macfor.Artifact{
				PluginID: "shellhistory",
				Category: "shell",
				Type:     "history",
			},
			wantErr:     true,
			errContains: "category.name",
		},
		{
			name: "missing Category",
			artifact: macfor.Artifact{
				PluginID: "shell.history",
				Type:     "history",
			},
			wantErr:     true,
			errContains: "Category is required",
		},
		{
			name: "missing Type",
			artifact: macfor.Artifact{
				PluginID: "shell.history",
				Category: "shell",
			},
			wantErr:     true,
			errContains: "Type is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.artifact.Validate()

			if tt.wantErr {
				if err == nil {
					t.Error("Validate() error = nil, want error")
				} else {
					if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
						t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.errContains)
					}
					if !errors.Is(err, macfor.ErrParseError) {
						t.Error("Validate() error should wrap ErrParseError")
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestArtifactContainerPath(t *testing.T) {
	tests := []struct {
		name     string
		artifact macfor.Artifact
		want     string
	}{
		{
			name: "user artifact with source path",
			artifact: macfor.Artifact{
				PluginID:   "shell.history",
				Category:   "shell",
				Type:       "history",
				SourcePath: "/Users/admin/.bash_history",
				Username:   "admin",
			},
			want: filepath.Join("artifacts", "shell", "shell.history", "files", "admin", ".bash_history"),
		},
		{
			name: "system-level artifact",
			artifact: macfor.Artifact{
				PluginID:   "system.logs",
				Category:   "system",
				Type:       "syslog",
				SourcePath: "/var/log/system.log",
				Username:   "",
			},
			want: filepath.Join("artifacts", "system", "system.logs", "files", "system", "system.log"),
		},
		{
			name: "artifact without source path",
			artifact: macfor.Artifact{
				PluginID: "browser.history",
				Category: "browser",
				Type:     "history",
				Username: "user1",
			},
			want: filepath.Join("artifacts", "browser", "browser.history", "files", "user1", "history"),
		},
		{
			name: "artifact with deep source path",
			artifact: macfor.Artifact{
				PluginID:   "fish.history",
				Category:   "shell",
				Type:       "history",
				SourcePath: "/Users/dev/.local/share/fish/fish_history",
				Username:   "dev",
			},
			want: filepath.Join("artifacts", "shell", "fish.history", "files", "dev", "fish_history"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.artifact.ContainerPath()
			if got != tt.want {
				t.Errorf("ContainerPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestArtifactRecordPath(t *testing.T) {
	tests := []struct {
		name     string
		artifact macfor.Artifact
		want     string
	}{
		{
			name: "shell history",
			artifact: macfor.Artifact{
				PluginID: "shell.history",
				Category: "shell",
				Type:     "history",
			},
			want: filepath.Join("records", "shell.history", "parsed.jsonl"),
		},
		{
			name: "browser cookies",
			artifact: macfor.Artifact{
				PluginID: "browser.cookies",
				Category: "browser",
				Type:     "cookies",
			},
			want: filepath.Join("records", "browser.cookies", "parsed.jsonl"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.artifact.RecordPath()
			if got != tt.want {
				t.Errorf("RecordPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestArtifactBuilderMethods(t *testing.T) {
	a, err := macfor.NewArtifact("shell.history", "shell", "history")
	if err != nil {
		t.Fatalf("NewArtifact() error = %v", err)
	}

	modTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// Test chaining
	result := a.
		WithSourcePath("/Users/test/.bash_history").
		WithUsername("test").
		WithDescription("Bash shell history").
		WithOriginalFile(modTime, 12345).
		WithMetadata("shell", "bash").
		WithMetadata("format", "plaintext")

	// Verify chaining returns same pointer
	if result != a {
		t.Error("Builder methods should return same artifact pointer")
	}

	// Verify values
	if a.SourcePath != "/Users/test/.bash_history" {
		t.Errorf("SourcePath = %q, want %q", a.SourcePath, "/Users/test/.bash_history")
	}
	if a.Username != "test" {
		t.Errorf("Username = %q, want %q", a.Username, "test")
	}
	if a.Description != "Bash shell history" {
		t.Errorf("Description = %q, want %q", a.Description, "Bash shell history")
	}
	if !a.OriginalModTime.Equal(modTime) {
		t.Errorf("OriginalModTime = %v, want %v", a.OriginalModTime, modTime)
	}
	if a.OriginalSize != 12345 {
		t.Errorf("OriginalSize = %d, want %d", a.OriginalSize, 12345)
	}
	if a.Metadata["shell"] != "bash" {
		t.Errorf("Metadata[shell] = %v, want %q", a.Metadata["shell"], "bash")
	}
	if a.Metadata["format"] != "plaintext" {
		t.Errorf("Metadata[format] = %v, want %q", a.Metadata["format"], "plaintext")
	}
}

func TestArtifactWithMetadataNilMap(t *testing.T) {
	// Test WithMetadata initializes nil map
	a := &macfor.Artifact{
		PluginID: "test.plugin",
		Category: "test",
		Type:     "test",
		Metadata: nil,
	}

	a.WithMetadata("key", "value")

	if a.Metadata == nil {
		t.Error("WithMetadata should initialize nil Metadata map")
	}
	if a.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %v, want %q", a.Metadata["key"], "value")
	}
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
