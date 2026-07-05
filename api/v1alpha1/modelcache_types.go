/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
type SecretRef struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key,omitempty"`
}
type Token struct {
	// +kubebuilder:validation:Required
	SecretRef SecretRef `json:"secretRef"`
}
type HuggingfaceSource struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Repo     string `json:"repo"`
	Revision string `json:"revision,omitempty"`
	Token    *Token `json:"token,omitempty"`
}
type Source struct {
	// +kubebuilder:validation:Required
	Huggingface HuggingfaceSource `json:"huggingface"`
}

type Storage struct {
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Size string `json:"size"`
}
type ResourceList struct {
	// +optional
	CPU string `json:"cpu,omitempty"`
	// +optional
	Memory string `json:"memory,omitempty"`
}
type ResourceRequirements struct {
	// +optional
	Requests ResourceList `json:"requests,omitempty"`
	// +optional
	Limits ResourceList `json:"limits,omitempty"`
}

type ContainerSecurityContext struct {
	// +optional
	Capabilities *corev1.Capabilities `json:"capabilities,omitempty"`
	// +optional
	Privileged *bool `json:"privileged,omitempty"`
	// +optional
	SELinuxOptions *corev1.SELinuxOptions `json:"seLinuxOptions,omitempty"`
	// +optional
	RunAsUser *int64 `json:"runAsUser,omitempty"`
	// +optional
	RunAsGroup *int64 `json:"runAsGroup,omitempty"`
	// +optional
	RunAsNonRoot *bool `json:"runAsNonRoot,omitempty"`
	// +optional
	ReadOnlyRootFilesystem *bool `json:"readOnlyRootFilesystem,omitempty"`
	// +optional
	AllowPrivilegeEscalation *bool `json:"allowPrivilegeEscalation,omitempty"`
	// +optional
	// +kubebuilder:validation:Enum=Default;Unmasked
	ProcMount *corev1.ProcMountType `json:"procMount,omitempty"`
	// +optional
	SeccompProfile *corev1.SeccompProfile `json:"seccompProfile,omitempty"`
}

type Downloader struct {
	// +optional
	Image string `json:"image,omitempty"`
	// +optional
	Resources ResourceRequirements `json:"resources,omitempty"`
	// +optional
	ContainerSecurityContext *ContainerSecurityContext `json:"containerSecurityContext,omitempty"`
}

type Eviction struct {
	UnusedTTL string `json:"unusedTTL,omitempty"`
}

// ModelCacheSpec defines the desired state of ModelCache
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ModelCache spec is immutable after creation"
type ModelCacheSpec struct {
	Eviction Eviction `json:"eviction,omitempty"`
	// +kubebuilder:validation:Required
	Source Source `json:"source"`

	// +kubebuilder:validation:Required
	Storage Storage `json:"storage"`

	// +optional
	Downloader Downloader `json:"downloader,omitempty"`

	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

const (
	ModelCachePhaseReady       = "Ready"
	ModelCachePhaseDownloading = "Downloading"
	ModelCachePhaseFailed      = "Failed"
	ModelCachePhasePending     = "Pending"
	ModelCachePhaseEvicted     = "Evicted"
)

const (
	ModelCacheModePVC  = "PVC"
	ModelCacheModeNode = "Node"
)

// ModelCacheStatus defines the observed state of ModelCache.
type ModelCacheStatus struct {
	// +kubebuilder:validation:Enum=Ready;Downloading;Failed;Pending;Evicted
	Phase string `json:"phase,omitempty"`
	// +kubebuilder:validation:Enum=PVC;Node
	Mode string `json:"mode,omitempty"`

	PvcName         string      `json:"pvcName,omitempty"`
	DownloadJobName string      `json:"downloadJobName,omitempty"`
	LastUsedTime    metav1.Time `json:"lastUsedTime,omitempty"`

	TotalNodes       int32 `json:"totalNodes,omitempty"`
	ReadyNodes       int32 `json:"readyNodes,omitempty"`
	DownloadingNodes int32 `json:"downloadingNodes,omitempty"`
	FailedNodes      int32 `json:"failedNodes,omitempty"`
	PendingNodes     int32 `json:"pendingNodes,omitempty"`

	// Conditions represent the latest available observations of the ModelCache state.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.status.mode`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyNodes`
// +kubebuilder:printcolumn:name="Total",type=integer,JSONPath=`.status.totalNodes`
// +kubebuilder:printcolumn:name="PVC",type=string,JSONPath=`.status.pvcName`
// +kubebuilder:printcolumn:name="Download Job",type=string,JSONPath=`.status.downloadJobName`

// ModelCache is the Schema for the modelcaches API
type ModelCache struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ModelCache
	// +required
	Spec ModelCacheSpec `json:"spec"`

	// status defines the observed state of ModelCache
	// +optional
	Status ModelCacheStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ModelCacheList contains a list of ModelCache
type ModelCacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelCache `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelCache{}, &ModelCacheList{})
}
