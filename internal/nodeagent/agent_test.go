package nodeagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	"github.com/federicolepera/praesto/internal/modeldownload"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcilePreparesModelCacheDirectory(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode, testModelCache()).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
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
	if !containsString(updated.Finalizers, FinalizerName) {
		t.Fatalf("expected node-agent finalizer, got %#v", updated.Finalizers)
	}
	condition := findCondition(updated.Status.Conditions, praestov1alpha1.ModelCacheNodeConditionDirectoryReady)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected DirectoryReady=True, got %#v", condition)
	}
}

func TestReconcileDeletesLocalCacheDirectoryAndRemovesFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	modelCacheNode.Finalizers = []string{FinalizerName}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode, testModelCache()).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
	cacheRoot := t.TempDir()
	namespacePath := filepath.Join(cacheRoot, "default")
	localPath := filepath.Join(namespacePath, "tinyllama")
	if err := os.MkdirAll(localPath, 0o775); err != nil {
		t.Fatalf("create local path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localPath, "model.safetensors"), []byte("weights"), 0o644); err != nil {
		t.Fatalf("write model file: %v", err)
	}

	if err := k8sClient.Delete(context.Background(), modelCacheNode); err != nil {
		t.Fatalf("delete ModelCacheNode: %v", err)
	}
	reconciler := &Reconciler{Client: k8sClient, NodeName: "node-a", CacheRoot: cacheRoot, DirMode: 0o775}
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: clientObjectKey(modelCacheNode)})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("expected local cache directory to be removed, err=%v", err)
	}
	if info, err := os.Stat(namespacePath); err != nil || !info.IsDir() {
		t.Fatalf("expected namespace directory to remain, info=%v err=%v", info, err)
	}

	var deleted praestov1alpha1.ModelCacheNode
	err = k8sClient.Get(context.Background(), clientObjectKey(modelCacheNode), &deleted)
	if err == nil && containsString(deleted.Finalizers, FinalizerName) {
		t.Fatalf("expected node-agent finalizer to be removed, got %#v", deleted.Finalizers)
	}
}

func TestReconcileDownloadsAndWritesCacheMarkers(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode, testModelCache()).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
	cacheRoot := t.TempDir()
	downloader := &fakeDownloader{}
	reconciler := &Reconciler{Client: k8sClient, NodeName: "node-a", CacheRoot: cacheRoot, DirMode: 0o775, Downloader: downloader}

	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: clientObjectKey(modelCacheNode)})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	localPath := filepath.Join(cacheRoot, "default", "tinyllama")
	if !downloader.called {
		t.Fatalf("expected downloader to be called")
	}
	if downloader.targetPath != localPath {
		t.Fatalf("expected downloader target path %q, got %q", localPath, downloader.targetPath)
	}
	for _, fileName := range []string{OwnerFileName, ManifestFileName, CompleteFileName} {
		if _, err := os.Stat(filepath.Join(localPath, fileName)); err != nil {
			t.Fatalf("expected marker %s: %v", fileName, err)
		}
	}

	manifestData, err := os.ReadFile(filepath.Join(localPath, ManifestFileName))
	if err != nil {
		t.Fatalf("read manifest marker: %v", err)
	}
	var manifest ManifestFile
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("unmarshal manifest marker: %v", err)
	}
	if manifest.Repo != "org/tinyllama" || manifest.Revision != "main" || manifest.Node != "node-a" {
		t.Fatalf("unexpected manifest marker: %#v", manifest)
	}

	var updated praestov1alpha1.ModelCacheNode
	if err := k8sClient.Get(context.Background(), clientObjectKey(modelCacheNode), &updated); err != nil {
		t.Fatalf("get updated ModelCacheNode: %v", err)
	}
	if updated.Status.Phase != praestov1alpha1.ModelCacheNodePhaseReady {
		t.Fatalf("expected phase Ready, got %s", updated.Status.Phase)
	}
	ready := findCondition(updated.Status.Conditions, praestov1alpha1.ModelCacheNodeConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %#v", ready)
	}
}

