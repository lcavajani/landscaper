// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors.
//
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	apimacherrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	lsv1alpha1 "github.com/gardener/landscaper/apis/core/v1alpha1"
	lsv1alpha1helper "github.com/gardener/landscaper/apis/core/v1alpha1/helper"
	"github.com/gardener/landscaper/apis/deployer/manifest"
	manifestv1alpha2 "github.com/gardener/landscaper/apis/deployer/manifest/v1alpha2"
	kutil "github.com/gardener/landscaper/pkg/utils/kubernetes"
)

// Reconcile handles the reconcile flow for a manifest deploy item.
func (m *Manifest) Reconcile(ctx context.Context) error {
	currOp := "ReconcileManifests"
	m.DeployItem.Status.Phase = lsv1alpha1.ExecutionPhaseProgressing

	_, targetClient, err := m.TargetClient()
	if err != nil {
		m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
			currOp, "TargetClusterClient", err.Error())
		return err
	}

	var (
		objects         = make([]*unstructured.Unstructured, len(m.ProviderConfiguration.Manifests))
		manifestDecoder = serializer.NewCodecFactory(ManifestScheme).UniversalDecoder()
		status          = &manifest.ProviderStatus{
			TypeMeta: metav1.TypeMeta{
				APIVersion: manifest.SchemeGroupVersion.String(),
				Kind:       "ProviderStatus",
			},
			ManagedResources: make([]manifest.ManagedResourceStatus, len(objects)),
		}
	)

	for i, manifestData := range m.ProviderConfiguration.Manifests {
		obj := &unstructured.Unstructured{}
		if _, _, err := manifestDecoder.Decode(manifestData.Manifest.Raw, nil, obj); err != nil {
			m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
				currOp, "DecodeManifest", fmt.Sprintf("error while decoding manifest at index %d: %s", i, err.Error()))
			return err
		}

		status.ManagedResources[i] = manifest.ManagedResourceStatus{
			Policy: manifestData.Policy,
			Resource: lsv1alpha1.TypedObjectReference{
				APIVersion: obj.GetAPIVersion(),
				Kind:       obj.GetKind(),
				ObjectReference: lsv1alpha1.ObjectReference{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				},
			},
		}

		if manifestData.Policy == manifest.IgnorePolicy {
			continue
		}
		objects[i] = obj
		if err := m.ApplyObject(ctx, targetClient, manifestData.Policy, obj); err != nil {
			return err
		}
	}

	if m.ProviderStatus != nil {
		if err := m.cleanupOrphanedResources(ctx, targetClient, m.ProviderStatus.ManagedResources, objects); err != nil {
			m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
				currOp, "CleanupOrphanedResources", err.Error())
			return fmt.Errorf("unable to cleanup orphaned resources: %w", err)
		}
	}

	statusData, err := encodeStatus(status)
	if err != nil {
		m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
			currOp, "ProviderStatus", err.Error())
		return err
	}

	m.DeployItem.Status.ProviderStatus = statusData
	if err := m.kubeClient.Status().Update(ctx, m.DeployItem); err != nil {
		m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
			currOp, "UpdateStatus", err.Error())
		return err
	}

	return nil
}

// CheckResourcesHealth checks if the managed resources are Ready/Healthy.
func (m *Manifest) CheckResourcesHealth(ctx context.Context) error {
	var (
		currOp          = "CheckResourcesHealthManifests"
		manifestDecoder = serializer.NewCodecFactory(ManifestScheme).UniversalDecoder()
		status          = &manifest.ProviderStatus{}
	)

	if _, _, err := manifestDecoder.Decode(m.DeployItem.Status.ProviderStatus.Raw, nil, status); err != nil {
		return err
	}

	if len(status.ManagedResources) == 0 {
		return nil
	}

	objects := make([]*unstructured.Unstructured, len(status.ManagedResources))
	for i, mr := range status.ManagedResources {
		// do not check ignored resources.
		if mr.Policy == manifest.IgnorePolicy {
			continue
		}
		ref := mr.Resource
		obj := kutil.ObjectFromTypedObjectReference(&ref)
		objects[i] = obj
	}

	backoff := wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   0,
		Steps:    3,
	}

	if err := kutil.WaitObjectsReady(ctx, backoff, m.log, m.kubeClient, objects); err != nil {
		m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
			currOp, "CheckResourcesReadiness", err.Error())
		return err
	}

	m.DeployItem.Status.Phase = lsv1alpha1.ExecutionPhaseSucceeded
	m.DeployItem.Status.ObservedGeneration = m.DeployItem.Generation
	m.DeployItem.Status.LastError = nil

	return m.kubeClient.Status().Update(ctx, m.DeployItem)
}

