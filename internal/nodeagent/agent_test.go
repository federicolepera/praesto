package nodeagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcilePreparesModelCacheDirectory(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
	cacheRoot := t.TempDir()
	reconciler := &Reconciler{Client: client, NodeName: "node-a", CacheRoot: cacheRoot, DirMode: 0o775}

	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: clientObjectKey(modelCacheNode)})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	localPath := filepath.Join(cacheRoot, "default", "tinyllama")
	if info, err := os.Stat(localPath); err != nil || !info.IsDir() {
		t.Fatalf("expected directory %q, info=%v err=%v", localPath, info, err)
	}

	marker, err := os.ReadFile(filepath.Join(localPath, OwnerFileName))
	if err != nil {
		t.Fatalf("read owner marker: %v", err)
	}
	var owner OwnerFile
	if err := json.Unmarshal(marker, &owner); err != nil {
		t.Fatalf("unmarshal owner marker: %v", err)
	}
	if owner.Namespace != "default" || owner.Name != "tinyllama" || owner.Node != "node-a" {
		t.Fatalf("unexpected owner marker: %#v", owner)
	}

	var updated praestov1alpha1.ModelCacheNode
	if err := client.Get(context.Background(), clientObjectKey(modelCacheNode), &updated); err != nil {
		t.Fatalf("get updated ModelCacheNode: %v", err)
	}
	condition := findCondition(updated.Status.Conditions, praestov1alpha1.ModelCacheNodeConditionDirectoryReady)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected DirectoryReady=True, got %#v", condition)
	}
}

func TestReconcileReportsMissingBasePath(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
	reconciler := &Reconciler{Client: client, NodeName: "node-a", CacheRoot: filepath.Join(t.TempDir(), "missing"), DirMode: 0o775}

	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: clientObjectKey(modelCacheNode)})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var updated praestov1alpha1.ModelCacheNode
	if err := client.Get(context.Background(), clientObjectKey(modelCacheNode), &updated); err != nil {
		t.Fatalf("get updated ModelCacheNode: %v", err)
	}
	condition := findCondition(updated.Status.Conditions, praestov1alpha1.ModelCacheNodeConditionDirectoryReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "BasePathMissing" {
		t.Fatalf("expected DirectoryReady=False/BasePathMissing, got %#v", condition)
	}
}

func testModelCacheNode() *praestov1alpha1.ModelCacheNode {
	return &praestov1alpha1.ModelCacheNode{
		TypeMeta: metav1.TypeMeta{APIVersion: praestov1alpha1.GroupVersion.String(), Kind: "ModelCacheNode"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-tinyllama-node-a",
		},
		Spec: praestov1alpha1.ModelCacheNodeSpec{
			ModelCacheRef: praestov1alpha1.ModelCacheNodeModelCacheRef{Namespace: "default", Name: "tinyllama", UID: "uid-1"},
			NodeName:      "node-a",
			Storage:       praestov1alpha1.StorageNode{Size: "1Gi"},
		},
	}
}

func clientObjectKey(modelCacheNode *praestov1alpha1.ModelCacheNode) types.NamespacedName {
	return types.NamespacedName{Name: modelCacheNode.Name}
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
