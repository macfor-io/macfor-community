package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/macfor-io/macfor-community/pkg/macfor/atime"
)

// accessTimeState is the access-time state a collect command shares across
// its detection and collection phases: the source's volumes, and the tracker
// observing plugin reads (nil when no volume may update access times). One
// tracker spans both phases, so reads made by the detection-only pass,
// before the evidence container exists, are recorded in it too.
type accessTimeState struct {
	volumes []atime.Volume
	tracker *atime.Tracker
}

// newAccessTimeState returns the state for a source with the given volumes.
func newAccessTimeState(vols []atime.Volume) accessTimeState {
	st := accessTimeState{volumes: vols}
	if atime.AnyMayUpdate(vols) {
		st.tracker = atime.NewTracker()
	}
	return st
}

// probeVolumes probes the volumes behind the source for their access-time
// policies. It is a variable so tests can replace the probe.
var probeVolumes = atime.ProbeSource

// warnIfAccessTimesMayChange warns on stderr, before any artifact is read,
// when reading files on one of the source's volumes can update their access
// times. Like the Full Disk Access warning it is silent with --quiet or
// --json; the manifest records source.volumes either way.
func warnIfAccessTimesMayChange(cmd *cobra.Command, vols []atime.Volume) {
	if quiet || collectJSON {
		return
	}
	writeAccessTimeWarning(cmd.ErrOrStderr(), vols)
}

// writeAccessTimeWarning writes the access-time warning for vols to w. It
// writes nothing when no volume may update access times.
func writeAccessTimeWarning(w io.Writer, vols []atime.Volume) {
	if !atime.AnyMayUpdate(vols) {
		return
	}
	_, _ = fmt.Fprintln(w, "Warning: reading files may update their access times on:")
	for _, v := range vols {
		if v.Policy.MayUpdate() {
			_, _ = fmt.Fprintf(w, "    %s (%s, %s)\n", v.MountPoint, v.FSType, describePolicy(v.Policy))
		}
	}
	_, _ = fmt.Fprint(w, `  macfor never resets access times. Each change it observes is recorded in
  chain-of-custody.json with the file's stat before and after the read.
  To avoid changes, collect from a disk image (--image) or a read-only mount (--from).

`)
}

// describePolicy describes an access-time policy for the warning.
func describePolicy(p atime.Policy) string {
	switch p {
	case atime.PolicyLazy:
		return "default lazy access-time updates"
	case atime.PolicyStrict:
		return "strictatime: every read updates the access time"
	case atime.PolicyNone:
		return "read-only or noatime: reads never update access times"
	case atime.PolicyUnknown:
		return "access-time policy unknown"
	default:
		return string(p)
	}
}

// AccessTimeResult totals the observed reads of a collection in --json
// output.
type AccessTimeResult struct {
	ObservedReads      int `json:"observed_reads"`
	Modified           int `json:"modified"`
	LikelyCollector    int `json:"likely_collector"`
	ConcurrentActivity int `json:"concurrent_activity"`
	Unclosed           int `json:"unclosed,omitempty"`
}

// newAccessTimeResult converts a summary for --json output. It returns nil
// when reads were not observed.
func newAccessTimeResult(s *atime.Summary) *AccessTimeResult {
	if s == nil {
		return nil
	}
	return &AccessTimeResult{
		ObservedReads:      s.ObservedReads,
		Modified:           s.Modified,
		LikelyCollector:    s.LikelyCollector,
		ConcurrentActivity: s.ConcurrentActivity,
		Unclosed:           s.Unclosed,
	}
}

// reportAccessTimes prints a one-line access-time summary on stderr when any
// observed read changed an access time. It is silent with --quiet or --json.
func reportAccessTimes(cmd *cobra.Command, s *atime.Summary, dryRun bool) {
	if quiet || collectJSON {
		return
	}
	writeAccessTimeSummary(cmd.ErrOrStderr(), s, dryRun)
}

// writeAccessTimeSummary writes the access-time summary line for s to w.
func writeAccessTimeSummary(w io.Writer, s *atime.Summary, dryRun bool) {
	if s == nil || s.Modified == 0 {
		return
	}
	if dryRun {
		_, _ = fmt.Fprintf(w,
			"\nAccess times: %d of %d observed reads during detection changed an access time.\n"+
				"  A dry run writes no evidence container, so these changes are not recorded.\n",
			s.Modified, s.ObservedReads)
		return
	}
	_, _ = fmt.Fprintf(w,
		"\nAccess times: %d of %d observed reads changed an access time "+
			"(%d likely caused by macfor, %d during concurrent activity).\n"+
			"  See access_time_modified entries in chain-of-custody.json and access_times in manifest.json.\n",
		s.Modified, s.ObservedReads, s.LikelyCollector, s.ConcurrentActivity)
}
