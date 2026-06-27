package kubeident

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

const (
	MaxDNS1123LabelLength = 63
	MaxLabelValueLength   = 63
	hashLength            = 10
)

// DNS1123Label returns a deterministic Kubernetes-safe DNS-1123 label.
// If the sanitized value is too long, it keeps a readable prefix and appends
// a short hash of the full sanitized value to avoid collisions.
func DNS1123Label(prefix string, parts ...string) string {
	return DNS1123LabelFromRaw(strings.Join(append([]string{prefix}, parts...), "-"))
}

// DNS1123LabelFromRaw normalizes and truncates an already composed name.
func DNS1123LabelFromRaw(value string) string {
	return truncateWithHash(sanitizeDNS1123Label(value), MaxDNS1123LabelLength)
}

// LabelValue returns a deterministic Kubernetes-safe label value.
// Kubernetes label values have a stricter 63-character limit than many object
// names, so never put raw object or node names in labels.
func LabelValue(value string) string {
	return truncateWithHash(sanitizeLabelValue(value), MaxLabelValueLength)
}

func truncateWithHash(value string, maxLength int) string {
	if value == "" {
		value = "x"
	}
	if len(value) <= maxLength {
		return value
	}

	hash := shortHash(value)
	keep := maxLength - len(hash) - 1
	if keep < 1 {
		return hash[:maxLength]
	}

	trimmed := strings.TrimRight(value[:keep], "-_.")
	if trimmed == "" {
		trimmed = "x"
	}
	return trimmed + "-" + hash
}

func sanitizeDNS1123Label(value string) string {
	return sanitize(value, func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
	}, '-')
}

func sanitizeLabelValue(value string) string {
	return sanitize(value, func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
	}, '-')
}

func sanitize(value string, allowed func(rune) bool, replacement rune) string {
	var builder strings.Builder
	lastReplacement := false
	for _, r := range strings.ToLower(value) {
		if allowed(r) {
			builder.WriteRune(r)
			lastReplacement = false
			continue
		}
		if !lastReplacement {
			builder.WriteRune(replacement)
			lastReplacement = true
		}
	}

	return strings.Trim(builder.String(), "-_.")
}

func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:hashLength]
}
