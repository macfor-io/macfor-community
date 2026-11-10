// Package atime makes the collector's effect on file access times visible.
//
// On a live Mac, reading a file or listing a directory can update its access
// time (atime), depending on the mount options of the volume it lives on. The
// collector never resets an access time: doing so is itself a write, needs
// write access, and updates the inode change time (ctime), a more valuable
// timestamp. Instead this package:
//
//   - probes each volume behind a source for its access-time policy
//     (VolumeFor, ProbeSource), so the collector can warn before it reads; and
//   - observes reads made through a macfor.Source (Tracker.Source), taking a
//     stat before and after each read and recording every access-time change
//     it sees, with both stats, as chain-of-custody evidence.
package atime

import (
	"os"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// Policy describes when reads on a volume update access times.
type Policy string

const (
	// PolicyNone means reads never update access times: the volume is
	// mounted read-only or with noatime.
	PolicyNone Policy = "none"

	// PolicyLazy means reads may update access times. It is the default for
	// APFS and HFS+ volumes mounted without noatime or strictatime, where
	// macOS skips some, but not all, access-time updates.
	PolicyLazy Policy = "lazy"

	// PolicyStrict means every read updates the access time: the volume is
	// mounted with strictatime.
	PolicyStrict Policy = "strict"

	// PolicyUnknown means the policy could not be determined, for example
	// on a platform without the macOS mount flags.
	PolicyUnknown Policy = "unknown"
)

// MayUpdate reports whether reads under this policy can change access times.
// An unknown policy is treated as one that may.
func (p Policy) MayUpdate() bool {
	return p != PolicyNone
}

// Volume describes a mounted volume and its access-time policy.
type Volume struct {
	// MountPoint is where the volume is mounted, e.g. /System/Volumes/Data.
	MountPoint string `json:"mount_point"`

	// FSType is the filesystem type, e.g. apfs.
	FSType string `json:"fs_type"`

	// Device is the mounted device, e.g. /dev/disk3s5.
	Device string `json:"device"`

	// Flags lists the access-time-relevant mount flags that are set, e.g.
	// read-only, noatime, strictatime.
	Flags []string `json:"flags"`

	// Policy is the access-time policy the flags imply.
	Policy Policy `json:"atime_policy"`

	// fsid identifies the volume for de-duplication.
	fsid [2]int32
}

// VolumeFor returns the volume that holds path and its access-time policy,
// read with one statfs(2) call. It is not cached: a collection probes a
// handful of paths once, and a cache would outlive remounts.
func VolumeFor(path string) (Volume, error) {
	return statVolume(path)
}

// Rooted is implemented by sources that read from a known set of host
// directories, such as a directory source or an attached disk image.
type Rooted interface {
	// Roots returns the host directories the source reads from.
	Roots() []string
}

// liveRoots are the host paths that hold artifacts on a live macOS system:
// the System volume, the Data volume (where almost every artifact lives) and
// the default home-directory location.
var liveRoots = []string{"/", "/System/Volumes/Data", "/Users", "/private/var"}

// ProbeSource returns the volumes behind src, de-duplicated, in a stable
// order. For a live source these are the System and Data volumes plus the
// volume of each enumerated user's home directory; for a Rooted source, the
// volumes of its roots. Paths that cannot be probed are skipped. It returns
// nil when nothing could be probed.
func ProbeSource(src macfor.Source) []Volume {
	var roots []string
	switch {
	case src.Type() == macfor.SourceTypeLive:
		roots = append(roots, liveRoots...)
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, home)
		}
		if users, err := src.Users(); err == nil {
			for _, u := range users {
				if u.HomeDir != "" {
					roots = append(roots, u.HomeDir)
				}
			}
		}
	default:
		if r, ok := src.(Rooted); ok {
			roots = r.Roots()
		}
	}
	return probePaths(roots)
}

// probePaths probes each path and returns the distinct volumes in first-seen
// order.
func probePaths(paths []string) []Volume {
	var vols []Volume
	seen := make(map[[2]int32]bool)
	for _, p := range paths {
		v, err := VolumeFor(p)
		if err != nil || seen[v.fsid] {
			continue
		}
		seen[v.fsid] = true
		vols = append(vols, v)
	}
	return vols
}

// ShouldObserve reports whether reads from a source with these volumes
// should be observed: when any volume may update access times, or when no
// volume could be probed at all, since an unknown policy is treated as one
// that may.
func ShouldObserve(vols []Volume) bool {
	return len(vols) == 0 || AnyMayUpdate(vols)
}

// AnyMayUpdate reports whether reads on any of vols may change access times.
func AnyMayUpdate(vols []Volume) bool {
	for _, v := range vols {
		if v.Policy.MayUpdate() {
			return true
		}
	}
	return false
}
