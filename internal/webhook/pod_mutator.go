package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	"github.com/federicolepera/praesto/internal/downloader"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	ModelMountsAnnotationKey = "praesto.io/model-mounts"
	ModelContainerNameKey    = "praesto.io/target-container"
	UsesModelCacheLabelKey   = "praesto.io/uses-model-cache"
	DefaultModelMountPath    = "/models"
	WaitForCacheImage        = "busybox:1.36"

	ModelCacheVolumeName             = "praesto-model-cache"
	ModelCacheVolumeNamePrefix       = "praesto-model-cache-"
	WaitForCacheContainerNamePrefix  = "praesto-wait-model-cache-"
	PraestoCSIDriverName             = "csi.praesto.io"
	CSIVolumeAttributeModelNamespace = "modelCacheNamespace"
	CSIVolumeAttributeModelCacheName = "modelCacheName"
)

type modelMountAnnotation struct {
	ModelCache string `json:"modelCache"`
	MountPath  string `json:"mountPath"`
}

type resolvedModelMount struct {
	modelCache *praestov1alpha1.ModelCache
	mountPath  string
	volumeName string
}

type PodMutator struct {
	Client  client.Client
	Decoder admission.Decoder
}

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}

	if err := m.Decoder.Decode(req, pod); err != nil {
		return admission.Errored(400, err)
	}

	modelMounts, err := m.resolveModelMounts(ctx, pod)
	if err != nil {
		return admission.Errored(400, err)
	}
	if len(modelMounts) == 0 {
		return admission.Allowed("no model cache annotation, skipping mutation")
	}
	if len(pod.Spec.Containers) == 0 {
		return admission.Errored(400, fmt.Errorf("pod has no containers to mount model cache into"))
	}

	for _, modelMount := range modelMounts {
		if err := ensureModelCacheVolume(pod, modelMount.volumeName, modelMount.modelCache); err != nil {
			return admission.Errored(400, err)
		}
	}
	if err := ensurePVCModelCacheWaitInitContainers(pod, modelMounts); err != nil {
		return admission.Errored(400, err)
	}

	targetContainerName, hasTargetContainerAnnotation := pod.Annotations[ModelContainerNameKey]
	targetContainerName = strings.TrimSpace(targetContainerName)
	if hasTargetContainerAnnotation {
		if targetContainerName == "" {
			return admission.Errored(400, fmt.Errorf("target container annotation is empty"))
		}
		found := false
		for i, container := range pod.Spec.Containers {
			if container.Name == targetContainerName {
				if err := ensureModelCacheVolumeMounts(&pod.Spec.Containers[i], modelMounts); err != nil {
					return admission.Errored(400, err)
				}
				found = true
				break
			}
		}
		if !found {
			return admission.Errored(400, fmt.Errorf("target container %s not found in pod", targetContainerName))
		}
	} else {
		if err := ensureModelCacheVolumeMounts(&pod.Spec.Containers[0], modelMounts); err != nil {
			return admission.Errored(400, err)
		}
	}
	ensureUsesModelCacheLabel(pod)

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(500, fmt.Errorf("unable to marshal mutated pod: %w", err))
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func ensureUsesModelCacheLabel(pod *corev1.Pod) {
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels[UsesModelCacheLabelKey] = "true"
}

func (m *PodMutator) resolveModelMounts(ctx context.Context, pod *corev1.Pod) ([]resolvedModelMount, error) {
	annotations := pod.GetAnnotations()
	modelMountsJSON, hasModelMountsAnnotation := annotations[ModelMountsAnnotationKey]

	if hasModelMountsAnnotation {
		return m.resolveMultiModelMounts(ctx, pod.Namespace, modelMountsJSON)
	}

	return nil, nil
}

