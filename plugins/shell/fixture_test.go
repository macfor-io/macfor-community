package shell

import (
	"strings"
	"testing"
)

// Tests that use the testdata fixtures to verify parsing behavior.

func TestParseBashHistory_FromFixture_Basic(t *testing.T) {
	content := mustLoadFixture(FixtureBashBasic)
	records, warnings := parseBashHistory(content, "testuser", "/test/.bash_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 7 {
		t.Errorf("expected 7 records, got %d", len(records))
	}

	// Verify first and last commands
	if records[0].Command != "ls -la" {
		t.Errorf("expected first command 'ls -la', got %q", records[0].Command)
	}
	if records[len(records)-1].Command != `echo "hello world"` {
		t.Errorf("expected last command 'echo \"hello world\"', got %q", records[len(records)-1].Command)
	}
}

func TestParseBashHistory_FromFixture_Timestamped(t *testing.T) {
	content := mustLoadFixture(FixtureBashTimestamped)
	records, warnings := parseBashHistory(content, "testuser", "/test/.bash_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}

	// All records should have timestamps
	for i, rec := range records {
		if rec.Timestamp == nil {
			t.Errorf("record %d: expected timestamp", i)
		}
	}

	// First record should be "ls -la" with timestamp 1706012400
	if records[0].Command != "ls -la" {
		t.Errorf("expected first command 'ls -la', got %q", records[0].Command)
	}
	if records[0].Timestamp != nil && records[0].Timestamp.Unix() != 1706012400 {
		t.Errorf("expected timestamp 1706012400, got %d", records[0].Timestamp.Unix())
	}
}

func TestParseBashHistory_FromFixture_Unicode(t *testing.T) {
	content := mustLoadFixture(FixtureBashUnicode)
	records, warnings := parseBashHistory(content, "testuser", "/test/.bash_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	// Should preserve unicode characters
	found := map[string]bool{
		"日本語テスト":     false,
		"Привет мир": false,
		"emoji":      false,
		"中文":         false,
	}

	for _, rec := range records {
		for key := range found {
			if strings.Contains(rec.Command, key) {
				found[key] = true
			}
		}
	}

	for key, wasFound := range found {
		if !wasFound {
			t.Errorf("expected to find %q in parsed records", key)
		}
	}
}

func TestParseBashHistory_FromFixture_Special(t *testing.T) {
	content := mustLoadFixture(FixtureBashSpecial)
	records, warnings := parseBashHistory(content, "testuser", "/test/.bash_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	// Should handle pipes, redirects, quotes, etc.
	if len(records) == 0 {
		t.Fatal("expected records")
	}

	// Check that pipe command is preserved
	foundPipe := false
	foundRedirect := false
	for _, rec := range records {
		if strings.Contains(rec.Command, "| grep") {
			foundPipe = true
		}
		if strings.Contains(rec.Command, ">>") {
			foundRedirect = true
		}
	}

	if !foundPipe {
		t.Error("expected to find pipe command")
	}
	if !foundRedirect {
		t.Error("expected to find redirect command")
	}
}

func TestParseBashHistory_FromFixture_LongCommand(t *testing.T) {
	content := mustLoadFixture(FixtureBashLongCmd)
	records, warnings := parseBashHistory(content, "testuser", "/test/.bash_history")

	// Currently, bufio.Scanner has a max token size of 64KB.
	// Lines exceeding this limit will generate a warning and be skipped.
	// This test documents the current behavior - very long lines are not fully supported.
	if len(warnings) == 0 {
		// If we get here, the implementation was updated to handle long lines
		t.Log("Note: Long line handling has been implemented")
	} else {
		// Current expected behavior: scanner token too long error
		foundScannerError := false
		for _, w := range warnings {
			if strings.Contains(w, "token too long") || strings.Contains(w, "scanner") {
				foundScannerError = true
				break
			}
		}
		if !foundScannerError {
			t.Errorf("expected scanner error for long line, got: %v", warnings)
		}
	}

	// Short commands before/after the long one should still be parsed
	if len(records) < 2 {
		t.Errorf("expected at least 2 records (short commands), got %d", len(records))
	}

	// Verify short commands are still collected
	foundShort := false
	for _, rec := range records {
		if rec.Command == "ls -la" || rec.Command == "pwd" {
			foundShort = true
			break
		}
	}
	if !foundShort {
		t.Error("expected to find short commands in records")
	}
}

func TestParseZshHistory_FromFixture_Extended(t *testing.T) {
	content := mustLoadFixture(FixtureZshExtended)
	records, warnings := parseZshHistory(content, "testuser", "/test/.zsh_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}

	// All records should have timestamps
	for i, rec := range records {
		if rec.Timestamp == nil {
			t.Errorf("record %d: expected timestamp", i)
		}
	}

	// Check duration is parsed (third record should have duration 5)
	if records[2].Duration != 5 {
		t.Errorf("expected duration 5 for third record, got %d", records[2].Duration)
	}
}

func TestParseZshHistory_FromFixture_Multiline(t *testing.T) {
	content := mustLoadFixture(FixtureZshMultiline)
	records, warnings := parseZshHistory(content, "testuser", "/test/.zsh_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	// Should have 3 commands (multiline commands merged)
	if len(records) != 3 {
		t.Errorf("expected 3 records (multiline merged), got %d", len(records))
	}

	// First command should contain continuation
	if !strings.Contains(records[0].Command, "continuation line 1") {
		t.Errorf("expected first command to contain continuation, got %q", records[0].Command)
	}
}

func TestParseZshHistory_FromFixture_LongCommand(t *testing.T) {
	content := mustLoadFixture(FixtureZshLongCmd)
	records, warnings := parseZshHistory(content, "testuser", "/test/.zsh_history")

	// Currently, bufio.Scanner has a max token size of 64KB.
	// Lines exceeding this limit will generate a warning and be skipped.
	// This test documents the current behavior - very long lines are not fully supported.
	if len(warnings) == 0 {
		t.Log("Note: Long line handling has been implemented")
	} else {
		foundScannerError := false
		for _, w := range warnings {
			if strings.Contains(w, "token too long") || strings.Contains(w, "scanner") {
				foundScannerError = true
				break
			}
		}
		if !foundScannerError {
			t.Errorf("expected scanner error for long line, got: %v", warnings)
		}
	}

	// Short commands before/after the long one should still be parsed
	if len(records) < 1 {
		t.Errorf("expected at least 1 record (short commands), got %d", len(records))
	}
}

func TestParseFishHistory_FromFixture(t *testing.T) {
	content := mustLoadFixture(FixtureFish)
	records, warnings := parseFishHistory(content, "testuser", "/test/fish_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}

	// Check paths are preserved
	foundPaths := false
	for _, rec := range records {
		if len(rec.Paths) > 0 {
			foundPaths = true
			break
		}
	}
	if !foundPaths {
		t.Error("expected to find records with paths")
	}
}

func TestParseFishHistory_FromFixture_LongCommand(t *testing.T) {
	content := mustLoadFixture(FixtureFishLongCmd)
	records, warnings := parseFishHistory(content, "testuser", "/test/fish_history")

	// Currently, bufio.Scanner has a max token size of 64KB.
	// Lines exceeding this limit will generate a warning and be skipped.
	// This test documents the current behavior - very long lines are not fully supported.
	if len(warnings) == 0 {
		t.Log("Note: Long line handling has been implemented")
	} else {
		foundScannerError := false
		for _, w := range warnings {
			if strings.Contains(w, "token too long") || strings.Contains(w, "scanner") {
				foundScannerError = true
				break
			}
		}
		if !foundScannerError {
			t.Errorf("expected scanner error for long line, got: %v", warnings)
		}
	}

	// Short commands before/after the long one should still be parsed
	if len(records) < 1 {
		t.Errorf("expected at least 1 record (short commands), got %d", len(records))
	}
}

func TestParseShHistory_FromFixture_Basic(t *testing.T) {
	content := mustLoadFixture(FixtureShBasic)
	records, warnings := parseShHistory(content, "testuser", "/test/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 7 {
		t.Errorf("expected 7 records, got %d", len(records))
	}

	// Sh does not support timestamps
	for i, rec := range records {
		if rec.Timestamp != nil {
			t.Errorf("record %d: sh should not have timestamps", i)
		}
	}
}

func TestParseShHistory_FromFixture_Unicode(t *testing.T) {
	content := mustLoadFixture(FixtureShUnicode)
	records, warnings := parseShHistory(content, "testuser", "/test/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	// Should preserve unicode characters
	found := map[string]bool{
		"日本語テスト":     false,
		"Привет мир": false,
		"emoji":      false,
		"中文":         false,
	}

	for _, rec := range records {
		for key := range found {
			if strings.Contains(rec.Command, key) {
				found[key] = true
			}
		}
	}

	for key, wasFound := range found {
		if !wasFound {
			t.Errorf("expected to find %q in parsed records", key)
		}
	}
}

func TestParseBashHistory_FromFixture_Empty(t *testing.T) {
	content := mustLoadFixture(FixtureEmpty)
	records, warnings := parseBashHistory(content, "testuser", "/test/.bash_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records for empty file, got %d", len(records))
	}
}

func TestParseZshHistory_FromFixture_Empty(t *testing.T) {
	content := mustLoadFixture(FixtureEmpty)
	records, warnings := parseZshHistory(content, "testuser", "/test/.zsh_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records for empty file, got %d", len(records))
	}
}

func TestParseFishHistory_FromFixture_Empty(t *testing.T) {
	content := mustLoadFixture(FixtureEmpty)
	records, warnings := parseFishHistory(content, "testuser", "/test/fish_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records for empty file, got %d", len(records))
	}
}

func TestParseShHistory_FromFixture_Empty(t *testing.T) {
	content := mustLoadFixture(FixtureEmpty)
	records, warnings := parseShHistory(content, "testuser", "/test/.sh_history")

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records for empty file, got %d", len(records))
	}
}

func TestParseBashHistory_FromFixture_BinaryGarbage(t *testing.T) {
	content := mustLoadFixture(FixtureBinaryGarbage)
	// Binary content should not panic and should handle gracefully.
	// May produce some "records" from arbitrary binary data treated as lines.
	records, _ := parseBashHistory(content, "testuser", "/test/.bash_history")
	t.Logf("binary garbage produced %d bash records (expected: graceful handling)", len(records))
}

func TestParseZshHistory_FromFixture_BinaryGarbage(t *testing.T) {
	content := mustLoadFixture(FixtureBinaryGarbage)
	// Binary content should not panic and should handle gracefully.
	// May produce some "records" from arbitrary binary data treated as lines.
	records, _ := parseZshHistory(content, "testuser", "/test/.zsh_history")
	t.Logf("binary garbage produced %d zsh records (expected: graceful handling)", len(records))
}

func TestParseFishHistory_FromFixture_BinaryGarbage(t *testing.T) {
	content := mustLoadFixture(FixtureBinaryGarbage)
	// Binary content should not panic and should handle gracefully.
	// May produce some "records" from arbitrary binary data treated as lines.
	records, _ := parseFishHistory(content, "testuser", "/test/fish_history")
	t.Logf("binary garbage produced %d fish records (expected: graceful handling)", len(records))
}

func TestParseShHistory_FromFixture_BinaryGarbage(t *testing.T) {
	content := mustLoadFixture(FixtureBinaryGarbage)
	// Binary content should not panic and should handle gracefully.
	// May produce some "records" from arbitrary binary data treated as lines.
	records, _ := parseShHistory(content, "testuser", "/test/.sh_history")
	t.Logf("binary garbage produced %d sh records (expected: graceful handling)", len(records))
}
