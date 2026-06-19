package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	ModelAnnotationKey     = "praesto.io/model-cache"
	ModelPathAnnotationKey = "praesto.io/model-mount-path"
	ModelContainerNameKey  = "praesto.io/target-container"
	DefaultModelMountPath  = "/models"

	ModelCacheVolumeName             = "praesto-model-cache"
	PraestoCSIDriverName             = "csi.praesto.io"
	CSIVolumeAttributeModelNamespace = "modelCacheNamespace"
	CSIVolumeAttributeModelCacheName = "modelCacheName"
)

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

	modelCacheName, hasModelCacheAnnotation := pod.Annotations[ModelAnnotationKey]
	modelCacheName = strings.TrimSpace(modelCacheName)
	if !hasModelCacheAnnotation {
		return admission.Allowed("no model cache annotation, skipping mutation")
	}
	if modelCacheName == "" {
		return admission.Errored(400, fmt.Errorf("model cache annotation is empty"))
	}

	modelMountPath := strings.TrimSpace(pod.Annotations[ModelPathAnnotationKey])
	if modelMountPath == "" {
		modelMountPath = DefaultModelMountPath
	}
	if len(pod.Spec.Containers) == 0 {
		return admission.Errored(400, fmt.Errorf("pod has no containers to mount model cache into"))
	}

	modelCache := &praestov1alpha1.ModelCache{}
	if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: modelCacheName}, modelCache); err != nil {
		return admission.Errored(400, fmt.Errorf("unable to fetch ModelCache %s: %w", modelCacheName, err))
	}

	if modelCache.Status.Phase != praestov1alpha1.ModelCachePhaseReady {
		return admission.Errored(400, fmt.Errorf("model cache %s is not ready", modelCacheName))
	}
	if err := ensureModelCacheVolume(pod, modelCache); err != nil {
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
				if err := ensureModelCacheVolumeMount(&pod.Spec.Containers[i], ModelCacheVolumeName, modelMountPath); err != nil {
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
		if err := ensureModelCacheVolumeMount(&pod.Spec.Containers[0], ModelCacheVolumeName, modelMountPath); err != nil {
			return admission.Errored(400, err)
		}
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(500, fmt.Errorf("unable to marshal mutated pod: %w", err))
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func ensureModelCacheVolume(pod *corev1.Pod, modelCache *praestov1alpha1.ModelCache) error {
	if usesCSIVolume(modelCache) {
		return ensureModelCacheCSIVolume(pod, modelCache)
	}

	return ensureModelCachePVCVolume(pod, modelCache)
}

func usesCSIVolume(modelCache *praestov1alpha1.ModelCache) bool {
	return modelCache.Spec.Storage.StorageClassName == ""
}

func ensureModelCacheCSIVolume(pod *corev1.Pod, modelCache *praestov1alpha1.ModelCache) error {
	for _, volume := range pod.Spec.Volumes {
		if volume.Name != ModelCacheVolumeName {
			continue
		}

		csi := volume.CSI
		if csi == nil || csi.Driver != PraestoCSIDriverName || csi.ReadOnly == nil || !*csi.ReadOnly || csi.VolumeAttributes[CSIVolumeAttributeModelNamespace] != modelCache.Namespace || csi.VolumeAttributes[CSIVolumeAttributeModelCacheName] != modelCache.Name {
			return fmt.Errorf("volume %s already exists with a conflicting configuration", ModelCacheVolumeName)
		}
		return nil
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: ModelCacheVolumeName,
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

func ensureModelCachePVCVolume(pod *corev1.Pod, modelCache *praestov1alpha1.ModelCache) error {
	pvcName := modelCache.Status.PvcName
	if pvcName == "" {
		return fmt.Errorf("model cache %s does not have a PVC associated yet", modelCache.Name)
	}

	for _, volume := range pod.Spec.Volumes {
		if volume.Name != ModelCacheVolumeName {
			continue
		}

		pvc := volume.PersistentVolumeClaim
		if pvc == nil || pvc.ClaimName != pvcName || !pvc.ReadOnly {
			return fmt.Errorf("volume %s already exists with a conflicting configuration", ModelCacheVolumeName)
		}
		return nil
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: ModelCacheVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
				ReadOnly:  true,
			},
		},
	})
	return nil
}

func boolPtr(value bool) *bool {
	return &value
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
