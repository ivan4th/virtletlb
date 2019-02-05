/*
Copyright 2019 Mirantis.

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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InnerServicePort defines an inner service port
type InnerServicePort struct {
	// The name of this port within the service. This must be a DNS_LABEL.
	// All ports within a ServiceSpec must have unique names. This maps to
	// the 'Name' field in EndpointPort objects.
	// Optional if only one ServicePort is defined on this service.
	// +optional
	Name string `json:"name,omitempty"`

	// The IP protocol for this port. Supports "TCP", "UDP", and "SCTP".
	// Default is TCP.
	// +optional
	Protocol v1.Protocol `json:"protocol,omitempty"`

	// The port that will be exposed by this service.
	// This corresponds to the NodePort value in the inner cluster's service
	// and Port value in the outer service
	Port int32 `json:"port"`

	// The port on the inner cluster node used by the service.
	NodePort int32 `json:"nodePort,omitempty"`
}

// InnerServiceSpec defines the desired state of an InnerService
type InnerServiceSpec struct {
	NodeNames []string           `json:"nodeNames,omitempty"`
	Ports     []InnerServicePort `json:"ports,omitempty"`
}

// InnerServiceStatus defines the observed state of InnerService
type InnerServiceStatus struct {
	LoadBalancerIP string
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// InnerService is the Schema for the innerservices API
// +k8s:openapi-gen=true
type InnerService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InnerServiceSpec   `json:"spec,omitempty"`
	Status InnerServiceStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// InnerServiceList contains a list of InnerService
type InnerServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InnerService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InnerService{}, &InnerServiceList{})
}
