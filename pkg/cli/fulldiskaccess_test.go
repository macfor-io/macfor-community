package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestWarnIfNoFullDiskAccess(t *testing.T) {
	orig := fullDiskAccess
	t.Cleanup(func() { fullDiskAccess = orig; quiet = false; collectJSON = false })

	tests := []struct {
		name           string
		granted, known bool
		quiet, json    bool
		wantWarning    bool
	}{
		{"denied", false, true, false, false, true},
		{"granted", true, true, false, false, false},
		{"unknown", false, false, false, false, false},
		{"denied but quiet", false, true, true, false, false},
		{"denied but json", false, true, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullDiskAccess = func() (bool, bool) { return tt.granted, tt.known }
			quiet = tt.quiet
			collectJSON = tt.json
			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetErr(&buf)

			warnIfNoFullDiskAccess(cmd)

			if got := strings.Contains(buf.String(), "Full Disk Access"); got != tt.wantWarning {
				t.Errorf("warning printed = %v, want %v (output %q)", got, tt.wantWarning, buf.String())
			}
		})
	}
}