func (m *PodMutator) resolveMultiModelMounts(ctx context.Context, namespace, annotationValue string) ([]resolvedModelMount, error) {
	annotationValue = strings.TrimSpace(annotationValue)
	if annotationValue == "" {
		return nil, fmt.Errorf("model mounts annotation is empty")
	}

	var requestedMounts []modelMountAnnotation
	if err := json.Unmarshal([]byte(annotationValue), &requestedMounts); err != nil {
		return nil, fmt.Errorf("unable to parse %s as JSON: %w", ModelMountsAnnotationKey, err)
	}
	if len(requestedMounts) == 0 {
		return nil, fmt.Errorf("model mounts annotation must contain at least one mount")
	}

	modelMounts := make([]resolvedModelMount, 0, len(requestedMounts))
	seenMountPaths := map[string]struct{}{}
	for i, requestedMount := range requestedMounts {
		modelCacheName := strings.TrimSpace(requestedMount.ModelCache)
		if modelCacheName == "" {
			return nil, fmt.Errorf("model mounts entry %d has an empty modelCache", i)
		}

		mountPath, err := cleanMountPath(strings.TrimSpace(requestedMount.MountPath))
		if err != nil {
			return nil, fmt.Errorf("model mounts entry %d has invalid mountPath: %w", i, err)
		}
		if _, exists := seenMountPaths[mountPath]; exists {
			return nil, fmt.Errorf("model mounts annotation contains duplicate mountPath %s", mountPath)
		}
		seenMountPaths[mountPath] = struct{}{}

		modelCache, err := m.fetchInjectableModelCache(ctx, namespace, modelCacheName)
		if err != nil {
			return nil, err
		}

		volumeName := ModelCacheVolumeName
		if len(requestedMounts) > 1 {
			volumeName = fmt.Sprintf("%s%d", ModelCacheVolumeNamePrefix, i)
		}
		modelMounts = append(modelMounts, resolvedModelMount{
			modelCache: modelCache,
			mountPath:  mountPath,
			volumeName: volumeName,
		})
	}

	if err := rejectOverlappingMountPaths(modelMounts); err != nil {
		return nil, err
	}
	return modelMounts, nil
}

func (m *PodMutator) fetchInjectableModelCache(ctx context.Context, namespace, name string) (*praestov1alpha1.ModelCache, error) {
	modelCache := &praestov1alpha1.ModelCache{}
	if err := m.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, modelCache); err != nil {
		return nil, fmt.Errorf("unable to fetch ModelCache %s: %w", name, err)
	}
	if usesCSIVolume(modelCache) {
		if modelCache.Status.Phase == praestov1alpha1.ModelCachePhaseFailed {
			return nil, fmt.Errorf("model cache %s is failed", name)
		}
		return modelCache, nil
	}
	if modelCache.Status.Phase != praestov1alpha1.ModelCachePhaseReady && modelCache.Status.Phase != praestov1alpha1.ModelCachePhaseEvicted {
		return nil, fmt.Errorf("model cache %s is not ready", name)
	}
	return modelCache, nil
}

func cleanMountPath(mountPath string) (string, error) {
	if mountPath == "" {
		return "", fmt.Errorf("mountPath is empty")
	}
	if !path.IsAbs(mountPath) {
		return "", fmt.Errorf("mountPath must be absolute")
	}
	for _, segment := range strings.Split(mountPath, "/") {
		if segment == ".." {
			return "", fmt.Errorf("mountPath must not contain parent directory segments")
		}
	}
	cleaned := path.Clean(mountPath)
	if cleaned == "/" {
		return "", fmt.Errorf("mountPath must not be /")
	}
	return cleaned, nil
}

func rejectOverlappingMountPaths(modelMounts []resolvedModelMount) error {
	mountPaths := make([]string, 0, len(modelMounts))
	for _, modelMount := range modelMounts {
		mountPaths = append(mountPaths, modelMount.mountPath)
	}
	sort.Strings(mountPaths)

	for i := 1; i < len(mountPaths); i++ {
		parent := mountPaths[i-1]
		child := mountPaths[i]
		if strings.HasPrefix(child, parent+"/") {
			return fmt.Errorf("model mounts annotation contains overlapping mountPaths %s and %s", parent, child)
		}
	}
	return nil
}

func ensureModelCacheVolume(pod *corev1.Pod, volumeName string, modelCache *praestov1alpha1.ModelCache) error {
	if usesCSIVolume(modelCache) {
		return ensureModelCacheCSIVolume(pod, volumeName, modelCache)
	}

	return ensureModelCachePVCVolume(pod, volumeName, modelCache)
}

func usesCSIVolume(modelCache *praestov1alpha1.ModelCache) bool {
	return modelCache.Spec.Storage.StorageClassName == ""
}

func ensureModelCacheCSIVolume(pod *corev1.Pod, volumeName string, modelCache *praestov1alpha1.ModelCache) error {
	for _, volume := range pod.Spec.Volumes {
		if volume.Name != volumeName {
			continue
		}

		csi := volume.CSI
		if csi == nil || csi.Driver != PraestoCSIDriverName || csi.ReadOnly == nil || !*csi.ReadOnly || csi.VolumeAttributes[CSIVolumeAttributeModelNamespace] != modelCache.Namespace || csi.VolumeAttributes[CSIVolumeAttributeModelCacheName] != modelCache.Name {
			return fmt.Errorf("volume %s already exists with a conflicting configuration", volumeName)
		}
		return nil
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			CSI: &corev1.CSIVolumeSource{
				Driver:   PraestoCSIDriverName,
				ReadOnly: boolPtr(true),
				VolumeAttributes: map[string]string{
					CSIVolumeAttributeModelNamespace: modelCache.Namespace,
					CSIVolumeAttributeModelCacheName: modelCache.Name,
				},
			},
		},
	})
	return nil
}

