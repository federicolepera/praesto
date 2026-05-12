package downloader

import (
	"context"
	"strings"
	"testing"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPersistentVolumeClaimForModelCache(t *testing.T) {
	scheme := newTestScheme(t)
	modelCache := testModelCache()

	pvc, err := PersistentVolumeClaimForModelCache(scheme, modelCache)
	if err != nil {
		t.Fatalf("build PVC: %v", err)
	}

	if pvc.Name != PVCNameForModelCache(modelCache.Name) {
		t.Fatalf("unexpected PVC name: %s", pvc.Name)
	}
	if pvc.Namespace != modelCache.Namespace {
		t.Fatalf("unexpected PVC namespace: %s", pvc.Namespace)
	}
	assertLabels(t, pvc.Labels, ModelCacheLabels(modelCache.Name))
	if !containsAccessMode(pvc.Spec.AccessModes, corev1.ReadWriteMany) {
		t.Fatalf("expected PVC access mode %s, got %#v", corev1.ReadWriteMany, pvc.Spec.AccessModes)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != modelCache.Spec.Storage.StorageClassName {
		t.Fatalf("unexpected PVC storage class: %#v", pvc.Spec.StorageClassName)
	}
	if got := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; got != resource.MustParse("10Gi") {
		t.Fatalf("unexpected PVC storage request: %s", got.String())
	}
	assertOwnerReference(t, pvc.OwnerReferences, "ModelCache", modelCache.Name)
}

func TestPersistentVolumeClaimForModelCacheWithoutStorageClass(t *testing.T) {
	scheme := newTestScheme(t)
	modelCache := testModelCache()
	modelCache.Spec.Storage.StorageClassName = ""

	pvc, err := PersistentVolumeClaimForModelCache(scheme, modelCache)
	if err != nil {
		t.Fatalf("build PVC: %v", err)
	}
	if pvc.Spec.StorageClassName != nil {
		t.Fatalf("expected nil storage class, got %#v", *pvc.Spec.StorageClassName)
	}
}

func TestPersistentVolumeClaimForModelCacheRejectsInvalidSize(t *testing.T) {
	scheme := newTestScheme(t)
	modelCache := testModelCache()
	modelCache.Spec.Storage.Size = "not-a-size"

	_, err := PersistentVolumeClaimForModelCache(scheme, modelCache)
	if err == nil || !strings.Contains(err.Error(), "invalid storage size") {
		t.Fatalf("expected invalid storage size error, got %v", err)
	}
}

func TestEnsureModelCachePVC(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	modelCache := testModelCache()

	t.Run("creates managed PVC", func(t *testing.T) {
		k8sClient := newTestClient(scheme, modelCache)

		pvc, err := EnsureModelCachePVC(ctx, k8sClient, scheme, modelCache)
		if err != nil {
			t.Fatalf("ensure PVC: %v", err)
		}

		if pvc.Name != PVCNameForModelCache(modelCache.Name) {
			t.Fatalf("unexpected PVC name: %s", pvc.Name)
		}
		storedPVC := &corev1.PersistentVolumeClaim{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, storedPVC); err != nil {
			t.Fatalf("get stored PVC: %v", err)
		}
		assertLabels(t, storedPVC.Labels, ModelCacheLabels(modelCache.Name))
	})

	t.Run("returns existing managed PVC without duplicating it", func(t *testing.T) {
		existingPVC := testPVC(modelCache)
		k8sClient := newTestClient(scheme, modelCache, existingPVC)

		pvc, err := EnsureModelCachePVC(ctx, k8sClient, scheme, modelCache)
		if err != nil {
			t.Fatalf("ensure PVC: %v", err)
		}

		if pvc.Name != existingPVC.Name {
			t.Fatalf("expected existing PVC %s, got %s", existingPVC.Name, pvc.Name)
		}
		pvcList := &corev1.PersistentVolumeClaimList{}
		if err := k8sClient.List(ctx, pvcList, client.InNamespace(modelCache.Namespace)); err != nil {
			t.Fatalf("list PVCs: %v", err)
		}
		if len(pvcList.Items) != 1 {
			t.Fatalf("expected one PVC, got %d", len(pvcList.Items))
		}
	})

	t.Run("rejects existing unmanaged PVC", func(t *testing.T) {
		unmanagedPVC := testPVC(modelCache)
		unmanagedPVC.Labels = nil
		k8sClient := newTestClient(scheme, modelCache, unmanagedPVC)

		_, err := EnsureModelCachePVC(ctx, k8sClient, scheme, modelCache)
		if err == nil || !strings.Contains(err.Error(), "not managed by praesto") {
			t.Fatalf("expected unmanaged PVC error, got %v", err)
		}
	})
}

func TestDownloadJobForModelCache(t *testing.T) {
	modelCache := testModelCache()
	pvc := testPVC(modelCache)

	job := DownloadJobForModelCache(modelCache, pvc)

	if job.Name != JobNameForModelCache(modelCache.Name) {
		t.Fatalf("unexpected Job name: %s", job.Name)
	}
	if job.Namespace != modelCache.Namespace {
		t.Fatalf("unexpected Job namespace: %s", job.Namespace)
	}
	assertLabels(t, job.Labels, DownloadJobLabels(modelCache.Name))
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 7200 {
		t.Fatalf("unexpected active deadline: %#v", job.Spec.ActiveDeadlineSeconds)
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 3 {
		t.Fatalf("unexpected backoff limit: %#v", job.Spec.BackoffLimit)
	}
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyOnFailure {
		t.Fatalf("unexpected restart policy: %s", job.Spec.Template.Spec.RestartPolicy)
	}
	assertLabels(t, job.Spec.Template.Labels, ModelCacheLabels(modelCache.Name))

	if len(job.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected one container, got %d", len(job.Spec.Template.Spec.Containers))
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.Name != "downloader" {
		t.Fatalf("unexpected container name: %s", container.Name)
	}
	if container.Image != DefaultDownloaderImage {
		t.Fatalf("unexpected downloader image: %s", container.Image)
	}
	assertEnvValue(t, container.Env, "HF_REPO", modelCache.Spec.Source.Huggingface.Repo)
	assertEnvValue(t, container.Env, "SOURCE_TYPE", "huggingface")
	assertEnvValue(t, container.Env, "TARGET_PATH", "/model")
	assertEnvValue(t, container.Env, "MODELCACHE_NAME", modelCache.Name)
	assertEnvValue(t, container.Env, "MODELCACHE_NAMESPACE", modelCache.Namespace)
	assertEnvValue(t, container.Env, "HF_REVISION", modelCache.Spec.Source.Huggingface.Revision)
	assertSecretEnv(t, container.Env, "HF_TOKEN", "hf-token", "token")

	if !containsVolumeMount(container.VolumeMounts, "model-storage", "/model") {
		t.Fatalf("expected model-storage volume mount, got %#v", container.VolumeMounts)
	}
	if !containsPVCVolume(job.Spec.Template.Spec.Volumes, "model-storage", pvc.Name) {
		t.Fatalf("expected model-storage PVC volume, got %#v", job.Spec.Template.Spec.Volumes)
	}
}

func TestDownloadJobForModelCacheOmitsOptionalEnv(t *testing.T) {
	modelCache := testModelCache()
	modelCache.Spec.Source.Huggingface.Revision = ""
	modelCache.Spec.Source.Huggingface.Token.SecretRef = praestov1alpha1.SecretRef{}
	job := DownloadJobForModelCache(modelCache, testPVC(modelCache))
	container := job.Spec.Template.Spec.Containers[0]

	assertEnvMissing(t, container.Env, "HF_REVISION")
	assertEnvMissing(t, container.Env, "HF_TOKEN")
}

func TestEnsureDownloadJob(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	modelCache := testModelCache()
	pvc := testPVC(modelCache)

	t.Run("creates Job with owner reference", func(t *testing.T) {
		k8sClient := newTestClient(scheme, modelCache, pvc)

		job, err := EnsureDownloadJob(ctx, k8sClient, scheme, modelCache, pvc)
		if err != nil {
			t.Fatalf("ensure Job: %v", err)
		}

		if job.Name != JobNameForModelCache(modelCache.Name) {
			t.Fatalf("unexpected Job name: %s", job.Name)
		}
		assertOwnerReference(t, job.OwnerReferences, "ModelCache", modelCache.Name)
		storedJob := &batchv1.Job{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, storedJob); err != nil {
			t.Fatalf("get stored Job: %v", err)
		}
		assertLabels(t, storedJob.Labels, DownloadJobLabels(modelCache.Name))
	})

	t.Run("returns existing Job without duplicating it", func(t *testing.T) {
		existingJob := DownloadJobForModelCache(modelCache, pvc)
		k8sClient := newTestClient(scheme, modelCache, pvc, existingJob)

		job, err := EnsureDownloadJob(ctx, k8sClient, scheme, modelCache, pvc)
		if err != nil {
			t.Fatalf("ensure Job: %v", err)
		}
		if job.Name != existingJob.Name {
			t.Fatalf("expected existing Job %s, got %s", existingJob.Name, job.Name)
		}

		jobList := &batchv1.JobList{}
		if err := k8sClient.List(ctx, jobList, client.InNamespace(modelCache.Namespace)); err != nil {
			t.Fatalf("list Jobs: %v", err)
		}
		if len(jobList.Items) != 1 {
			t.Fatalf("expected one Job, got %d", len(jobList.Items))
		}
	})
}

func TestIsDownloadJobComplete(t *testing.T) {
	t.Run("returns true when Job completed", func(t *testing.T) {
		complete, err := IsDownloadJobComplete(jobWithCondition(batchv1.JobComplete, corev1.ConditionTrue, "done"))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !complete {
			t.Fatalf("expected complete Job")
		}
	})

	t.Run("returns false when Job is still running", func(t *testing.T) {
		complete, err := IsDownloadJobComplete(&batchv1.Job{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if complete {
			t.Fatalf("expected incomplete Job")
		}
	})

	t.Run("returns error when Job failed", func(t *testing.T) {
		complete, err := IsDownloadJobComplete(jobWithCondition(batchv1.JobFailed, corev1.ConditionTrue, "download failed"))
		if err == nil || !strings.Contains(err.Error(), "download failed") {
			t.Fatalf("expected failed Job error, got %v", err)
		}
		if complete {
			t.Fatalf("expected failed Job not to be complete")
		}
	})
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

func newTestClient(scheme *runtime.Scheme, objects ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func testModelCache() *praestov1alpha1.ModelCache {
	return &praestov1alpha1.ModelCache{
		TypeMeta: metav1.TypeMeta{
			APIVersion: praestov1alpha1.GroupVersion.String(),
			Kind:       "ModelCache",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tinyllama-test",
			Namespace: "default",
			UID:       "modelcache-uid",
		},
		Spec: praestov1alpha1.ModelCacheSpec{
			Source: praestov1alpha1.Source{Huggingface: praestov1alpha1.HuggingfaceSource{
				Repo:     "TinyLlama/TinyLlama-1.1B-Chat-v1.0",
				Revision: "main",
				Token: praestov1alpha1.Token{SecretRef: praestov1alpha1.SecretRef{
					Name: "hf-token",
					Key:  "token",
				}},
			}},
			Storage: praestov1alpha1.Storage{
				StorageClassName: "fast-rwx",
				Size:             "10Gi",
			},
		},
	}
}

func testPVC(modelCache *praestov1alpha1.ModelCache) *corev1.PersistentVolumeClaim {
	storageClassName := modelCache.Spec.Storage.StorageClassName
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PVCNameForModelCache(modelCache.Name),
			Namespace: modelCache.Namespace,
			Labels:    ModelCacheLabels(modelCache.Name),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: &storageClassName,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(modelCache.Spec.Storage.Size),
			}},
		},
	}
}

func jobWithCondition(conditionType batchv1.JobConditionType, status corev1.ConditionStatus, message string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "download", Namespace: "default"},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type:    conditionType,
			Status:  status,
			Message: message,
		}}},
	}
}

