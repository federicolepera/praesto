package downloader

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
)

const DefaultDownloaderImage = "ghcr.io/federicolepera/praesto/downloader:latest"

const (
	ManagedLabelKey   = "praesto.io/managed"
	ManagedLabelValue = "true"
	ModelLabelKey     = "praesto.io/model"
	JobTypeLabelKey   = "praesto.io/job-type"
)

func PVCNameForModelCache(name string) string { return fmt.Sprintf("praesto-%s", name) }

func PVNameForModelCacheNode(node string, name string) string {
	return fmt.Sprintf("praesto-%s-%s", node, name)
}

func LocalPathForModelCacheNode(modelCacheNode *praestov1alpha1.ModelCacheNode) string {
	return fmt.Sprintf("/var/praesto/%s/%s", modelCacheNode.Spec.ModelCacheRef.Namespace, modelCacheNode.Spec.ModelCacheRef.Name)
}

func JobNameForModelCache(name string) string { return fmt.Sprintf("praesto-download-%s", name) }

func JobNameForModelCacheNode(name string) string { return fmt.Sprintf("praesto-download-%s", name) }

func ModelCacheLabels(name string) map[string]string {
	return map[string]string{
		ModelLabelKey:   name,
		ManagedLabelKey: ManagedLabelValue,
	}
}

func DownloadJobLabels(name string) map[string]string {
	labels := ModelCacheLabels(name)
	labels[JobTypeLabelKey] = "download"
	return labels
}

func EnsureModelCachePVC(ctx context.Context, k8sClient client.Client, scheme *runtime.Scheme, modelCache *praestov1alpha1.ModelCache) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: PVCNameForModelCache(modelCache.Name), Namespace: modelCache.Namespace}
	if err := k8sClient.Get(ctx, key, pvc); err == nil {
		if err := validateManagedPVC(pvc, modelCache); err != nil {
			return nil, err
		}
		return pvc, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	pvc, err := PersistentVolumeClaimForModelCache(scheme, modelCache)
	if err != nil {
		return nil, err
	}
	if err := k8sClient.Create(ctx, pvc); err != nil {
		if apierrors.IsAlreadyExists(err) {
			pvc := &corev1.PersistentVolumeClaim{}
			if getErr := k8sClient.Get(ctx, key, pvc); getErr != nil {
				return nil, getErr
			}
			if err := validateManagedPVC(pvc, modelCache); err != nil {
				return nil, err
			}
			return pvc, nil
		}
		return nil, err
	}

	return pvc, nil
}

func EnsureModelCacheNodePV(ctx context.Context, k8sClient client.Client, scheme *runtime.Scheme, modelCacheNode *praestov1alpha1.ModelCacheNode, pvc *corev1.PersistentVolumeClaim) (*corev1.PersistentVolume, error) {
	pv := &corev1.PersistentVolume{}
	name := PVNameForModelCacheNode(modelCacheNode.Spec.NodeName, modelCacheNode.Name)

	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, pv); err == nil {
		if err := validateModelCacheNodePV(pv, modelCacheNode); err != nil {
			return nil, err
		}
		return pv, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	pv, err := PersistentVolumeForModelCacheNode(scheme, modelCacheNode, pvc)
	if err != nil {
		return nil, err
	}
	if err := k8sClient.Create(ctx, pv); err != nil {
		if apierrors.IsAlreadyExists(err) {
			pv := &corev1.PersistentVolume{}
			if getErr := k8sClient.Get(ctx, types.NamespacedName{Name: name}, pv); getErr != nil {
				return nil, getErr
			}
			if err := validateModelCacheNodePV(pv, modelCacheNode); err != nil {
				return nil, err
			}
			return pv, nil
		}
		return nil, err
	}

	return pv, nil
}

