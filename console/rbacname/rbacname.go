package rbacname

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

const maxDNS1123LabelLength = 63

// RoleBindingName returns the deterministic RoleBinding name specified by ADR
// 036. rolePurpose is truncated as needed, preserving the subject hash suffix.
func RoleBindingName(rolePurpose, kind, name string) string {
	kindPrefix := "x"
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "user":
		kindPrefix = "u"
	case "group":
		kindPrefix = "g"
	}

	sum := sha256.Sum256([]byte(name))
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
	suffix := kindPrefix + "-" + encoded[:10]

	prefix := dns1123(strings.ToLower(strings.TrimSpace(rolePurpose)))
	if prefix == "" {
		prefix = "role"
	}
	maxPrefix := maxDNS1123LabelLength - len(suffix) - 1
	if len(prefix) > maxPrefix {
		prefix = strings.Trim(prefix[:maxPrefix], "-")
	}
	if prefix == "" {
		prefix = "role"
	}
	return prefix + "-" + suffix
}

func dns1123(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