// Delete deletes the managed resources from the cluster.
func (m *Manifest) Delete(ctx context.Context) error {
	currOp := "DeleteManifests"
	m.DeployItem.Status.Phase = lsv1alpha1.ExecutionPhaseDeleting

	if m.ProviderStatus == nil || len(m.ProviderStatus.ManagedResources) == 0 {
		controllerutil.RemoveFinalizer(m.DeployItem, lsv1alpha1.LandscaperFinalizer)
		return m.kubeClient.Update(ctx, m.DeployItem)
	}

	_, kubeClient, err := m.TargetClient()
	if err != nil {
		m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
			currOp, "TargetClusterClient", err.Error())
		return err
	}

	completed := true
	for _, mr := range m.ProviderStatus.ManagedResources {
		if mr.Policy == manifest.IgnorePolicy || mr.Policy == manifest.KeepPolicy {
			continue
		}
		ref := mr.Resource
		obj := kutil.ObjectFromTypedObjectReference(&ref)
		if err := kubeClient.Delete(ctx, obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
				currOp, "DeleteManifest", err.Error())
			return err
		}
		completed = false
	}

	if !completed {
		m.DeployItem.Status.LastError = nil
		return errors.New("not all items are deleted")
	}

	// remove finalizer
	controllerutil.RemoveFinalizer(m.DeployItem, lsv1alpha1.LandscaperFinalizer)
	return m.kubeClient.Update(ctx, m.DeployItem)
}

// ApplyObject applies a managed resource to the target cluster.
func (m *Manifest) ApplyObject(ctx context.Context, kubeClient client.Client, policy manifest.ManifestPolicy, obj *unstructured.Unstructured) error {
	currOp := "ApplyObjects"
	currObj := unstructured.Unstructured{} // can't use obj.NewEmptyInstance() as this returns a runtime.Unstructured object which doesn't implement client.Object
	currObj.GetObjectKind().SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
	key := kutil.ObjectKey(obj.GetName(), obj.GetNamespace())
	if err := kubeClient.Get(ctx, key, &currObj); err != nil {
		if !apierrors.IsNotFound(err) {
			m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
				currOp, "GetObject", err.Error())
			return err
		}
		// inject manifest specific labels
		kutil.SetMetaDataLabel(obj, manifestv1alpha2.ManagedDeployItemLabel, m.DeployItem.Name)
		if err := kubeClient.Create(ctx, obj); err != nil {
			err = fmt.Errorf("unable to create resource %s: %w", key.String(), err)
			m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
				currOp, "CreateObject", err.Error())
			return err
		}
		return nil
	}

	// if fallback policy is set and the resource is already managed by another deployer
	// we are not allowed to manage that resource
	if policy == manifest.FallbackPolicy && !kutil.HasLabelWithValue(obj, manifestv1alpha2.ManagedDeployItemLabel, m.DeployItem.Name) {
		m.log.Info("resource is already managed", "resource", key.String())
		return nil
	}
	// inject manifest specific labels
	kutil.SetMetaDataLabel(obj, manifestv1alpha2.ManagedDeployItemLabel, m.DeployItem.Name)

	if err := kutil.SetRequiredNestedFieldsFromObj(&currObj, obj); err != nil {
		return err
	}

	switch m.ProviderConfiguration.UpdateStrategy {
	case manifest.UpdateStrategyUpdate:
		if err := kubeClient.Update(ctx, obj); err != nil {
			err = fmt.Errorf("unable to update resource %s: %w", key.String(), err)
			m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
				currOp, "ApplyObject", err.Error())
			return err
		}
	case manifest.UpdateStrategyPatch:
		if err := kubeClient.Patch(ctx, &currObj, client.MergeFrom(obj)); err != nil {
			err = fmt.Errorf("unable to patch resource %s: %w", key.String(), err)
			m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
				currOp, "ApplyObject", err.Error())
			return err
		}
	default:
		err := fmt.Errorf("%s is not a valid update strategy", m.ProviderConfiguration.UpdateStrategy)
		m.DeployItem.Status.LastError = lsv1alpha1helper.UpdatedError(m.DeployItem.Status.LastError,
			currOp, "ApplyObject", err.Error())
		return err
	}
	return nil
}

