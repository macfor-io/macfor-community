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

func TestWriteAccessTimeSummary(t *testing.T) {
	s := &atime.Summary{ObservedReads: 1204, Modified: 37, LikelyCollector: 35, ConcurrentActivity: 2}

	var buf bytes.Buffer
	writeAccessTimeSummary(&buf, s, false)
	want := "37 of 1204 observed reads changed an access time (35 likely caused by macfor, 2 during concurrent activity)"
	if !strings.Contains(buf.String(), want) || !strings.Contains(buf.String(), "chain-of-custody.json") {
		t.Errorf("summary = %q, want it to contain %q", buf.String(), want)
	}

	buf.Reset()
	writeAccessTimeSummary(&buf, s, true)
	if !strings.Contains(buf.String(), "not recorded") {
		t.Errorf("detection-only summary = %q, want a note that changes are not recorded", buf.String())
	}

	for _, quietCase := range []*atime.Summary{nil, {ObservedReads: 5}} {
		buf.Reset()
		writeAccessTimeSummary(&buf, quietCase, false)
		if buf.Len() != 0 {
			t.Errorf("summary for %+v = %q, want nothing", quietCase, buf.String())
		}
	}
}

func TestReportAccessTimesSilentWithQuietOrJSON(t *testing.T) {
	t.Cleanup(func() { quiet = false; collectJSON = false })
	s := &atime.Summary{ObservedReads: 2, Modified: 1, LikelyCollector: 1}
	for _, tc := range []struct{ quiet, json bool }{{true, false}, {false, true}} {
		quiet, collectJSON = tc.quiet, tc.json
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetErr(&buf)
		reportAccessTimes(cmd, s, false)
		if buf.Len() != 0 {
			t.Errorf("quiet=%v json=%v printed %q", tc.quiet, tc.json, buf.String())
		}
	}
}

func TestNewAccessTimeResult(t *testing.T) {
	if newAccessTimeResult(nil) != nil {
		t.Error("nil summary should give nil result")
	}
	r := newAccessTimeResult(&atime.Summary{ObservedReads: 3, Modified: 2, LikelyCollector: 1, ConcurrentActivity: 1})
	if r == nil || r.ObservedReads != 3 || r.Modified != 2 || r.ConcurrentActivity != 1 {
		t.Errorf("result = %+v", r)
	}
}
