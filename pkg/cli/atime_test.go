package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/macfor-io/macfor-community/pkg/macfor/atime"
)

func TestWarnIfAccessTimesMayChange(t *testing.T) {
	t.Cleanup(func() { quiet = false; collectJSON = false })

	data := atime.Volume{MountPoint: "/System/Volumes/Data", FSType: "apfs", Policy: atime.PolicyLazy}
	system := atime.Volume{MountPoint: "/", FSType: "apfs", Policy: atime.PolicyNone}
	strict := atime.Volume{MountPoint: "/Volumes/Evidence", FSType: "apfs", Policy: atime.PolicyStrict}

	tests := []struct {
		name        string
		vols        []atime.Volume
		quiet, json bool
		wantWarning bool
		wantLines   []string
		notWant     []string
	}{
		{name: "lazy data volume", vols: []atime.Volume{system, data}, wantWarning: true,
			wantLines: []string{"/System/Volumes/Data (apfs, default lazy access-time updates)", "chain-of-custody.json"},
			notWant:   []string{"    / ("}},
		{name: "strict volume", vols: []atime.Volume{strict}, wantWarning: true,
			wantLines: []string{"strictatime: every read updates"}},
		{name: "read-only only", vols: []atime.Volume{system}},
		{name: "no volumes", vols: nil},
		{name: "lazy but quiet", vols: []atime.Volume{data}, quiet: true},
		{name: "lazy but json", vols: []atime.Volume{data}, json: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quiet = tt.quiet
			collectJSON = tt.json
			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetErr(&buf)

			warnIfAccessTimesMayChange(cmd, tt.vols)

			out := buf.String()
			if got := strings.Contains(out, "access times"); got != tt.wantWarning {
				t.Fatalf("warning printed = %v, want %v (output %q)", got, tt.wantWarning, out)
			}
			for _, l := range tt.wantLines {
				if !strings.Contains(out, l) {
					t.Errorf("output missing %q:\n%s", l, out)
				}
			}
			for _, l := range tt.notWant {
				if strings.Contains(out, l) {
					t.Errorf("output should not contain %q:\n%s", l, out)
				}
			}
		})
	}
}
