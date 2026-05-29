package version

import (
	"strconv"
	"strings"
)

// BuildVersion can be injected at build time via -ldflags.
// In release builds it should come from Git tag (e.g. v2.3.5).
var BuildVersion = ""

func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}

func Tag(v string) string {
	v = normalize(v)
	if v == "" || v == "dev" {
		return v
	}
	if v[0] < '0' || v[0] > '9' {
		return v
	}
	return "v" + v
}

func Compare(a, b string) int {
	pa := parse(normalize(a))
	pb := parse(normalize(b))
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parse(v string) [3]int {
	var out [3]int
	parts := strings.SplitN(v, ".", 4)
	for i := 0; i < 3 && i < len(parts); i++ {
		n := readLeadingInt(parts[i])
		out[i] = n
	}
	return out
}

func readLeadingInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	i := 0
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
	}
	if i == 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0
	}
	return n
}
