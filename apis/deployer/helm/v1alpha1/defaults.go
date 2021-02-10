// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company and Gardener contributors.
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

// SetDefaults_Configuration sets the defaults for the helm deployer configuration.
func SetDefaults_Configuration(obj *Configuration) {
	if obj.HealthCheckTimeOutSeconds == 0 {
		obj.HealthCheckTimeOutSeconds = 60
	}
	if obj.DeleteTimeOutSeconds == 0 {
		obj.DeleteTimeOutSeconds = 60
	}
}

// SetDefaults_ProviderConfiguration sets the defaults for the helm deployer provider configuration.
func SetDefaults_ProviderConfiguration(obj *ProviderConfiguration) {
	if len(obj.UpdateStrategy) == 0 {
		obj.UpdateStrategy = UpdateStrategyUpdate
	}
}
