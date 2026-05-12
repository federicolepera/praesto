package webhook

import (
	"context"
	"encoding/json"
	"fmt"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	ModelAnnotationKey     = "praesto.io/model-cache"
	ModelPathAnnotationKey = "praesto.io/model-mount-path"
	DefaultModelMountPath  = "/models"
)

type PodMutator struct {
	Client  client.Client
	Decoder admission.Decoder
}

func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}

	if err := m.Decoder.Decode(req, pod); err != nil {
		return admission.Errored(400, err)
	}

	modelCacheName, hasModelCacheAnnotation := pod.Annotations[ModelAnnotationKey]
	if !hasModelCacheAnnotation {
		return admission.Allowed("no model cache annotation, skipping mutation")
	}
	if modelCacheName == "" {
		return admission.Errored(400, fmt.Errorf("model cache annotation is empty"))
	}

	modelMountPath := pod.Annotations[ModelPathAnnotationKey]
	if modelMountPath == "" {
		modelMountPath = DefaultModelMountPath
	}

	modelCache := &praestov1alpha1.ModelCache{}
	if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: modelCacheName}, modelCache); err != nil {
		return admission.Errored(400, fmt.Errorf("unable to fetch ModelCache %s: %w", modelCacheName, err))
	}

	if modelCache.Status.Phase != praestov1alpha1.ModelCachePhaseReady {
		return admission.Errored(400, fmt.Errorf("model cache %s is not ready", modelCacheName))
	}
	pvcName := modelCache.Status.PvcName
	if pvcName == "" {
		return admission.Errored(400, fmt.Errorf("model cache %s does not have a PVC associated yet", modelCacheName))
	}

	volumeName := "praesto-model-cache"

	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
				ReadOnly:  true,
			},
		},
	})

	// TODO: handle multiple containers and potential name conflicts
	if len(pod.Spec.Containers) == 0 {
		return admission.Errored(400, fmt.Errorf("pod has no containers to mount the model cache volume"))
	}
	pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: modelMountPath,
		ReadOnly:  true,
	})

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(500, fmt.Errorf("unable to marshal mutated pod: %w", err))
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}
