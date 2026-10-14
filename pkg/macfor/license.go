package macfor

import "time"

// LicenseInfo describes the license a build of the collector runs under. The
// open-source build has none; a licensed edition supplies it through the CLI's
// license check, and the collection records it in the evidence container's
// manifest and chain of custody.
type LicenseInfo struct {
	// LicenseID uniquely identifies the license.
	LicenseID string `json:"license_id"`

	// Customer is the licensee.
	Customer string `json:"customer,omitempty"`

	// Tier is the license tier or edition.
	Tier string `json:"tier,omitempty"`

	// IssuedAt is when the license was issued.
	IssuedAt time.Time `json:"issued_at,omitzero"`

	// ExpiresAt is when the license stops permitting collection.
	ExpiresAt time.Time `json:"expires_at,omitzero"`

	// KeyID identifies the key that signed the license.
	KeyID string `json:"key_id,omitempty"`
}
