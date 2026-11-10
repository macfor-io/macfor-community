package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/macfor-io/macfor-community/pkg/macfor/atime"
)

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
  To avoid changes, collect from a read-only image or snapshot (--image, --from).

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