func assertLabels(t *testing.T, actual, expected map[string]string) {
	t.Helper()
	for key, expectedValue := range expected {
		if actual[key] != expectedValue {
			t.Fatalf("expected label %s=%s, got %q", key, expectedValue, actual[key])
		}
	}
}

func assertOwnerReference(t *testing.T, ownerReferences []metav1.OwnerReference, kind, name string) {
	t.Helper()
	for _, ownerReference := range ownerReferences {
		if ownerReference.Kind == kind && ownerReference.Name == name && ownerReference.Controller != nil && *ownerReference.Controller {
			return
		}
	}
	t.Fatalf("expected controller owner reference %s/%s, got %#v", kind, name, ownerReferences)
}

func assertEnvValue(t *testing.T, env []corev1.EnvVar, name, value string) {
	t.Helper()
	for _, envVar := range env {
		if envVar.Name == name {
			if envVar.Value != value {
				t.Fatalf("expected env %s=%s, got %s", name, value, envVar.Value)
			}
			return
		}
	}
	t.Fatalf("expected env %s", name)
}

func assertEnvMissing(t *testing.T, env []corev1.EnvVar, name string) {
	t.Helper()
	for _, envVar := range env {
		if envVar.Name == name {
			t.Fatalf("expected env %s to be absent, got %#v", name, envVar)
		}
	}
}