func EnsureModelCacheNodePVC(ctx context.Context, k8sClient client.Client, scheme *runtime.Scheme, modelCacheNode *praestov1alpha1.ModelCacheNode) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: PVNameForModelCacheNode(modelCacheNode.Spec.NodeName, modelCacheNode.Name), Namespace: "praesto-system"}

	if err := k8sClient.Get(ctx, key, pvc); err == nil {
		if err := validateModelCacheNodePVC(pvc, modelCacheNode); err != nil {
			return nil, err
		}
		return pvc, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	pvc, err := PersistentVolumeClaimForModelCacheNode(scheme, modelCacheNode)
	if err != nil {
		return nil, err
	}
	if err := k8sClient.Create(ctx, pvc); err != nil {
		if apierrors.IsAlreadyExists(err) {
			pvc := &corev1.PersistentVolumeClaim{}
			if getErr := k8sClient.Get(ctx, key, pvc); getErr != nil {
				return nil, getErr
			}
			if err := validateModelCacheNodePVC(pvc, modelCacheNode); err != nil {
				return nil, err
			}
			return pvc, nil
		}
		return nil, err
	}

	return pvc, nil
}

func PersistentVolumeClaimForModelCacheNode(scheme *runtime.Scheme, modelCacheNode *praestov1alpha1.ModelCacheNode) (*corev1.PersistentVolumeClaim, error) {
	storageSize, err := resource.ParseQuantity(modelCacheNode.Spec.Storage.Size)
	if err != nil {
		return nil, fmt.Errorf("invalid storage size %q: %w", modelCacheNode.Spec.Storage.Size, err)
	}

	storageClassName := modelCacheNode.Spec.Storage.StorageClassName

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PVNameForModelCacheNode(modelCacheNode.Spec.NodeName, modelCacheNode.Name),
			Namespace: "praesto-system",
			Labels:    ModelCacheLabels(modelCacheNode.Name),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClassName,
			VolumeName:       PVNameForModelCacheNode(modelCacheNode.Spec.NodeName, modelCacheNode.Name),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(modelCacheNode, pvc, scheme); err != nil {
		return nil, err
	}

	return pvc, nil
}
func validateModelCacheNodePVC(pvc *corev1.PersistentVolumeClaim, modelCacheNode *praestov1alpha1.ModelCacheNode) error {
	labels := pvc.GetLabels()
	expectedPVCName := PVNameForModelCacheNode(modelCacheNode.Spec.NodeName, modelCacheNode.Name)
	if labels[ManagedLabelKey] != ManagedLabelValue || labels[ModelLabelKey] != modelCacheNode.Name || pvc.Name != expectedPVCName {
		return fmt.Errorf(
			"PVC %s/%s already exists but is not managed by praesto for ModelCacheNode %s/%s; expected name %s and labels %s=true and %s=%s",
			pvc.Namespace,
			pvc.Name,
			modelCacheNode.Namespace,
			modelCacheNode.Name,
			expectedPVCName,
			ManagedLabelKey,
			ModelLabelKey,
			modelCacheNode.Name,
		)
	}

	return nil
}

func GetManagedModelCachePVC(ctx context.Context, reader client.Reader, modelCache *praestov1alpha1.ModelCache) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: PVCNameForModelCache(modelCache.Name), Namespace: modelCache.Namespace}
	if err := reader.Get(ctx, key, pvc); err != nil {
		return nil, err
	}
	if err := validateManagedPVC(pvc, modelCache); err != nil {
		return nil, err
	}

	return pvc, nil
}

func validateModelCacheNodePV(pv *corev1.PersistentVolume, modelCacheNode *praestov1alpha1.ModelCacheNode) error {
	labels := pv.GetLabels()
	expectedPVName := PVNameForModelCacheNode(modelCacheNode.Spec.NodeName, modelCacheNode.Name)
	if labels[ManagedLabelKey] != ManagedLabelValue || labels[ModelLabelKey] != modelCacheNode.Name || pv.Name != expectedPVName {
		return fmt.Errorf(
			"PV %s already exists but is not managed by praesto for ModelCacheNode %s/%s; expected name %s and labels %s=true and %s=%s",
			pv.Name,
			modelCacheNode.Namespace,
			modelCacheNode.Name,
			expectedPVName,
			ManagedLabelKey,
			ModelLabelKey,
			modelCacheNode.Name,
		)
	}

	return nil
}

