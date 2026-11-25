package updater

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version represents a parsed semantic version (major.minor.patch[-prerelease]).
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Raw        string
}

var assetVersionPattern = regexp.MustCompile(`(?i)^khutaf-v([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:-[0-9A-Za-z.-]+)?)-win64\.zip$`)

// ExtractAssetVersion extracts the version string from an asset filename like
// "Khutaf-v0.1.5-beta-win64.zip". Returns "" if the filename does not match.
func ExtractAssetVersion(assetName string) string {
	m := assetVersionPattern.FindStringSubmatch(strings.TrimSpace(assetName))
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

// ParseVersion parses a semantic version string (with optional leading 'v' and prerelease suffix).
func ParseVersion(s string) (Version, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return Version{}, fmt.Errorf("empty version string")
	}
	trimmed = strings.TrimPrefix(trimmed, "v")
	trimmed = strings.TrimPrefix(trimmed, "V")

	if idx := strings.IndexByte(trimmed, '+'); idx >= 0 {
		trimmed = trimmed[:idx]
	}

	core := trimmed
	pre := ""
	if idx := strings.IndexByte(trimmed, '-'); idx >= 0 {
		core = trimmed[:idx]
		pre = trimmed[idx+1:]
		if pre == "" {
			return Version{}, fmt.Errorf("invalid prerelease in version %q", s)
		}
	}

	parts := strings.Split(core, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, fmt.Errorf("invalid version %q", s)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return Version{}, fmt.Errorf("invalid major version in %q", s)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return Version{}, fmt.Errorf("invalid minor version in %q", s)
	}
	patch := 0
	if len(parts) == 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil || patch < 0 {
			return Version{}, fmt.Errorf("invalid patch version in %q", s)
		}
	}

	raw := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	if pre != "" {
		raw += "-" + pre
	}
	return Version{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: pre,
		Raw:        raw,
	}, nil
}

// Compare returns -1 if v < other, 0 if v == other, and 1 if v > other.
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		return cmpInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return cmpInt(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return cmpInt(v.Patch, other.Patch)
	}
	if v.Prerelease == other.Prerelease {
		return 0
	}
	if v.Prerelease == "" {
		return 1
	}
	if other.Prerelease == "" {
		return -1
	}

	aParts := strings.Split(v.Prerelease, ".")
	bParts := strings.Split(other.Prerelease, ".")
	limit := len(aParts)
	if len(bParts) < limit {
		limit = len(bParts)
	}
	for i := 0; i < limit; i++ {
		if aParts[i] == bParts[i] {
			continue
		}
		aNum, aErr := strconv.Atoi(aParts[i])
		bNum, bErr := strconv.Atoi(bParts[i])
		switch {
		case aErr == nil && bErr == nil:
			return cmpInt(aNum, bNum)
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if aParts[i] < bParts[i] {
				return -1
			}
			return 1
		}
	}
	return cmpInt(len(aParts), len(bParts))
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
