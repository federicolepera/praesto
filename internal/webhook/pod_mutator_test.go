package webhook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const testModelCacheVolumeName = "praesto-model-cache"

func TestPodMutatorHandle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := praestov1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add praesto scheme: %v", err)
	}

	t.Run("allows pod without model cache annotation", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme)
		resp, _ := handlePod(t, mutator, podWithContainers("app"))

		if !resp.Allowed {
			t.Fatalf("expected request to be allowed, got %q", resp.Result.Message)
		}
		if len(resp.Patches) != 0 || len(resp.Patch) != 0 {
			t.Fatalf("expected no patch for unannotated pod")
		}
	})

	t.Run("rejects empty model cache annotation", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme)
		pod := podWithContainers("app")
		pod.Annotations = map[string]string{ModelAnnotationKey: "   "}

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "model cache annotation is empty")
	})

	t.Run("rejects missing model cache", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme)
		pod := annotatedPod(podWithContainers("app"))

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "unable to fetch ModelCache")
	})

	t.Run("rejects model cache that is not ready", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, modelCacheWithStatus(praestov1alpha1.ModelCachePhasePending, "praesto-tinyllama"))
		pod := annotatedPod(podWithContainers("app"))

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "is not ready")
	})

	t.Run("rejects legacy ready model cache without PVC", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, legacyModelCacheWithStatus(praestov1alpha1.ModelCachePhaseReady, ""))
		pod := annotatedPod(podWithContainers("app"))

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "does not have a PVC")
	})

	t.Run("rejects annotated pod without containers", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers())

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "pod has no containers")
	})

	t.Run("mounts local ready model cache as a CSI volume into a single container", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))

		resp, mutatedPod := handlePod(t, mutator, pod)

		assertAllowed(t, resp)
		assertModelCSIVolume(t, mutatedPod, "default", "tinyllama-test")
		assertAppContainerMount(t, mutatedPod, DefaultModelMountPath)
	})

	t.Run("mounts legacy ready model cache as a PVC volume", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, legacyReadyModelCache())
		pod := annotatedPod(podWithContainers("app"))

		resp, mutatedPod := handlePod(t, mutator, pod)

		assertAllowed(t, resp)
		assertModelPVCVolume(t, mutatedPod, "praesto-tinyllama")
		assertAppContainerMount(t, mutatedPod, DefaultModelMountPath)
	})

	t.Run("mounts multiple local ready model caches as CSI volumes", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme,
			namedReadyModelCache("resnet-v1"),
			namedReadyModelCache("resnet-v2"),
			namedReadyModelCache("yolov8-v1"),
		)
		pod := podWithContainers("openvino")
		pod.Annotations = map[string]string{
			ModelMountsAnnotationKey: `[
				{"modelCache":"resnet-v1","mountPath":"/models/resnet/1"},
				{"modelCache":"resnet-v2","mountPath":"/models/resnet/2"},
				{"modelCache":"yolov8-v1","mountPath":"/models/yolov8/1"}
			]`,
		}

		resp, mutatedPod := handlePod(t, mutator, pod)

		assertAllowed(t, resp)
		assertModelCSIVolumeNamed(t, mutatedPod, "praesto-model-cache-0", "default", "resnet-v1")
		assertModelCSIVolumeNamed(t, mutatedPod, "praesto-model-cache-1", "default", "resnet-v2")
		assertModelCSIVolumeNamed(t, mutatedPod, "praesto-model-cache-2", "default", "yolov8-v1")
		assertContainerMountNamed(t, mutatedPod, "openvino", "praesto-model-cache-0", "/models/resnet/1")
		assertContainerMountNamed(t, mutatedPod, "openvino", "praesto-model-cache-1", "/models/resnet/2")
		assertContainerMountNamed(t, mutatedPod, "openvino", "praesto-model-cache-2", "/models/yolov8/1")
	})

	t.Run("mounts multiple legacy model caches as PVC volumes", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme,
			namedLegacyReadyModelCache("bert-v1", "praesto-bert-v1"),
			namedLegacyReadyModelCache("bert-v2", "praesto-bert-v2"),
		)
		pod := podWithContainers("openvino")
		pod.Annotations = map[string]string{
			ModelMountsAnnotationKey: `[
				{"modelCache":"bert-v1","mountPath":"/models/bert/1"},
				{"modelCache":"bert-v2","mountPath":"/models/bert/2"}
			]`,
		}

		resp, mutatedPod := handlePod(t, mutator, pod)

		assertAllowed(t, resp)
		assertModelPVCVolumeNamed(t, mutatedPod, "praesto-model-cache-0", "praesto-bert-v1")
		assertModelPVCVolumeNamed(t, mutatedPod, "praesto-model-cache-1", "praesto-bert-v2")
		assertContainerMountNamed(t, mutatedPod, "openvino", "praesto-model-cache-0", "/models/bert/1")
		assertContainerMountNamed(t, mutatedPod, "openvino", "praesto-model-cache-1", "/models/bert/2")
	})

	t.Run("rejects single and multi model annotations together", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))
		pod.Annotations[ModelMountsAnnotationKey] = `[{"modelCache":"tinyllama-test","mountPath":"/models/tinyllama/1"}]`

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "cannot be used together")
	})

	t.Run("rejects invalid multi model mount path", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, namedReadyModelCache("resnet-v1"))
		pod := podWithContainers("openvino")
		pod.Annotations = map[string]string{
			ModelMountsAnnotationKey: `[{"modelCache":"resnet-v1","mountPath":"models/resnet/1"}]`,
		}

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "mountPath must be absolute")
	})

	t.Run("rejects duplicate multi model mount paths", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, namedReadyModelCache("resnet-v1"), namedReadyModelCache("bert-v1"))
		pod := podWithContainers("openvino")
		pod.Annotations = map[string]string{
			ModelMountsAnnotationKey: `[
				{"modelCache":"resnet-v1","mountPath":"/models/shared/1"},
				{"modelCache":"bert-v1","mountPath":"/models/shared/1"}
			]`,
		}

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "duplicate mountPath")
	})

	t.Run("rejects overlapping multi model mount paths", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, namedReadyModelCache("resnet-v1"), namedReadyModelCache("bert-v1"))
		pod := podWithContainers("openvino")
		pod.Annotations = map[string]string{
			ModelMountsAnnotationKey: `[
				{"modelCache":"resnet-v1","mountPath":"/models/resnet"},
				{"modelCache":"bert-v1","mountPath":"/models/resnet/1"}
			]`,
		}

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "overlapping mountPaths")
	})

	t.Run("uses custom mount path", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))
		pod.Annotations[ModelPathAnnotationKey] = " /mnt/model "

		resp, mutatedPod := handlePod(t, mutator, pod)

		assertAllowed(t, resp)
		assertAppContainerMount(t, mutatedPod, "/mnt/model")
	})

	t.Run("mounts into first container when target container is omitted", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app", "sidecar"))

		resp, mutatedPod := handlePod(t, mutator, pod)

		assertAllowed(t, resp)
		assertAppContainerMount(t, mutatedPod, DefaultModelMountPath)
		assertContainerHasNoMount(t, mutatedPod, "sidecar")
	})

	t.Run("mounts into selected target container", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("sidecar", "app"))
		pod.Annotations[ModelContainerNameKey] = " app "

		resp, mutatedPod := handlePod(t, mutator, pod)

		assertAllowed(t, resp)
		assertContainerHasNoMount(t, mutatedPod, "sidecar")
		assertAppContainerMount(t, mutatedPod, DefaultModelMountPath)
	})

	t.Run("rejects empty target container annotation", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))
		pod.Annotations[ModelContainerNameKey] = "   "

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "target container annotation is empty")
	})

	t.Run("rejects unknown target container", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))
		pod.Annotations[ModelContainerNameKey] = "worker"

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "target container worker not found")
	})

	t.Run("does not duplicate existing identical volume and mount", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))
		pod.Spec.Volumes = []corev1.Volume{modelCacheCSIVolume("default", "tinyllama-test")}
		pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{modelCacheMount(DefaultModelMountPath, true)}

		resp, mutatedPod := handlePod(t, mutator, pod)

		assertAllowed(t, resp)
		if got := len(mutatedPod.Spec.Volumes); got != 1 {
			t.Fatalf("expected one volume, got %d", got)
		}
		if got := len(mutatedPod.Spec.Containers[0].VolumeMounts); got != 1 {
			t.Fatalf("expected one volume mount, got %d", got)
		}
	})

	t.Run("rejects conflicting existing model cache volume", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))
		pod.Spec.Volumes = []corev1.Volume{modelCacheVolume("other-pvc", true)}

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "volume praesto-model-cache already exists")
	})

	t.Run("rejects conflicting existing model cache mount", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))
		pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{modelCacheMount("/other", true)}

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "already has volume mount praesto-model-cache")
	})

	t.Run("rejects occupied mount path", func(t *testing.T) {
		mutator := newTestPodMutator(t, scheme, readyModelCache())
		pod := annotatedPod(podWithContainers("app"))
		pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "other", MountPath: DefaultModelMountPath, ReadOnly: true}}

		resp, _ := handlePod(t, mutator, pod)

		assertRejected(t, resp, "already has a volume mounted at /models")
	})
}

