package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
)

// nameRegex permits a libvirt-friendly subset: lowercase letters, digits, and
// hyphens, starting with a letter or digit, max 30 chars. We're stricter than
// libvirt itself because users will type these and we want them shell-safe.
var nameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)

// ValidateName returns an error if name violates the sandbox naming rules.
func ValidateName(name string) error {
	if !nameRegex.MatchString(name) {
		return fmt.Errorf("invalid name %q: must match %s", name, nameRegex)
	}
	return nil
}

// GenerateName returns a random short name like "sandbox-3f9a17".
func GenerateName() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return "sandbox-" + hex.EncodeToString(b[:])
}
