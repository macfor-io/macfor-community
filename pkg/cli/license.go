package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// LicenseCheck decides whether a collection may start. It returns the license
// to record in the evidence container, or an error to refuse the collection.
// Warnings, such as an approaching expiry, are written to w.
type LicenseCheck func(w io.Writer) (*macfor.LicenseInfo, error)

// licenseCheck is the registered license check. The open-source build has
// none, so every collection is permitted.
var licenseCheck LicenseCheck

// SetLicenseCheck registers the check that runs before every collection. A
// licensed edition calls it from main before Execute. Pass nil to remove it.
func SetLicenseCheck(check LicenseCheck) {
	licenseCheck = check
}

// runLicenseCheck runs the registered license check. A refusal is wrapped in
// ErrLicense so the process exits with ExitLicense.
func runLicenseCheck(cmd *cobra.Command) (*macfor.LicenseInfo, error) {
	if licenseCheck == nil {
		return nil, nil
	}
	license, err := licenseCheck(cmd.ErrOrStderr())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLicense, err)
	}
	return license, nil
}
