package macfor_test

import (
	"errors"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

func TestLogLevelString(t *testing.T) {
	tests := []struct {
		level macfor.LogLevel
		want  string
	}{
		{macfor.LogDebug, "DEBUG"},
		{macfor.LogInfo, "INFO"},
		{macfor.LogWarn, "WARN"},
		{macfor.LogError, "ERROR"},
		{macfor.LogLevel(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.want {
			t.Errorf("LogLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestSourceTypeConstants(t *testing.T) {
	if macfor.SourceTypeLive != "live" {
		t.Errorf("SourceTypeLive = %q, want %q", macfor.SourceTypeLive, "live")
	}
	if macfor.SourceTypeImage != "image" {
		t.Errorf("SourceTypeImage = %q, want %q", macfor.SourceTypeImage, "image")
	}
}

func TestCollectionError(t *testing.T) {
	underlying := errors.New("file not found")
	artifact := macfor.Artifact{
		PluginID:   "shell.history",
		SourcePath: "/Users/admin/.bash_history",
	}

	err := macfor.NewCollectionError(artifact, "read", underlying)

	// Test error message format
	want := "read /Users/admin/.bash_history: file not found"
	if err.Error() != want {
		t.Errorf("CollectionError.Error() = %q, want %q", err.Error(), want)
	}

	// Test unwrap
	if !errors.Is(err, underlying) {
		t.Error("errors.Is(CollectionError, underlying) = false, want true")
	}
}

func TestCollectionErrorEmptyPath(t *testing.T) {
	underlying := errors.New("unknown error")
	artifact := macfor.Artifact{
		PluginID: "shell.history",
	}

	err := macfor.NewCollectionError(artifact, "collect", underlying)

	want := "collect: unknown error"
	if err.Error() != want {
		t.Errorf("CollectionError.Error() = %q, want %q", err.Error(), want)
	}
}

func TestParseError(t *testing.T) {
	tests := []struct {
		name    string
		err     *macfor.ParseError
		want    string
		wantErr error
	}{
		{
			name: "with line number",
			err: &macfor.ParseError{
				File:    "/path/to/file",
				Line:    42,
				Message: "unexpected token",
			},
			want:    "parse /path/to/file:42: unexpected token",
			wantErr: macfor.ErrParseError,
		},
		{
			name: "with offset",
			err: &macfor.ParseError{
				File:    "/path/to/file",
				Offset:  1024,
				Message: "invalid byte",
			},
			want:    "parse /path/to/file (offset 1024): invalid byte",
			wantErr: macfor.ErrParseError,
		},
		{
			name: "no line or offset",
			err: &macfor.ParseError{
				File:    "/path/to/file",
				Message: "corrupted data",
			},
			want:    "parse /path/to/file: corrupted data",
			wantErr: macfor.ErrParseError,
		},
		{
			name: "with underlying error",
			err: &macfor.ParseError{
				File:    "/path/to/file",
				Message: "invalid JSON",
				Err:     macfor.ErrCorrupted,
			},
			want:    "parse /path/to/file: invalid JSON",
			wantErr: macfor.ErrCorrupted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("ParseError.Error() = %q, want %q", got, tt.want)
			}
			if !errors.Is(tt.err, tt.wantErr) {
				t.Errorf("errors.Is(ParseError, %v) = false, want true", tt.wantErr)
			}
		})
	}
}

func TestNewParseError(t *testing.T) {
	err := macfor.NewParseError("/test/file", 10, "syntax error")

	if err.File != "/test/file" {
		t.Errorf("ParseError.File = %q, want %q", err.File, "/test/file")
	}
	if err.Line != 10 {
		t.Errorf("ParseError.Line = %d, want %d", err.Line, 10)
	}
	if err.Message != "syntax error" {
		t.Errorf("ParseError.Message = %q, want %q", err.Message, "syntax error")
	}
}

func TestWrapPermissionDenied(t *testing.T) {
	underlying := errors.New("operation not permitted")
	err := macfor.WrapPermissionDenied("/private/file", underlying)

	if !errors.Is(err, macfor.ErrPermissionDenied) {
		t.Error("errors.Is(wrapped, ErrPermissionDenied) = false, want true")
	}
	if !errors.Is(err, underlying) {
		t.Error("errors.Is(wrapped, underlying) = false, want true")
	}
}

func TestWrapNotFound(t *testing.T) {
	underlying := errors.New("no such file or directory")
	err := macfor.WrapNotFound("/missing/file", underlying)

	if !errors.Is(err, macfor.ErrNotFound) {
		t.Error("errors.Is(wrapped, ErrNotFound) = false, want true")
	}
	if !errors.Is(err, underlying) {
		t.Error("errors.Is(wrapped, underlying) = false, want true")
	}
}

func TestWrapIOError(t *testing.T) {
	underlying := errors.New("disk full")
	err := macfor.WrapIOError("write", "/tmp/output", underlying)

	if !errors.Is(err, macfor.ErrIOError) {
		t.Error("errors.Is(wrapped, ErrIOError) = false, want true")
	}
	if !errors.Is(err, underlying) {
		t.Error("errors.Is(wrapped, underlying) = false, want true")
	}
}

func TestSentinelErrors(t *testing.T) {
	// Verify all sentinel errors are distinct
	sentinels := []error{
		macfor.ErrPermissionDenied,
		macfor.ErrNotFound,
		macfor.ErrParseError,
		macfor.ErrIOError,
		macfor.ErrUnsupportedVersion,
		macfor.ErrCorrupted,
	}

	for i, err1 := range sentinels {
		for j, err2 := range sentinels {
			if i != j && errors.Is(err1, err2) {
				t.Errorf("sentinel errors %d and %d should be distinct", i, j)
			}
		}
	}
}