func validateManagedPVC(pvc *corev1.PersistentVolumeClaim, modelCache *praestov1alpha1.ModelCache) error {
	labels := pvc.GetLabels()
	if labels[ManagedLabelKey] != ManagedLabelValue || labels[ModelLabelKey] != modelCache.Name {
		return fmt.Errorf(
			"PVC %s/%s already exists but is not managed by praesto for ModelCache %s/%s; expected labels %s=true and %s=%s",
			pvc.Namespace,
			pvc.Name,
			modelCache.Namespace,
			modelCache.Name,
			ManagedLabelKey,
			ModelLabelKey,
			modelCache.Name,
		)
	}

	return nil
}

func PersistentVolumeForModelCacheNode(scheme *runtime.Scheme, modelCacheNode *praestov1alpha1.ModelCacheNode, pvc *corev1.PersistentVolumeClaim) (*corev1.PersistentVolume, error) {
	storageSize, err := resource.ParseQuantity(modelCacheNode.Spec.Storage.Size)
	if err != nil {
		return nil, fmt.Errorf("invalid storage size %q: %w", modelCacheNode.Spec.Storage.Size, err)
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:   PVNameForModelCacheNode(modelCacheNode.Spec.NodeName, modelCacheNode.Name),
			Labels: ModelCacheLabels(modelCacheNode.Name),
		},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{Local: &corev1.LocalVolumeSource{Path: LocalPathForModelCacheNode(modelCacheNode)}},
			ClaimRef: &corev1.ObjectReference{
				Kind:      "PersistentVolumeClaim",
				Name:      pvc.Name,
				Namespace: pvc.Namespace,
			},
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: storageSize},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              modelCacheNode.Spec.Storage.StorageClassName,
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      "kubernetes.io/hostname",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{modelCacheNode.Spec.NodeName},
						}},
					}},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(modelCacheNode, pv, scheme); err != nil {
		return nil, err
	}

	return pv, nil
}
func PersistentVolumeClaimForModelCache(scheme *runtime.Scheme, modelCache *praestov1alpha1.ModelCache) (*corev1.PersistentVolumeClaim, error) {
	storageSize, err := resource.ParseQuantity(modelCache.Spec.Storage.Size)
	if err != nil {
		return nil, fmt.Errorf("invalid storage size %q: %w", modelCache.Spec.Storage.Size, err)
	}

	var storageClassName *string
	if modelCache.Spec.Storage.StorageClassName != "" {
		storageClassName = &modelCache.Spec.Storage.StorageClassName
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PVCNameForModelCache(modelCache.Name),
			Namespace: modelCache.Namespace,
			Labels:    ModelCacheLabels(modelCache.Name),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: storageClassName,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(modelCache, pvc, scheme); err != nil {
		return nil, err
	}

	return pvc, nil
}

func EnsureDownloadJob(ctx context.Context, k8sClient client.Client, scheme *runtime.Scheme, modelCache *praestov1alpha1.ModelCache, pvc *corev1.PersistentVolumeClaim) (*batchv1.Job, error) {
	job := &batchv1.Job{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: JobNameForModelCache(modelCache.Name), Namespace: modelCache.Namespace}, job)
	if err == nil {
		return job, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	job, err = DownloadJobForModelCache(modelCache, pvc)
	if err != nil {
		return nil, err
	}
	if err := controllerutil.SetControllerReference(modelCache, job, scheme); err != nil {
		return nil, err
	}
	if err := k8sClient.Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) {
			job := &batchv1.Job{}
			if getErr := k8sClient.Get(ctx, types.NamespacedName{Name: JobNameForModelCache(modelCache.Name), Namespace: modelCache.Namespace}, job); getErr != nil {
				return nil, getErr
			}
			return job, nil
		}
		return nil, err
	}

	return job, nil
}

func EnsureDownloadJobModelCacheNode(ctx context.Context, k8sClient client.Client, scheme *runtime.Scheme, modelCache *praestov1alpha1.ModelCache, modelCacheNode *praestov1alpha1.ModelCacheNode, pvc *corev1.PersistentVolumeClaim) (*batchv1.Job, error) {
	job := &batchv1.Job{}
	key := types.NamespacedName{Name: JobNameForModelCacheNode(modelCacheNode.Name), Namespace: pvc.Namespace}
	if err := k8sClient.Get(ctx, key, job); err == nil {
		return job, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	job, err := DownloadJobForModelCacheNode(modelCache, modelCacheNode, pvc)
	if err != nil {
		return nil, err
	}
	if err := controllerutil.SetControllerReference(modelCacheNode, job, scheme); err != nil {
		return nil, err
	}
	if err := k8sClient.Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) {
			job := &batchv1.Job{}
			if getErr := k8sClient.Get(ctx, key, job); getErr != nil {
				return nil, getErr
			}
			return job, nil
		}
		return nil, err
	}

	return job, nil
}

