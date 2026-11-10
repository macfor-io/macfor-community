//go:build darwin

package atime

import (
	"golang.org/x/sys/unix"
)

// mountFlagNames names the mount flags recorded on a Volume, in output order.
var mountFlagNames = []struct {
	flag uint32
	name string
}{
	{unix.MNT_RDONLY, "read-only"},
	{unix.MNT_NOATIME, "noatime"},
	{unix.MNT_STRICTATIME, "strictatime"},
	{unix.MNT_SNAPSHOT, "snapshot"},
}

// statVolume probes the volume holding path with statfs(2).
func statVolume(path string) (Volume, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Volume{}, err
	}
	return Volume{
		MountPoint: unix.ByteSliceToString(st.Mntonname[:]),
		FSType:     unix.ByteSliceToString(st.Fstypename[:]),
		Device:     unix.ByteSliceToString(st.Mntfromname[:]),
		Flags:      flagNames(st.Flags),
		Policy:     PolicyFromFlags(st.Flags),
		fsid:       st.Fsid.Val,
	}, nil
}

// PolicyFromFlags derives the access-time policy from statfs mount flags.
func PolicyFromFlags(flags uint32) Policy {
	switch {
	case flags&unix.MNT_RDONLY != 0, flags&unix.MNT_NOATIME != 0:
		return PolicyNone
	case flags&unix.MNT_STRICTATIME != 0:
		return PolicyStrict
	default:
		return PolicyLazy
	}
}

// flagNames returns the names of the recorded flags set in flags.
func flagNames(flags uint32) []string {
	names := []string{}
	for _, f := range mountFlagNames {
		if flags&f.flag != 0 {
			names = append(names, f.name)
		}
	}
	return names
}
