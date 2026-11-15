package core

import (
	"strconv"
	"strings"
)

// Version es una versión de HAProxy reducida a mayor.menor.
type Version struct{ Major, Minor int }

// ParseVersion interpreta «2.8», «2.8.3», «v3.0» o «3.0-dev». Compara
// numéricamente (2.10 > 2.9), no como texto.
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "v"), "V")
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return Version{}, false
	}
	maj, err1 := strconv.Atoi(parts[0])
	minor := parts[1]
	if i := strings.IndexAny(minor, "-+"); i >= 0 {
		minor = minor[:i]
	}
	min, err2 := strconv.Atoi(minor)
	if err1 != nil || err2 != nil || maj < 0 || min < 0 {
		return Version{}, false
	}
	return Version{maj, min}, true
}

// Less indica si v es anterior a o.
func (v Version) Less(o Version) bool {
	return v.Major < o.Major || (v.Major == o.Major && v.Minor < o.Minor)
}

func (v Version) String() string { return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) }