// cleanupOrphanedResources removes all managed resources that are not rendered anymore.
func (m *Manifest) cleanupOrphanedResources(ctx context.Context, kubeClient client.Client, oldObjects []manifest.ManagedResourceStatus, currentObjects []*unstructured.Unstructured) error {
	var (
		allErrs []error
		wg      sync.WaitGroup
	)
	for _, mr := range oldObjects {
		if mr.Policy == manifest.IgnorePolicy || mr.Policy == manifest.KeepPolicy {
			continue
		}
		ref := mr.Resource
		obj := kutil.ObjectFromTypedObjectReference(&ref)
		if err := kubeClient.Get(ctx, kutil.ObjectKey(ref.Name, ref.Namespace), obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("unable to get object %s %s: %w", obj.GroupVersionKind().String(), obj.GetName(), err)
		}

		if !containsUnstructuredObject(obj, currentObjects) {
			wg.Add(1)
			go func(obj *unstructured.Unstructured) {
				defer wg.Done()
				if err := kubeClient.Delete(ctx, obj); err != nil {
					allErrs = append(allErrs, fmt.Errorf("unable to delete %s %s/%s: %w", obj.GroupVersionKind().String(), obj.GetName(), obj.GetNamespace(), err))
				}

				pollCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
				defer cancel()
				delCondFunc := kutil.GenerateDeleteObjectConditionFunc(ctx, kubeClient, obj)
				err := wait.PollImmediateUntil(5*time.Second, delCondFunc, pollCtx.Done())
				if err != nil {
					allErrs = append(allErrs, err)
				}
			}(obj)
		}
	}
	wg.Wait()

	if len(allErrs) == 0 {
		return nil
	}
	return apimacherrors.NewAggregate(allErrs)
}

func containsUnstructuredObject(obj *unstructured.Unstructured, objects []*unstructured.Unstructured) bool {
	for _, found := range objects {
		if len(obj.GetUID()) != 0 && len(found.GetUID()) != 0 {
			if obj.GetUID() == found.GetUID() {
				return true
			}
			continue
		}
		// todo: check for conversions .e.g. networking.k8s.io -> apps.k8s.io
		if found.GetObjectKind().GroupVersionKind().GroupKind() != obj.GetObjectKind().GroupVersionKind().GroupKind() {
			continue
		}
		if found.GetName() == obj.GetName() && found.GetNamespace() == obj.GetNamespace() {
			return true
		}
	}
	return false
}

func encodeStatus(status *manifest.ProviderStatus) (*runtime.RawExtension, error) {
	status.TypeMeta = metav1.TypeMeta{
		APIVersion: manifest.SchemeGroupVersion.String(),
		Kind:       "ProviderStatus",
	}

	raw := &runtime.RawExtension{}
	obj := status.DeepCopyObject()
	if err := runtime.Convert_runtime_Object_To_runtime_RawExtension(&obj, raw, nil); err != nil {
		return nil, err
	}
	return raw, nil
}
