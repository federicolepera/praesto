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
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var modelcachelog = logf.Log.WithName("modelcache-resource")

// SetupModelCacheWebhookWithManager registers the webhook for ModelCache in the manager.
func SetupModelCacheWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&praestov1alpha1.ModelCache{}).
		WithValidator(&ModelCacheCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-praesto-praesto-io-v1alpha1-modelcache,mutating=false,failurePolicy=fail,sideEffects=None,groups=praesto.praesto.io,resources=modelcaches,verbs=create;update,versions=v1alpha1,name=vmodelcache-v1alpha1.kb.io,admissionReviewVersions=v1

// ModelCacheCustomValidator struct is responsible for validating the ModelCache resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ModelCacheCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &ModelCacheCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type ModelCache.
func (v *ModelCacheCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	modelcache, ok := obj.(*praestov1alpha1.ModelCache)
	if !ok {
		return nil, fmt.Errorf("expected a ModelCache object but got %T", obj)
	}
	modelcachelog.Info("Validation for ModelCache upon creation", "name", modelcache.GetName())

	return nil, validateModelCache(modelcache)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ModelCache.
func (v *ModelCacheCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	modelcache, ok := newObj.(*praestov1alpha1.ModelCache)
	if !ok {
		return nil, fmt.Errorf("expected a ModelCache object for the newObj but got %T", newObj)
	}
	modelcachelog.Info("Validation for ModelCache upon update", "name", modelcache.GetName())

	return nil, validateModelCache(modelcache)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type ModelCache.
func (v *ModelCacheCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	modelcache, ok := obj.(*praestov1alpha1.ModelCache)
	if !ok {
		return nil, fmt.Errorf("expected a ModelCache object but got %T", obj)
	}
	modelcachelog.Info("Validation for ModelCache upon deletion", "name", modelcache.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

func validateModelCache(modelCache *praestov1alpha1.ModelCache) error {
	var allErrs field.ErrorList

	storagePath := field.NewPath("spec", "storage")
	allErrs = append(allErrs, validateStorage(modelCache, storagePath)...)

	sourcePath := field.NewPath("spec", "source")
	allErrs = append(allErrs, validateSource(modelCache, sourcePath)...)

	downloaderPath := field.NewPath("spec", "downloader")
	allErrs = append(allErrs, validateDownloader(modelCache, downloaderPath)...)

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		praestov1alpha1.GroupVersion.WithKind("ModelCache").GroupKind(),
		modelCache.Name,
		allErrs,
	)
}

func validateDownloader(modelCache *praestov1alpha1.ModelCache, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	resourcesPath := path.Child("resources")
	allErrs = append(allErrs, validateResourceList(modelCache.Spec.Downloader.Resources.Requests, resourcesPath.Child("requests"))...)
	allErrs = append(allErrs, validateResourceList(modelCache.Spec.Downloader.Resources.Limits, resourcesPath.Child("limits"))...)

	return allErrs
}

func validateResourceList(resources praestov1alpha1.ResourceList, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	allErrs = append(allErrs, validateOptionalQuantity(resources.CPU, path.Child("cpu"))...)
	allErrs = append(allErrs, validateOptionalQuantity(resources.Memory, path.Child("memory"))...)

	return allErrs
}

func validateOptionalQuantity(value string, path *field.Path) field.ErrorList {
	if value == "" {
		return nil
	}

	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		return field.ErrorList{field.Invalid(path, value, "must be a valid Kubernetes quantity")}
	}
	if quantity.Sign() <= 0 {
		return field.ErrorList{field.Invalid(path, value, "must be greater than zero")}
	}

	return nil
}

func validateStorage(modelCache *praestov1alpha1.ModelCache, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	storageClassNamePath := path.Child("storageClassName")
	if modelCache.Spec.Storage.StorageClassName == "" {
		allErrs = append(allErrs, field.Required(storageClassNamePath, "storageClassName is required"))
	}

	sizePath := path.Child("size")
	if modelCache.Spec.Storage.Size == "" {
		allErrs = append(allErrs, field.Required(sizePath, "storage size is required"))
		return allErrs
	}

	storageSize, err := resource.ParseQuantity(modelCache.Spec.Storage.Size)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(sizePath, modelCache.Spec.Storage.Size, "must be a valid Kubernetes quantity"))
		return allErrs
	}

	if storageSize.Sign() <= 0 {
		allErrs = append(allErrs, field.Invalid(sizePath, modelCache.Spec.Storage.Size, "must be greater than zero"))
	}

	return allErrs
}

func validateSource(modelCache *praestov1alpha1.ModelCache, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	huggingfacePath := path.Child("huggingface")
	if modelCache.Spec.Source.Huggingface.Repo == "" {
		allErrs = append(allErrs, field.Required(huggingfacePath.Child("repo"), "huggingface repo is required"))
	}

	secretRef := modelCache.Spec.Source.Huggingface.Token.SecretRef
	secretRefPath := huggingfacePath.Child("token", "secretRef")
	if secretRef.Name != "" && secretRef.Key == "" {
		allErrs = append(allErrs, field.Required(secretRefPath.Child("key"), "secret key is required when secret name is set"))
	}
	if secretRef.Key != "" && secretRef.Name == "" {
		allErrs = append(allErrs, field.Required(secretRefPath.Child("name"), "secret name is required when secret key is set"))
	}

	return allErrs
}