func ensureModelCachePVCVolume(pod *corev1.Pod, volumeName string, modelCache *praestov1alpha1.ModelCache) error {
	pvcName := modelCache.Status.PvcName
	if pvcName == "" {
		pvcName = downloader.PVCNameForModelCache(modelCache.Name)
	}

	for _, volume := range pod.Spec.Volumes {
		if volume.Name != volumeName {
			continue
		}

		pvc := volume.PersistentVolumeClaim
		if pvc == nil || pvc.ClaimName != pvcName || !pvc.ReadOnly {
			return fmt.Errorf("volume %s already exists with a conflicting configuration", volumeName)
		}
		return nil
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
				ReadOnly:  true,
			},
		},
	})
	return nil
}

func ensurePVCModelCacheWaitInitContainers(pod *corev1.Pod, modelMounts []resolvedModelMount) error {
	for _, modelMount := range modelMounts {
		if usesCSIVolume(modelMount.modelCache) {
			continue
		}
		if err := ensurePVCModelCacheWaitInitContainer(pod, modelMount); err != nil {
			return err
		}
	}
	return nil
}

func ensurePVCModelCacheWaitInitContainer(pod *corev1.Pod, modelMount resolvedModelMount) error {
	containerName := waitForCacheContainerName(modelMount.volumeName)
	command := []string{"sh", "-c", fmt.Sprintf("until [ -f %s/.praesto-complete ]; do echo waiting for Praesto model cache %s; sleep 2; done", modelMount.mountPath, modelMount.modelCache.Name)}
	mount := corev1.VolumeMount{Name: modelMount.volumeName, MountPath: modelMount.mountPath, ReadOnly: true}

	for i, container := range pod.Spec.InitContainers {
		if container.Name != containerName {
			continue
		}
		if container.Image != WaitForCacheImage || !stringSlicesEqual(container.Command, command) || !volumeMountsContain(container.VolumeMounts, mount) {
			return fmt.Errorf("init container %s already exists with a conflicting configuration", containerName)
		}
		pod.Spec.InitContainers[i].VolumeMounts = appendMissingVolumeMount(pod.Spec.InitContainers[i].VolumeMounts, mount)
		return nil
	}

	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:         containerName,
		Image:        WaitForCacheImage,
		Command:      command,
		VolumeMounts: []corev1.VolumeMount{mount},
	})
	return nil
}

func waitForCacheContainerName(volumeName string) string {
	return strings.TrimSuffix(WaitForCacheContainerNamePrefix+volumeName, "-")
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func volumeMountsContain(mounts []corev1.VolumeMount, expected corev1.VolumeMount) bool {
	for _, mount := range mounts {
		if mount.Name == expected.Name && mount.MountPath == expected.MountPath && mount.ReadOnly == expected.ReadOnly {
			return true
		}
	}
	return false
}

func appendMissingVolumeMount(mounts []corev1.VolumeMount, mount corev1.VolumeMount) []corev1.VolumeMount {
	if volumeMountsContain(mounts, mount) {
		return mounts
	}
	return append(mounts, mount)
}

func boolPtr(value bool) *bool {
	return &value
}

func ensureModelCacheVolumeMounts(container *corev1.Container, modelMounts []resolvedModelMount) error {
	for _, modelMount := range modelMounts {
		if err := ensureModelCacheVolumeMount(container, modelMount.volumeName, modelMount.mountPath); err != nil {
			return err
		}
	}
	return nil
}

func ensureModelCacheVolumeMount(container *corev1.Container, volumeName, mountPath string) error {
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName {
			if mount.MountPath == mountPath && mount.ReadOnly {
				return nil
			}
			return fmt.Errorf("container %s already has volume mount %s with a conflicting configuration", container.Name, volumeName)
		}

		if mount.MountPath == mountPath {
			return fmt.Errorf("container %s already has a volume mounted at %s", container.Name, mountPath)
		}
	}

	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: mountPath,
		ReadOnly:  true,
	})
	return nil
}