func newTestPodMutator(t *testing.T, scheme *runtime.Scheme, objects ...runtime.Object) *PodMutator {
	t.Helper()

	return &PodMutator{
		Client:  fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build(),
		Decoder: admission.NewDecoder(scheme),
	}
}

func handlePod(t *testing.T, mutator *PodMutator, pod *corev1.Pod) (admission.Response, *corev1.Pod) {
	t.Helper()

	rawPod, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("marshal pod: %v", err)
	}

	resp := mutator.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Namespace: pod.Namespace,
		Object: runtime.RawExtension{
			Raw: rawPod,
		},
	}})

	if !resp.Allowed {
		return resp, pod
	}

	patchBytes := resp.Patch
	if len(patchBytes) == 0 && len(resp.Patches) > 0 {
		patchBytes, err = json.Marshal(resp.Patches)
		if err != nil {
			t.Fatalf("marshal admission patches: %v", err)
		}
	}
	if len(patchBytes) == 0 {
		return resp, pod
	}

	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		t.Fatalf("decode admission patch: %v", err)
	}
	mutatedRawPod, err := patch.Apply(rawPod)
	if err != nil {
		t.Fatalf("apply admission patch: %v", err)
	}

	mutatedPod := &corev1.Pod{}
	if err := json.Unmarshal(mutatedRawPod, mutatedPod); err != nil {
		t.Fatalf("unmarshal mutated pod: %v", err)
	}
	return resp, mutatedPod
}

