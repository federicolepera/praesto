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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
type SecretRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}
type Token struct {
	SecretRef SecretRef `json:"secretRef,omitempty"`
}
type HuggingfaceSource struct {
	Repo     string `json:"repo"`
	Revision string `json:"revision,omitempty"`
	Token    Token  `json:"token,omitempty"`
}
type Source struct {
	Huggingface HuggingfaceSource `json:"huggingface,omitempty"`
}

type Storage struct {
	StorageClassName string `json:"storageClassName,omitempty"`
	Size             string `json:"size,omitempty"`
}

// ModelCacheSpec defines the desired state of ModelCache
type ModelCacheSpec struct {
	Source Source `json:"source"`

	Storage Storage `json:"storage"`
}

const (
	ModelCachePhaseReady       = "Ready"
	ModelCachePhaseDownloading = "Downloading"
	ModelCachePhaseFailed      = "Failed"
	ModelCachePhasePending     = "Pending"
)

// ModelCacheStatus defines the observed state of ModelCache.
type ModelCacheStatus struct {
	Phase           string `json:"phase,omitempty"`
	PvcName         string `json:"pvcName,omitempty"`
	DownloadJobName string `json:"downloadJobName,omitempty"`

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