func DownloadJobForModelCacheNode(modelCache *praestov1alpha1.ModelCache, modelCacheNode *praestov1alpha1.ModelCacheNode, pvc *corev1.PersistentVolumeClaim) (*batchv1.Job, error) {
	resources, err := resourceRequirementsForDownloader(modelCache.Spec.Downloader.Resources)
	if err != nil {
		return nil, err
	}

	image := modelCache.Spec.Downloader.Image
	if image == "" {
		image = DefaultDownloaderImage
	}
	securityContext := securityContextForDownloader(modelCache.Spec.Downloader.ContainerSecurityContext)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      JobNameForModelCacheNode(modelCacheNode.Name),
			Namespace: pvc.Namespace,
			Labels:    DownloadJobLabels(modelCacheNode.Name),
		},
		Spec: batchv1.JobSpec{
			ActiveDeadlineSeconds: ptrToInt64(7200),
			BackoffLimit:          ptrToInt32(3),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: ModelCacheLabels(modelCacheNode.Name)},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					NodeName:      modelCacheNode.Spec.NodeName,
					Containers: []corev1.Container{{
						Name:            "downloader",
						Image:           image,
						SecurityContext: securityContext,
						Env: []corev1.EnvVar{
							{Name: "HF_REPO", Value: modelCache.Spec.Source.Huggingface.Repo},
							{Name: "SOURCE_TYPE", Value: "huggingface"},
							{Name: "TARGET_PATH", Value: "/model"},
							{Name: "MODELCACHE_NAME", Value: modelCache.Name},
							{Name: "MODELCACHE_NAMESPACE", Value: modelCache.Namespace},
							{Name: "MODELCACHENODE_NAME", Value: modelCacheNode.Name},
							{Name: "NODE_NAME", Value: modelCacheNode.Spec.NodeName},
						},
						VolumeMounts:    []corev1.VolumeMount{{Name: "model-storage", MountPath: "/model", ReadOnly: false}},
						Resources:       resources,
						ImagePullPolicy: corev1.PullIfNotPresent,
					}},
					Volumes: []corev1.Volume{{
						Name: "model-storage",
						VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc.Name,
							ReadOnly:  false,
						}},
					}},
				},
			},
		},
	}

	container := &job.Spec.Template.Spec.Containers[0]
	if modelCache.Spec.Source.Huggingface.Revision != "" {
		container.Env = append(container.Env, corev1.EnvVar{Name: "HF_REVISION", Value: modelCache.Spec.Source.Huggingface.Revision})
	}
	if modelCache.Spec.Source.Huggingface.Token != nil {
		token := modelCache.Spec.Source.Huggingface.Token.SecretRef
		if token.Name != "" && token.Key != "" {
			container.Env = append(container.Env, corev1.EnvVar{
				Name: "HF_TOKEN",
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: token.Name},
					Key:                  token.Key,
				}},
			})
		}
	}

	return job, nil
}