func TestReconcileSkipsDownloadWhenCacheIsComplete(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode, testModelCache()).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
	cacheRoot := t.TempDir()
	localPath := filepath.Join(cacheRoot, "default", "tinyllama")
	if err := os.MkdirAll(localPath, 0o775); err != nil {
		t.Fatalf("prepare local path: %v", err)
	}
	if err := writeCompleteMarker(localPath); err != nil {
		t.Fatalf("write complete marker: %v", err)
	}
	downloader := &fakeDownloader{}
	reconciler := &Reconciler{Client: k8sClient, NodeName: "node-a", CacheRoot: cacheRoot, DirMode: 0o775, Downloader: downloader}

	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: clientObjectKey(modelCacheNode)})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if downloader.called {
		t.Fatalf("expected downloader not to be called")
	}

	var updated praestov1alpha1.ModelCacheNode
	if err := k8sClient.Get(context.Background(), clientObjectKey(modelCacheNode), &updated); err != nil {
		t.Fatalf("get updated ModelCacheNode: %v", err)
	}
	if updated.Status.Phase != praestov1alpha1.ModelCacheNodePhaseReady {
		t.Fatalf("expected phase Ready, got %s", updated.Status.Phase)
	}
}

func TestReconcileReportsMissingBasePath(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode, testModelCache()).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
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

func TestReconcileUnusedReadyCacheWaitsUntilTTLExpires(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	modelCacheNode.Spec.Eviction.UnusedTTL = "1h"
	modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhaseReady
	modelCacheNode.Status.LastUsedTime = metav1.NewTime(time.Now().Add(-10 * time.Minute))
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
	cacheRoot := t.TempDir()
	localPath := filepath.Join(cacheRoot, "default", "tinyllama")
	if err := os.MkdirAll(localPath, 0o775); err != nil {
		t.Fatalf("create local path: %v", err)
	}
	reconciler := &Reconciler{Client: client, NodeName: "node-a", CacheRoot: cacheRoot, DirMode: 0o775}

	result, evicted, err := reconciler.reconcileUnusedReadyCache(context.Background(), modelCacheNode, localPath)
	if err != nil {
		t.Fatalf("reconcile unused cache: %v", err)
	}
	if evicted {
		t.Fatalf("expected cache not to be evicted before TTL expires")
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected requeue after remaining TTL, got %v", result.RequeueAfter)
	}
	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("expected local path to remain: %v", err)
	}
}

func TestReconcileUnusedReadyCacheEvictsAfterTTL(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	modelCacheNode.Spec.Eviction.UnusedTTL = "1h"
	modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhaseReady
	modelCacheNode.Status.LastUsedTime = metav1.NewTime(time.Now().Add(-2 * time.Hour))
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelCacheNode).WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).Build()
	cacheRoot := t.TempDir()
	localPath := filepath.Join(cacheRoot, "default", "tinyllama")
	if err := os.MkdirAll(localPath, 0o775); err != nil {
		t.Fatalf("create local path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localPath, "model.safetensors"), []byte("weights"), 0o644); err != nil {
		t.Fatalf("write model file: %v", err)
	}
	reconciler := &Reconciler{Client: client, NodeName: "node-a", CacheRoot: cacheRoot, DirMode: 0o775}

	_, evicted, err := reconciler.reconcileUnusedReadyCache(context.Background(), modelCacheNode, localPath)
	if err != nil {
		t.Fatalf("reconcile unused cache: %v", err)
	}
	if !evicted {
		t.Fatalf("expected cache to be evicted after TTL expires")
	}
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("expected local path to be removed, err=%v", err)
	}

	var updated praestov1alpha1.ModelCacheNode
	if err := client.Get(context.Background(), clientObjectKey(modelCacheNode), &updated); err != nil {
		t.Fatalf("get updated ModelCacheNode: %v", err)
	}
	if updated.Status.Phase != praestov1alpha1.ModelCacheNodePhaseEvicted {
		t.Fatalf("expected phase Evicted, got %s", updated.Status.Phase)
	}
	ready := findCondition(updated.Status.Conditions, praestov1alpha1.ModelCacheNodeConditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "CacheEvicted" {
		t.Fatalf("expected Ready=False/CacheEvicted, got %#v", ready)
	}
}