func readyModelCache() *praestov1alpha1.ModelCache {
	return modelCacheWithStatus(praestov1alpha1.ModelCachePhaseReady, "")
}

func namedReadyModelCache(name string) *praestov1alpha1.ModelCache {
	modelCache := modelCacheWithStatus(praestov1alpha1.ModelCachePhaseReady, "")
	modelCache.Name = name
	return modelCache
}

func legacyReadyModelCache() *praestov1alpha1.ModelCache {
	return legacyModelCacheWithStatus(praestov1alpha1.ModelCachePhaseReady, "praesto-tinyllama")
}

func namedLegacyReadyModelCache(name, pvcName string) *praestov1alpha1.ModelCache {
	modelCache := legacyModelCacheWithStatus(praestov1alpha1.ModelCachePhaseReady, pvcName)
	modelCache.Name = name
	return modelCache
}

func modelCacheWithStatus(phase, pvcName string) *praestov1alpha1.ModelCache {
	return &praestov1alpha1.ModelCache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tinyllama-test",
			Namespace: "default",
		},
		Status: praestov1alpha1.ModelCacheStatus{
			Phase:   phase,
			PvcName: pvcName,
		},
	}
}

func legacyModelCacheWithStatus(phase, pvcName string) *praestov1alpha1.ModelCache {
	modelCache := modelCacheWithStatus(phase, pvcName)
	modelCache.Spec.Storage.StorageClassName = "standard"
	return modelCache
}

func podWithContainers(containerNames ...string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	for _, name := range containerNames {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name:  name,
			Image: "busybox:1.36",
		})
	}
	return pod
}

func annotatedPod(pod *corev1.Pod) *corev1.Pod {
	pod.Annotations = map[string]string{ModelAnnotationKey: "tinyllama-test"}
	return pod
}

func modelCacheVolume(pvcName string, readOnly bool) corev1.Volume {
	return corev1.Volume{
		Name: testModelCacheVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
				ReadOnly:  readOnly,
			},
		},
	}
}

