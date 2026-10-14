package macfor

import (
	"regexp"
	"testing"

	"github.com/macfor-io/macfor-community/internal/sysinfo"
)

func TestHardwareArchitecture(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{"M1 MacBook Air", "MacBookAir10,1", ArchARM64},
		{"M2 MacBook Air", "Mac14,2", ArchARM64},
		{"M1 Max Mac Studio", "Mac13,1", ArchARM64},
		{"M2 Ultra Mac Pro", "Mac14,8", ArchARM64},
		{"M4 Max only in internal DB", "Mac16,6", ArchARM64},
		{"Intel MacBook Pro 2019", "MacBookPro16,1", ArchAMD64},
		{"Intel Mac Pro 2019", "MacPro7,1", ArchAMD64},
		{"Intel MacBook Pro Mid 2012 internal DB", "MacBookPro9,1", ArchAMD64},
		{"Intel iMac Pro", "iMacPro1,1", ArchAMD64},
		{"unknown model", "MacFuture99,1", ""},
		{"empty model", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HardwareArchitecture(tt.model); got != tt.want {
				t.Errorf("HardwareArchitecture(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestLookupHardwareModelName(t *testing.T) {
	if name, ok := LookupHardwareModelName("Mac14,2"); !ok || name != "MacBook Air (M2, 2022)" {
		t.Errorf("LookupHardwareModelName(Mac14,2) = %q, %v", name, ok)
	}
	if _, ok := LookupHardwareModelName("Mac16,1"); !ok {
		t.Error("LookupHardwareModelName(Mac16,1) not found; want internal DB fallback")
	}
	if name, ok := LookupHardwareModelName("MacFuture99,1"); ok || name != "" {
		t.Errorf("LookupHardwareModelName(unknown) = %q, %v; want \"\", false", name, ok)
	}
}

// appleSiliconIdentifier matches the hardware model identifiers Apple has used
// for Apple silicon Macs: MacN,M with N >= 13, plus the first-generation M1
// identifiers that kept the legacy family names.
var appleSiliconIdentifier = regexp.MustCompile(
	`^(Mac(1[3-9]|[2-9][0-9]),[0-9]+|MacBookAir10,1|MacBookPro1[78],[0-9]+|Macmini9,1|iMac21,[0-9]+)$`)

// TestHardwareArchitecture_DatabaseConsistency checks the chip-token rule
// against every entry of both model databases: a name carries an Apple
// silicon chip token exactly when its identifier is an Apple silicon one.
func TestHardwareArchitecture_DatabaseConsistency(t *testing.T) {
	for _, db := range []map[string]string{HardwareModels, sysinfo.HardwareModels} {
		for model, name := range db {
			wantARM := appleSiliconIdentifier.MatchString(model)
			gotARM := appleSiliconChip.MatchString(name)
			if wantARM != gotARM {
				t.Errorf("model %q name %q: chip token = %v, Apple silicon identifier = %v",
					model, name, gotARM, wantARM)
			}
		}
	}
}