func DownloadJobForModelCache(modelCache *praestov1alpha1.ModelCache, pvc *corev1.PersistentVolumeClaim) (*batchv1.Job, error) {
	resources, err := resourceRequirementsForDownloader(modelCache.Spec.Downloader.Resources)
	if err != nil {
		return nil, err
	}

	image := modelCache.Spec.Downloader.Image
	if image == "" {
		image = DefaultDownloaderImage
	}
	securityContext := securityContextForDownloader(modelCache.Spec.Downloader.ContainerSecurityContext)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      JobNameForModelCache(modelCache.Name),
			Namespace: modelCache.Namespace,
			Labels:    DownloadJobLabels(modelCache.Name),
		},
		Spec: batchv1.JobSpec{
			ActiveDeadlineSeconds: ptrToInt64(7200),
			BackoffLimit:          ptrToInt32(3),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: ModelCacheLabels(modelCache.Name)},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{{
						Name:            "downloader",
						Image:           image,
						SecurityContext: securityContext,
						Env: []corev1.EnvVar{
							{Name: "HF_REPO", Value: modelCache.Spec.Source.Huggingface.Repo},
							{Name: "SOURCE_TYPE", Value: "huggingface"},
							{Name: "TARGET_PATH", Value: "/model"},
							{Name: "MODELCACHE_NAME", Value: modelCache.Name},
							{Name: "MODELCACHE_NAMESPACE", Value: modelCache.Namespace},
						},
						VolumeMounts:    []corev1.VolumeMount{{Name: "model-storage", MountPath: "/model"}},
						Resources:       resources,
						ImagePullPolicy: corev1.PullIfNotPresent,
					}},
					Volumes: []corev1.Volume{{
						Name: "model-storage",
						VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc.Name,
						}},
					}},
				},
			},
		},
	}

	container := &job.Spec.Template.Spec.Containers[0]
	if modelCache.Spec.Source.Huggingface.Revision != "" {
		container.Env = append(container.Env, corev1.EnvVar{Name: "HF_REVISION", Value: modelCache.Spec.Source.Huggingface.Revision})
	}
	if modelCache.Spec.Source.Huggingface.Token != nil {
		token := modelCache.Spec.Source.Huggingface.Token.SecretRef
		if token.Name != "" && token.Key != "" {
			container.Env = append(container.Env, corev1.EnvVar{
				Name: "HF_TOKEN",
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: token.Name},
					Key:                  token.Key,
				}},
			})
		}
	}

	return job, nil
}

func securityContextForDownloader(securityContext *praestov1alpha1.ContainerSecurityContext) *corev1.SecurityContext {
	if securityContext == nil {
		return nil
	}

	return &corev1.SecurityContext{
		Capabilities:             securityContext.Capabilities,
		Privileged:               securityContext.Privileged,
		SELinuxOptions:           securityContext.SELinuxOptions,
		RunAsUser:                securityContext.RunAsUser,
		RunAsGroup:               securityContext.RunAsGroup,
		RunAsNonRoot:             securityContext.RunAsNonRoot,
		ReadOnlyRootFilesystem:   securityContext.ReadOnlyRootFilesystem,
		AllowPrivilegeEscalation: securityContext.AllowPrivilegeEscalation,
		ProcMount:                securityContext.ProcMount,
		SeccompProfile:           securityContext.SeccompProfile,
	}
}

func resourceRequirementsForDownloader(resources praestov1alpha1.ResourceRequirements) (corev1.ResourceRequirements, error) {
	requests, err := resourceListForDownloader(resources.Requests, "requests")
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}

	limits, err := resourceListForDownloader(resources.Limits, "limits")
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}

	return corev1.ResourceRequirements{
		Requests: requests,
		Limits:   limits,
	}, nil
}

func resourceListForDownloader(resources praestov1alpha1.ResourceList, fieldName string) (corev1.ResourceList, error) {
	resourceList := corev1.ResourceList{}

	if resources.CPU != "" {
		cpu, err := parsePositiveQuantity(resources.CPU, fieldName+".cpu")
		if err != nil {
			return nil, err
		}
		resourceList[corev1.ResourceCPU] = cpu
	}

	if resources.Memory != "" {
		memory, err := parsePositiveQuantity(resources.Memory, fieldName+".memory")
		if err != nil {
			return nil, err
		}
		resourceList[corev1.ResourceMemory] = memory
	}

	if len(resourceList) == 0 {
		return nil, nil
	}

	return resourceList, nil
}

func parsePositiveQuantity(value, fieldName string) (resource.Quantity, error) {
	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("invalid downloader resource %s %q: %w", fieldName, value, err)
	}
	if quantity.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("invalid downloader resource %s %q: must be greater than zero", fieldName, value)
	}

	return quantity, nil
}

func IsDownloadJobComplete(job *batchv1.Job) (bool, error) {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true, nil
		}
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return false, fmt.Errorf("download Job %s/%s failed: %s", job.Namespace, job.Name, condition.Message)
		}
	}

	return false, nil
}
func ptrToInt64(value int64) *int64 { return &value }

func ptrToInt32(value int32) *int32 { return &value }