func TestReconcileRehydratesEvictedCacheRequestedByLocalPod(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	modelCacheNode := testModelCacheNode()
	modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhaseEvicted
	pod := podUsingModelCacheOnNode("node-a", "default", "tinyllama")
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(modelCacheNode, testModelCache(), pod).
		WithStatusSubresource(&praestov1alpha1.ModelCacheNode{}).
		WithIndex(&corev1.Pod{}, "spec.nodeName", func(obj crclient.Object) []string {
			pod := obj.(*corev1.Pod)
			if pod.Spec.NodeName == "" {
				return nil
			}
			return []string{pod.Spec.NodeName}
		}).
		Build()
	cacheRoot := t.TempDir()
	downloader := &fakeDownloader{}
	reconciler := &Reconciler{Client: k8sClient, NodeName: "node-a", CacheRoot: cacheRoot, DirMode: 0o775, Downloader: downloader}

	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: clientObjectKey(modelCacheNode)})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	localPath := filepath.Join(cacheRoot, "default", "tinyllama")
	if !downloader.called {
		t.Fatalf("expected downloader to be called")
	}
	if downloader.targetPath != localPath {
		t.Fatalf("expected downloader target path %q, got %q", localPath, downloader.targetPath)
	}
	if _, err := os.Stat(filepath.Join(localPath, CompleteFileName)); err != nil {
		t.Fatalf("expected complete marker after rehydration: %v", err)
	}

	var updated praestov1alpha1.ModelCacheNode
	if err := k8sClient.Get(context.Background(), clientObjectKey(modelCacheNode), &updated); err != nil {
		t.Fatalf("get updated ModelCacheNode: %v", err)
	}
	if updated.Status.Phase != praestov1alpha1.ModelCacheNodePhaseReady {
		t.Fatalf("expected phase Ready, got %s", updated.Status.Phase)
	}
	if updated.Status.LastUsedTime.IsZero() {
		t.Fatalf("expected LastUsedTime to be updated")
	}
	ready := findCondition(updated.Status.Conditions, praestov1alpha1.ModelCacheNodeConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %#v", ready)
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

func testModelCache() *praestov1alpha1.ModelCache {
	return &praestov1alpha1.ModelCache{
		TypeMeta: metav1.TypeMeta{APIVersion: praestov1alpha1.GroupVersion.String(), Kind: "ModelCache"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tinyllama",
			Namespace: "default",
		},
		Spec: praestov1alpha1.ModelCacheSpec{
			Source:  praestov1alpha1.Source{Huggingface: praestov1alpha1.HuggingfaceSource{Repo: "org/tinyllama"}},
			Storage: praestov1alpha1.Storage{Size: "1Gi"},
		},
	}
}

func podUsingModelCacheOnNode(nodeName, namespace, name string) *corev1.Pod {
	readOnly := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: namespace,
			Labels: map[string]string{
				usesModelCacheLabelKey: "true",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "busybox:1.36",
			}},
			Volumes: []corev1.Volume{{
				Name: "model",
				VolumeSource: corev1.VolumeSource{
					CSI: &corev1.CSIVolumeSource{
						Driver:   praestoCSIDriverName,
						ReadOnly: &readOnly,
						VolumeAttributes: map[string]string{
							csiVolumeAttributeModelNamespaceKey: namespace,
							csiVolumeAttributeModelCacheNameKey: name,
						},
					},
				},
			}},
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

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

type fakeDownloader struct {
	called     bool
	targetPath string
}

func (f *fakeDownloader) Download(_ context.Context, req modeldownload.Request) error {
	f.called = true
	f.targetPath = req.TargetPath
	return nil
}
