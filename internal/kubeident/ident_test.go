package kubeident

import (
	"strings"
	"testing"
)

func TestDNS1123LabelTruncatesWithStableHash(t *testing.T) {
	longNodeName := strings.Repeat("very-long-node-name-", 8)
	name := DNS1123Label("mcn", "namespace", "model-cache", longNodeName)

	if len(name) > MaxDNS1123LabelLength {
		t.Fatalf("expected name length <= %d, got %d: %s", MaxDNS1123LabelLength, len(name), name)
	}
	if name != DNS1123Label("mcn", "namespace", "model-cache", longNodeName) {
		t.Fatalf("expected deterministic name")
	}
	if name == DNS1123Label("mcn", "namespace", "model-cache", longNodeName+"different") {
		t.Fatalf("expected different long names to keep distinct hashes")
	}
}

func TestLabelValueTruncatesToKubernetesLimit(t *testing.T) {
	value := LabelValue(strings.Repeat("node.", 20))

	if len(value) > MaxLabelValueLength {
		t.Fatalf("expected label value length <= %d, got %d: %s", MaxLabelValueLength, len(value), value)
	}
	if strings.HasSuffix(value, ".") || strings.HasSuffix(value, "-") || strings.HasSuffix(value, "_") {
		t.Fatalf("expected label value to end with an alphanumeric character: %s", value)
	}
}
