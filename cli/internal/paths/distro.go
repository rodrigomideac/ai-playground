package paths

import (
	"fmt"
	"os"
	"strings"
)

// Distro is one of the supported package-manager families.
type Distro int

const (
	DistroUnknown Distro = iota
	DistroArch
	DistroDebian
	DistroFedora
)

func (d Distro) String() string {
	switch d {
	case DistroArch:
		return "arch"
	case DistroDebian:
		return "debian"
	case DistroFedora:
		return "fedora"
	}
	return "unknown"
}

// DetectDistro reads /etc/os-release and maps ID + ID_LIKE to one of our
// supported families. The raw ID is also returned for use in error messages.
func DetectDistro() (Distro, string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return DistroUnknown, "", fmt.Errorf("read /etc/os-release: %w", err)
	}
	rawID := ""
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			rawID = v
			ids = append(ids, v)
		case "ID_LIKE":
			ids = append(ids, strings.Fields(v)...)
		}
	}
	for _, id := range ids {
		switch id {
		case "arch", "manjaro", "endeavouros", "artix":
			return DistroArch, rawID, nil
		case "debian", "ubuntu", "linuxmint", "raspbian":
			return DistroDebian, rawID, nil
		case "fedora", "rhel", "centos", "rocky", "almalinux":
			return DistroFedora, rawID, nil
		}
	}
	return DistroUnknown, rawID, nil
}
