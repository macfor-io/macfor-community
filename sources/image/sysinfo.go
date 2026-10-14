package image

import (
	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// SystemInfo returns system information read from the files on the image.
//
// Extraction is delegated to the directory source over the merged volumes;
// the provenance fields are then overridden so they describe the image rather
// than its mount point: SourceType is SourceTypeImage, ImagePath is the
// original image file, and ImageFormat is the detected format. It never
// returns an error: fields that cannot be derived from the image are left
// "unknown" or zero.
func (s *Source) SystemInfo() (*macfor.SystemInfo, error) {
	info, err := s.dir.SystemInfo()
	if err != nil || info == nil {
		// The directory source never fails today; stay populated if it ever does.
		info = &macfor.SystemInfo{}
	}

	info.SourceType = macfor.SourceTypeImage
	info.ImagePath = s.path
	info.ImageFormat = string(s.format)
	return info, nil
}