func modelCacheCSIVolume(namespace, name string) corev1.Volume {
	return corev1.Volume{
		Name: testModelCacheVolumeName,
		VolumeSource: corev1.VolumeSource{
			CSI: &corev1.CSIVolumeSource{
				Driver:   PraestoCSIDriverName,
				ReadOnly: boolPtr(true),
				VolumeAttributes: map[string]string{
					CSIVolumeAttributeModelNamespace: namespace,
					CSIVolumeAttributeModelCacheName: name,
				},
			},
		},
	}
}

func modelCacheMount(mountPath string, readOnly bool) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      testModelCacheVolumeName,
		MountPath: mountPath,
		ReadOnly:  readOnly,
	}
}

func assertAllowed(t *testing.T, resp admission.Response) {
	t.Helper()
	if !resp.Allowed {
		t.Fatalf("expected request to be allowed, got %q", resp.Result.Message)
	}
}

func assertRejected(t *testing.T, resp admission.Response, expectedMessage string) {
	t.Helper()
	if resp.Allowed {
		t.Fatalf("expected request to be rejected")
	}
	if resp.Result == nil || !strings.Contains(resp.Result.Message, expectedMessage) {
		t.Fatalf("expected rejection message to contain %q, got %#v", expectedMessage, resp.Result)
	}
}

func assertModelPVCVolume(t *testing.T, pod *corev1.Pod, pvcName string) {
	assertModelPVCVolumeNamed(t, pod, testModelCacheVolumeName, pvcName)
}

func assertModelPVCVolumeNamed(t *testing.T, pod *corev1.Pod, volumeName, pvcName string) {
	t.Helper()
	for _, volume := range pod.Spec.Volumes {
		if volume.Name != volumeName {
			continue
		}
		if volume.PersistentVolumeClaim == nil {
			t.Fatalf("expected model volume to use a PVC")
		}
		if volume.PersistentVolumeClaim.ClaimName != pvcName || !volume.PersistentVolumeClaim.ReadOnly {
			t.Fatalf("unexpected model volume PVC config: %#v", volume.PersistentVolumeClaim)
		}
		return
	}
	t.Fatalf("expected model cache volume %s", volumeName)
}

func assertModelCSIVolume(t *testing.T, pod *corev1.Pod, namespace, name string) {
	assertModelCSIVolumeNamed(t, pod, testModelCacheVolumeName, namespace, name)
}

func assertModelCSIVolumeNamed(t *testing.T, pod *corev1.Pod, volumeName, namespace, name string) {
	t.Helper()
	for _, volume := range pod.Spec.Volumes {
		if volume.Name != volumeName {
			continue
		}
		if volume.CSI == nil {
			t.Fatalf("expected model volume to use CSI")
		}
		if volume.CSI.Driver != PraestoCSIDriverName || volume.CSI.ReadOnly == nil || !*volume.CSI.ReadOnly {
			t.Fatalf("unexpected model volume CSI config: %#v", volume.CSI)
		}
		if volume.CSI.VolumeAttributes[CSIVolumeAttributeModelNamespace] != namespace || volume.CSI.VolumeAttributes[CSIVolumeAttributeModelCacheName] != name {
			t.Fatalf("unexpected model volume CSI attributes: %#v", volume.CSI.VolumeAttributes)
		}
		return
	}
	t.Fatalf("expected model cache CSI volume %s", volumeName)
}

func assertAppContainerMount(t *testing.T, pod *corev1.Pod, mountPath string) {
	assertContainerMountNamed(t, pod, "app", testModelCacheVolumeName, mountPath)
}

func assertContainerMountNamed(t *testing.T, pod *corev1.Pod, containerName, volumeName, mountPath string) {
	t.Helper()
	container := findContainer(t, pod, containerName)
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName && mount.MountPath == mountPath && mount.ReadOnly {
			return
		}
	}
	t.Fatalf("expected container %s to mount volume %s at %s, got %#v", containerName, volumeName, mountPath, container.VolumeMounts)
}

func assertContainerHasNoMount(t *testing.T, pod *corev1.Pod, containerName string) {
	t.Helper()
	container := findContainer(t, pod, containerName)
	for _, mount := range container.VolumeMounts {
		if mount.Name == testModelCacheVolumeName {
			t.Fatalf("expected container %s not to mount model cache, got %#v", containerName, container.VolumeMounts)
		}
	}
}

func findContainer(t *testing.T, pod *corev1.Pod, containerName string) corev1.Container {
	t.Helper()
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			return container
		}
	}
	t.Fatalf("container %s not found", containerName)
	return corev1.Container{}
}
