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

// ModelCacheNodeSpec defines the desired state of ModelCacheNode
type ModelCacheNodeSpec struct {
	ModelCacheRef ModelCacheNodeModelCacheRef `json:"modelCacheRef"`
	NodeName      string                      `json:"nodeName"`
	StorageClass  string                      `json:"storageClassName"`
}

type ModelCacheNodeModelCacheRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid,omitempty"`
}

const (
	ModelCacheNodePhaseReady       = "Ready"
	ModelCacheNodePhaseDownloading = "Downloading"
	ModelCacheNodePhaseFailed      = "Failed"
	ModelCacheNodePhasePending     = "Pending"
	ModelCacheNodePhaseEvicted     = "Evicted"
)

// ModelCacheNodeStatus defines the observed state of ModelCacheNode.
type ModelCacheNodeStatus struct {
	// +kubebuilder:validation:Enum=Ready;Downloading;Failed;Pending;Evicted
	Phase string `json:"phase,omitempty"`

	LocalPath string `json:"localPath,omitempty"`

	PvcName string `json:"pvcName,omitempty"`

	PvName string `json:"pvName,omitempty"`

	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status

// ModelCacheNode is the Schema for the modelcachenodes API
type ModelCacheNode struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ModelCacheNode
	// +required
	Spec ModelCacheNodeSpec `json:"spec"`

	// status defines the observed state of ModelCacheNode
	// +optional
	Status ModelCacheNodeStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ModelCacheNodeList contains a list of ModelCacheNode
type ModelCacheNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelCacheNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelCacheNode{}, &ModelCacheNodeList{})
}
