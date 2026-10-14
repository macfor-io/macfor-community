package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

func TestRunLicenseCheck(t *testing.T) {
	t.Cleanup(func() { SetLicenseCheck(nil) })

	t.Run("no check registered", func(t *testing.T) {
		SetLicenseCheck(nil)
		license, err := runLicenseCheck(newRootCmd())
		if err != nil || license != nil {
			t.Errorf("runLicenseCheck() = %v, %v, want nil, nil", license, err)
		}
	})

	t.Run("check permits", func(t *testing.T) {
		want := &macfor.LicenseInfo{LicenseID: "lic-001"}
		SetLicenseCheck(func(w io.Writer) (*macfor.LicenseInfo, error) {
			_, _ = io.WriteString(w, "expires soon\n")
			return want, nil
		})
		cmd := newRootCmd()
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)
		license, err := runLicenseCheck(cmd)
		if err != nil || license != want {
			t.Errorf("runLicenseCheck() = %v, %v, want %v, nil", license, err, want)
		}
		if !strings.Contains(stderr.String(), "expires soon") {
			t.Errorf("warning not written to stderr: %q", stderr.String())
		}
	})

	t.Run("check refuses", func(t *testing.T) {
		SetLicenseCheck(func(io.Writer) (*macfor.LicenseInfo, error) {
			return nil, errors.New("license expired")
		})
		_, err := runLicenseCheck(newRootCmd())
		if !errors.Is(err, ErrLicense) {
			t.Fatalf("runLicenseCheck() error = %v, want ErrLicense", err)
		}
		if ExitCode(err) != ExitLicense {
			t.Errorf("ExitCode() = %d, want %d", ExitCode(err), ExitLicense)
		}
		if !strings.Contains(err.Error(), "license expired") {
			t.Errorf("error %q does not carry the cause", err)
		}
	})
}
