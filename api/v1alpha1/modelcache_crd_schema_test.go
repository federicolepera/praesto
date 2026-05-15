package v1alpha1

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestModelCacheCRDSchemaValidation(t *testing.T) {
	schema := modelCacheOpenAPISchema(t)
	spec := schemaMap(t, schema, "properties", "spec")

	assertRequired(t, spec, "source", "storage")
	assertXValidation(t, spec, "self == oldSelf", "ModelCache spec is immutable after creation")

	source := schemaMap(t, spec, "properties", "source")
	assertRequired(t, source, "huggingface")

	huggingface := schemaMap(t, source, "properties", "huggingface")
	assertRequired(t, huggingface, "repo")
	assertMinLengthOne(t, schemaMap(t, huggingface, "properties", "repo"))

	secretRef := schemaMap(t, huggingface, "properties", "token", "properties", "secretRef")
	assertRequired(t, secretRef, "key", "name")
	assertMinLengthOne(t, schemaMap(t, secretRef, "properties", "key"))
	assertMinLengthOne(t, schemaMap(t, secretRef, "properties", "name"))

	storage := schemaMap(t, spec, "properties", "storage")
	assertRequired(t, storage, "size", "storageClassName")
	assertMinLengthOne(t, schemaMap(t, storage, "properties", "size"))
	assertMinLengthOne(t, schemaMap(t, storage, "properties", "storageClassName"))

	procMount := schemaMap(t, spec, "properties", "downloader", "properties", "containerSecurityContext", "properties", "procMount")
	assertEnum(t, procMount, "Default", "Unmasked")

	phase := schemaMap(t, schema, "properties", "status", "properties", "phase")
	assertEnum(t, phase, ModelCachePhaseReady, ModelCachePhaseDownloading, ModelCachePhaseFailed, ModelCachePhasePending)
}

func modelCacheOpenAPISchema(t *testing.T) map[string]any {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file")
	}
	crdPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "config", "crd", "bases", "praesto.praesto.io_modelcaches.yaml")
	crdBytes, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read CRD: %v", err)
	}

	var crd map[string]any
	if err := yaml.Unmarshal(crdBytes, &crd); err != nil {
		t.Fatalf("parse CRD: %v", err)
	}

	return schemaMap(t, crd, "spec", "versions", "0", "schema", "openAPIV3Schema")
}

func schemaMap(t *testing.T, root map[string]any, path ...string) map[string]any {
	t.Helper()

	var current any = root
	for _, segment := range path {
		if mapValue, ok := current.(map[string]any); ok {
			next, ok := mapValue[segment]
			if !ok {
				t.Fatalf("schema path %v: segment %q is not present", path, segment)
			}
			current = next
			continue
		}

		list, ok := current.([]any)
		if !ok {
			t.Fatalf("schema path %v: segment %q is not present", path, segment)
		}
		if segment != "0" {
			t.Fatalf("schema path %v: unsupported list segment %q", path, segment)
		}
		if len(list) == 0 {
			t.Fatalf("schema path %v: empty list at %q", path, segment)
		}
		current = list[0]
	}

	mapValue, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("schema path %v: expected object, got %T", path, current)
	}

	return mapValue
}

func assertRequired(t *testing.T, schema map[string]any, expected ...string) {
	t.Helper()

	actual := stringSet(t, schema["required"])
	for _, field := range expected {
		if !actual[field] {
			t.Fatalf("expected required field %q in %#v", field, actual)
		}
	}
}

func assertMinLengthOne(t *testing.T, schema map[string]any) {
	t.Helper()

	actual, ok := schema["minLength"].(float64)
	if !ok || int(actual) != 1 {
		t.Fatalf("expected minLength=1, got %#v", schema["minLength"])
	}
}

func assertEnum(t *testing.T, schema map[string]any, expected ...string) {
	t.Helper()

	actual := stringSet(t, schema["enum"])
	for _, value := range expected {
		if !actual[value] {
			t.Fatalf("expected enum value %q in %#v", value, actual)
		}
	}
}

func assertXValidation(t *testing.T, schema map[string]any, rule, message string) {
	t.Helper()

	validations, ok := schema["x-kubernetes-validations"].([]any)
	if !ok {
		t.Fatalf("expected x-kubernetes-validations, got %#v", schema["x-kubernetes-validations"])
	}
	for _, validation := range validations {
		validationMap, ok := validation.(map[string]any)
		if ok && validationMap["rule"] == rule && validationMap["message"] == message {
			return
		}
	}

	t.Fatalf("expected x-kubernetes-validation rule=%q message=%q in %#v", rule, message, validations)
}

func stringSet(t *testing.T, value any) map[string]bool {
	t.Helper()

	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected string list, got %#v", value)
	}

	set := map[string]bool{}
	for _, item := range items {
		stringItem, ok := item.(string)
		if !ok {
			t.Fatalf("expected string item, got %#v", item)
		}
		set[stringItem] = true
	}

	return set
}