func assertSecretEnv(t *testing.T, env []corev1.EnvVar, name, secretName, secretKey string) {
	t.Helper()
	for _, envVar := range env {
		if envVar.Name != name {
			continue
		}
		if envVar.ValueFrom == nil || envVar.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("expected env %s to come from secret, got %#v", name, envVar)
		}
		if envVar.ValueFrom.SecretKeyRef.Name != secretName || envVar.ValueFrom.SecretKeyRef.Key != secretKey {
			t.Fatalf("unexpected secret env %s: %#v", name, envVar.ValueFrom.SecretKeyRef)
		}
		return
	}
	t.Fatalf("expected secret env %s", name)
}

func containsAccessMode(accessModes []corev1.PersistentVolumeAccessMode, expected corev1.PersistentVolumeAccessMode) bool {
	for _, accessMode := range accessModes {
		if accessMode == expected {
			return true
		}
	}
	return false
}

func containsVolumeMount(volumeMounts []corev1.VolumeMount, name, mountPath string) bool {
	for _, volumeMount := range volumeMounts {
		if volumeMount.Name == name && volumeMount.MountPath == mountPath {
			return true
		}
	}
	return false
}

func containsPVCVolume(volumes []corev1.Volume, name, claimName string) bool {
	for _, volume := range volumes {
		if volume.Name == name && volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == claimName {
			return true
		}
	}
	return false
}
