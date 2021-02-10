// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors.
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lsv1alpha1 "github.com/gardener/landscaper/apis/core/v1alpha1"
)

// ManagedInstanceLabel describes label that is added to every manifest deployer managed resource
// to define its corresponding instance.
const ManagedInstanceLabel = "manifest.deployer.landscaper.gardener.cloud/instance"

// ManagedDeployItemLabel describes label that is added to every manifest deployer managed resource
// to define its source deploy item.
const ManagedDeployItemLabel = "manifest.deployer.landscaper.gardener.cloud/deployitem"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ProviderConfiguration is the manifest deployer configuration that configures the controller
type Configuration struct {
	metav1.TypeMeta
	// HealthCheckTimeOutSeconds is the time to wait in seconds before giving up on a resource to be ready.
	// Defaults to 60s.
	// +optional
	HealthCheckTimeOutSeconds int32 `json:"healthCheckTimeOutSeconds,omitempty"`
	// DeleteTimeOutSeconds is the time to wait in seconds before giving up on a resource to be deleted.
	// Defaults to 60s.
	// +optional
	DeleteTimeOutSeconds int32 `json:"deleteTimeOutSeconds,omitempty"`
	// TargetSelector describes all selectors the deployer should depend on.
	TargetSelector []lsv1alpha1.TargetSelector `json:"targetSelector,omitempty"`
}
