# System information by source type

Every evidence container records a `source` block in `manifest.json`, built from
the source's `SystemInfo`. What can be filled in depends on how macfor reached the
evidence:

- **live**: the running Mac (`macfor collect`). Values come from system APIs and
  tools such as `sysctl`, `csrutil` and `fdesetup`.
- **directory**: a directory tree (`macfor collect --from <dir>`), such as a
  volume mounted by hand or an extracted snapshot. Values come only from files in
  the tree.
- **image**: a disk image file (`macfor collect --image <file>`) that macfor
  attaches read-only itself. It reads the same files as a directory source, over
  a merged view of the System and Data volumes of a macOS 10.15+ volume group.

A dead tree or image holds no runtime state, so some fields can't be known from
it. macfor never fills those from the machine running the collection, and never
guesses. String fields that can't be determined are `"unknown"` and the rest are
empty, zero or `false`. An `"unknown"` in a report means "not derivable from this
source", not "absent on the subject Mac".

## Field matrix

| Field | live | directory | image | Notes for directory and image |
|---|---|---|---|---|
| `os_name` | Yes | Yes | Yes | Always `macOS`. |
| `os_version` | Yes | Yes | Yes | `System/Library/CoreServices/SystemVersion.plist`. |
| `os_build` | Yes | Yes | Yes | Same plist. |
| `os_codename` | Yes | Yes | Yes | Derived from the version. |
| `kernel_version` | Yes | No | No | Live reads `kern.osrelease`. Not extracted from files. |
| `hardware_model` | Yes | Yes | Yes | `private/var/db/SystemConfiguration/com.apple.platform.plist`. |
| `hardware_model_name` | Yes | Yes | Yes | Looked up from the model. Falls back to the model identifier when the model isn't in the database. |
| `serial_number` | Yes | No | No | Live reads the IORegistry, which is runtime hardware state. |
| `architecture` | Yes | Partial | Partial | Derived from the hardware model: an Apple silicon chip in the model name gives `arm64`, any other known model `amd64`. `unknown` when the model is missing or not in the database. Never read from `System/Library/Kernels/kernel`, which is an x86_64 binary even on Apple silicon. Live reports the architecture of the macfor binary, so an Intel build running under Rosetta reports `amd64`. |
| `cpu_type` | Yes | No | No | Runtime hardware state. |
| `cpu_cores` | Yes | No | No | Runtime hardware state. |
| `memory_gb` | Yes | No | No | Runtime hardware state. |
| `boot_volume_name`, `boot_volume_uuid`, `boot_volume_fs` | Yes | No | No | Live asks the running system about its boot volume. Not extracted for directory or image sources. |
| `boot_volume_encrypted` | Yes | No | No | Same as `filevault_enabled`. |
| `sip_enabled` | Yes | No | No | Runtime NVRAM and boot-policy state, absent from a dead image. |
| `filevault_enabled` | Yes | No | No | Runtime state reported by `fdesetup`. macfor doesn't unlock encrypted volumes, so on an image of a FileVault-protected Mac the encrypted Data volume stays unreadable. |
| `secure_boot_level` | Yes | No | No | Boot policy held outside the macOS volumes. |
| `gatekeeper_enabled` | Yes | No | No | Runtime policy state reported by `spctl`. |
| `hostname` | Yes | Yes | Yes | `Library/Preferences/SystemConfiguration/preferences.plist` (`HostName`). |
| `local_hostname` | Yes | Yes | Yes | Same plist (`LocalHostName`). |
| `computer_name` | Yes | Yes | Yes | Same plist (`ComputerName`). |
| `timezone` | Yes | Partial | Partial | The target of the `private/etc/localtime` symlink, read as text and never followed, because it is absolute and would otherwise resolve against the collecting machine. Falls back to the selected-city setting in `Library/Preferences/.GlobalPreferences.plist`, which current macOS doesn't write when the time zone is set automatically. `unknown` when neither is present. |
| `timezone_offset` | Yes | No | No | The offset depends on the date and on time zone rules. Computing it would use the collecting machine's time zone database. |
| `boot_time` | Yes | No | No | Runtime state. |
| `uptime_seconds` | Yes | No | No | Runtime state. |
| `collection_time` | Yes | Yes | Yes | Collecting machine. See below. |
| `collection_user`, `collection_uid`, `is_root` | Yes | Yes | Yes | Collecting machine. See below. |
| `full_disk_access` | Yes | No | No | Whether macfor itself had Full Disk Access. Only meaningful for a live collection. |
| `source_type` | Yes | Yes | Yes | `live`, `directory` or `image`. |
| `image_path` | No | Yes | Yes | Directory: the tree's root (the first root if layered). Image: the original image file, not the mount point. |
| `image_format` | No | No | Yes | `dmg`, `e01` or `raw`, detected from the file's content. |

## Collecting-machine fields

`collection_time`, `collection_user`, `collection_uid` and `is_root` always describe
the machine and account running macfor, for every source type. For a directory or
image collection they say who collected the evidence and when, not anything about
the Mac the evidence came from.

## Accounts

Separately from the fields above, every manifest lists the user accounts the source
enumerated in `source.users`. Live sources read them from directory services. Directory
and image sources list the folders under `Users/`, enriched from the matching account
plists on the volume.
