/*

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

package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProjectSpec defines the desired state of Project
type ProjectSpec struct {
	// The name of the Config to use for this Project
	ConfigName string `json:"configName"`
}

// ProjectStatus defines the observed state of Project
type ProjectStatus struct {
	Conditions []ProjectCondition `json:"conditions,omitempty"`
}

// ProjectCondition represents a condition of a Project
// ---
// +k8s:openapi-gen=true
type ProjectCondition struct {
	Type               ProjectConditionType   `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastProbeTime      metav1.Time            `json:"lastProbeTime,omitempty"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}

// ProjectConditionType is the type of a ProjectCondition
type ProjectConditionType string

const (
	// ProjectConditionConfigured defines whether the Project is assigned to a Config
	ProjectConditionConfigured ProjectConditionType = "Configured"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=projects,scope=Cluster
// +kubebuilder:subresource:status

// Project is the Schema for the projects API
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProjectSpec   `json:"spec,omitempty"`
	Status ProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProjectList contains a list of Project
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
