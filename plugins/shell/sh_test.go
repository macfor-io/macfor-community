package shell

import (
	"testing"
	"time"
)

// Test constants to satisfy goconst linter.
const (
	testTypeShellCommand = "shell_command"
	testUserAdmin        = "admin"
	testCmdLsLa          = "ls -la"
)

func TestParseShHistory_BasicCommands(t *testing.T) {
	content := `ls -la
cd /var/log
cat messages`

	records, warnings := parseShHistory(content, testUserAdmin, "/Users/admin/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Verify first record
	if records[0].Type != testTypeShellCommand {
		t.Errorf("expected type %q, got %q", testTypeShellCommand, records[0].Type)
	}
	if records[0].User != testUserAdmin {
		t.Errorf("expected user %q, got %q", testUserAdmin, records[0].User)
	}
	if records[0].Shell != "sh" {
		t.Errorf("expected shell 'sh', got %q", records[0].Shell)
	}
	if records[0].Command != testCmdLsLa {
		t.Errorf("expected command %q, got %q", testCmdLsLa, records[0].Command)
	}
	if records[0].LineNumber != 1 {
		t.Errorf("expected line number 1, got %d", records[0].LineNumber)
	}
	if records[0].SourceFile != "/Users/admin/.sh_history" {
		t.Errorf("expected source file '/Users/admin/.sh_history', got %q", records[0].SourceFile)
	}

	// Sh does not support timestamps
	if records[0].Timestamp != nil {
		t.Error("expected nil timestamp for sh record")
	}

	// Verify other records
	if records[1].Command != "cd /var/log" {
		t.Errorf("expected 'cd /var/log', got %q", records[1].Command)
	}
	if records[2].Command != "cat messages" {
		t.Errorf("expected 'cat messages', got %q", records[2].Command)
	}
}

func TestParseShHistory_EmptyLines(t *testing.T) {
	content := `ls

cd /tmp

pwd
`

	records, warnings := parseShHistory(content, testUserAdmin, "/Users/admin/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	if len(records) != 3 {
		t.Errorf("expected 3 records (skipping empty lines), got %d", len(records))
	}

	// Check line numbers account for empty lines
	if records[0].LineNumber != 1 {
		t.Errorf("expected line number 1, got %d", records[0].LineNumber)
	}
	if records[1].LineNumber != 3 {
		t.Errorf("expected line number 3, got %d", records[1].LineNumber)
	}
	if records[2].LineNumber != 5 {
		t.Errorf("expected line number 5, got %d", records[2].LineNumber)
	}
}

func TestParseShHistory_WhitespaceLines(t *testing.T) {
	content := `ls


pwd`

	records, warnings := parseShHistory(content, testUserAdmin, "/Users/admin/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	if len(records) != 2 {
		t.Errorf("expected 2 records (skipping whitespace lines), got %d", len(records))
	}
}

func TestParseShHistory_SpecialCharacters(t *testing.T) {
	content := `echo "hello world"
grep 'pattern' file.txt
awk '{print $1}' data.csv
cat file | sort | uniq -c
LANG=en_US.UTF-8 ./script.sh`

	records, warnings := parseShHistory(content, testUserAdmin, "/Users/admin/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	if len(records) != 5 {
		t.Fatalf("expected 5 records, got %d", len(records))
	}

	expected := []string{
		`echo "hello world"`,
		`grep 'pattern' file.txt`,
		`awk '{print $1}' data.csv`,
		`cat file | sort | uniq -c`,
		`LANG=en_US.UTF-8 ./script.sh`,
	}

	for i, exp := range expected {
		if records[i].Command != exp {
			t.Errorf("record %d: expected %q, got %q", i, exp, records[i].Command)
		}
	}
}

func TestParseShHistory_Unicode(t *testing.T) {
	content := `echo "こんにちは"
echo "مرحبا"
echo "🎉 success"`

	records, warnings := parseShHistory(content, testUserAdmin, "/Users/admin/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	if records[0].Command != `echo "こんにちは"` {
		t.Errorf("expected Japanese characters, got %q", records[0].Command)
	}
	if records[1].Command != `echo "مرحبا"` {
		t.Errorf("expected Arabic characters, got %q", records[1].Command)
	}
	if records[2].Command != `echo "🎉 success"` {
		t.Errorf("expected emoji, got %q", records[2].Command)
	}
}

func TestParseShHistory_EmptyContent(t *testing.T) {
	records, warnings := parseShHistory("", testUserAdmin, "/Users/admin/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records for empty content, got %d", len(records))
	}
}

func TestParseShHistory_OnlyWhitespace(t *testing.T) {
	content := `



`

	records, warnings := parseShHistory(content, testUserAdmin, "/Users/admin/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records for whitespace-only content, got %d", len(records))
	}
}

func TestParseShHistory_NoTimestamps(t *testing.T) {
	// Sh does not support timestamps like bash does with HISTTIMEFORMAT.
	// Lines starting with # should be treated as regular commands in sh.
	content := `#1706012400
` + testCmdLsLa + `
#comment_looking_like_timestamp
pwd`

	records, warnings := parseShHistory(content, testUserAdmin, "/Users/admin/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	// All lines including those starting with # should be commands
	if len(records) != 4 {
		t.Fatalf("expected 4 records, got %d", len(records))
	}

	// Verify # lines are treated as commands, not timestamps
	if records[0].Command != "#1706012400" {
		t.Errorf("expected '#1706012400', got %q", records[0].Command)
	}
	if records[0].Timestamp != nil {
		t.Error("expected nil timestamp for sh record")
	}
}

func TestShRecord_Fields(t *testing.T) {
	now := time.Now()
	record := ShRecord{
		Type:       testTypeShellCommand,
		User:       "testuser",
		Shell:      "sh",
		Command:    "test command",
		Timestamp:  &now,
		LineNumber: 42,
		SourceFile: "/path/to/file",
	}

	if record.Type != testTypeShellCommand {
		t.Errorf("unexpected Type: %s", record.Type)
	}
	if record.User != "testuser" {
		t.Errorf("unexpected User: %s", record.User)
	}
	if record.Shell != "sh" {
		t.Errorf("unexpected Shell: %s", record.Shell)
	}
	if record.Command != "test command" {
		t.Errorf("unexpected Command: %s", record.Command)
	}
	if record.LineNumber != 42 {
		t.Errorf("unexpected LineNumber: %d", record.LineNumber)
	}
	if record.SourceFile != "/path/to/file" {
		t.Errorf("unexpected SourceFile: %s", record.SourceFile)
	}
}
