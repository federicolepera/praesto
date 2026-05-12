package status

import (
	"context"
	"testing"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSetConditionAddsNewCondition(t *testing.T) {
	modelCache := testModelCache()

	SetCondition(modelCache, ConditionPVCReady, metav1.ConditionTrue, "PVCBound", "PVC is bound")

	if len(modelCache.Status.Conditions) != 1 {
		t.Fatalf("expected one condition, got %d", len(modelCache.Status.Conditions))
	}
	condition := modelCache.Status.Conditions[0]
	if condition.Type != ConditionPVCReady {
		t.Fatalf("unexpected condition type: %s", condition.Type)
	}
	if condition.Status != metav1.ConditionTrue {
		t.Fatalf("unexpected condition status: %s", condition.Status)
	}
	if condition.ObservedGeneration != modelCache.Generation {
		t.Fatalf("expected observed generation %d, got %d", modelCache.Generation, condition.ObservedGeneration)
	}
	if condition.Reason != "PVCBound" {
		t.Fatalf("unexpected condition reason: %s", condition.Reason)
	}
	if condition.Message != "PVC is bound" {
		t.Fatalf("unexpected condition message: %s", condition.Message)
	}
	if condition.LastTransitionTime.IsZero() {
		t.Fatalf("expected last transition time to be set")
	}
}

func TestSetConditionUpdatesExistingCondition(t *testing.T) {
	modelCache := testModelCache()
	SetCondition(modelCache, ConditionReady, metav1.ConditionFalse, "Downloading", "download is running")
	originalTransitionTime := modelCache.Status.Conditions[0].LastTransitionTime

	SetCondition(modelCache, ConditionReady, metav1.ConditionTrue, "Ready", "model cache is ready")

	if len(modelCache.Status.Conditions) != 1 {
		t.Fatalf("expected one condition after update, got %d", len(modelCache.Status.Conditions))
	}
	condition := modelCache.Status.Conditions[0]
	if condition.Type != ConditionReady {
		t.Fatalf("unexpected condition type: %s", condition.Type)
	}
	if condition.Status != metav1.ConditionTrue {
		t.Fatalf("unexpected condition status: %s", condition.Status)
	}
	if condition.Reason != "Ready" {
		t.Fatalf("unexpected condition reason: %s", condition.Reason)
	}
	if condition.Message != "model cache is ready" {
		t.Fatalf("unexpected condition message: %s", condition.Message)
	}
	if !condition.LastTransitionTime.After(originalTransitionTime.Time) && condition.LastTransitionTime != originalTransitionTime {
		t.Fatalf("expected last transition time to be preserved or advanced")
	}
}

func TestSetConditionKeepsMultipleConditionTypes(t *testing.T) {
	modelCache := testModelCache()

	SetCondition(modelCache, ConditionPVCReady, metav1.ConditionTrue, "PVCBound", "PVC is bound")
	SetCondition(modelCache, ConditionDownloadComplete, metav1.ConditionTrue, "JobComplete", "download completed")
	SetCondition(modelCache, ConditionReady, metav1.ConditionTrue, "Ready", "model cache is ready")

	if len(modelCache.Status.Conditions) != 3 {
		t.Fatalf("expected three conditions, got %d", len(modelCache.Status.Conditions))
	}
	assertTrueCondition(t, modelCache, ConditionPVCReady, "PVCBound", "PVC is bound")
	assertTrueCondition(t, modelCache, ConditionDownloadComplete, "JobComplete", "download completed")
	assertTrueCondition(t, modelCache, ConditionReady, "Ready", "model cache is ready")
}

func TestUpdatePersistsStatus(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	modelCache := testModelCache()
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&praestov1alpha1.ModelCache{}).
		WithObjects(modelCache).
		Build()

	modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseReady
	modelCache.Status.PvcName = "praesto-tinyllama-test"
	modelCache.Status.DownloadJobName = "praesto-download-tinyllama-test"
	SetCondition(modelCache, ConditionReady, metav1.ConditionTrue, "Ready", "model cache is ready")

	if err := Update(ctx, k8sClient, modelCache); err != nil {
		t.Fatalf("update status: %v", err)
	}

	storedModelCache := &praestov1alpha1.ModelCache{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: modelCache.Name, Namespace: modelCache.Namespace}, storedModelCache); err != nil {
		t.Fatalf("get stored ModelCache: %v", err)
	}
	if storedModelCache.Status.Phase != praestov1alpha1.ModelCachePhaseReady {
		t.Fatalf("unexpected stored phase: %s", storedModelCache.Status.Phase)
	}
	if storedModelCache.Status.PvcName != "praesto-tinyllama-test" {
		t.Fatalf("unexpected stored PVC name: %s", storedModelCache.Status.PvcName)
	}
	if storedModelCache.Status.DownloadJobName != "praesto-download-tinyllama-test" {
		t.Fatalf("unexpected stored download Job name: %s", storedModelCache.Status.DownloadJobName)
	}
	assertTrueCondition(t, storedModelCache, ConditionReady, "Ready", "model cache is ready")
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add praesto scheme: %v", err)
	}
	return scheme
}

func testModelCache() *praestov1alpha1.ModelCache {
	return &praestov1alpha1.ModelCache{
		TypeMeta: metav1.TypeMeta{
			APIVersion: praestov1alpha1.GroupVersion.String(),
			Kind:       "ModelCache",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "tinyllama-test",
			Namespace:  "default",
			Generation: 7,
		},
	}
}

func assertTrueCondition(t *testing.T, modelCache *praestov1alpha1.ModelCache, conditionType, expectedReason, expectedMessage string) {
	t.Helper()
	for _, condition := range modelCache.Status.Conditions {
		if condition.Type != conditionType {
			continue
		}
		if condition.Status != metav1.ConditionTrue {
			t.Fatalf("expected condition %s status %s, got %s", conditionType, metav1.ConditionTrue, condition.Status)
		}
		if condition.Reason != expectedReason {
			t.Fatalf("expected condition %s reason %s, got %s", conditionType, expectedReason, condition.Reason)
		}
		if condition.Message != expectedMessage {
			t.Fatalf("expected condition %s message %s, got %s", conditionType, expectedMessage, condition.Message)
		}
		return
	}
	t.Fatalf("expected condition %s, got %#v", conditionType, modelCache.Status.Conditions)
}
