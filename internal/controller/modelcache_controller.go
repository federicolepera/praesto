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

package controller

import (
	"context"
	"time"

	"github.com/federicolepera/praesto/internal/downloader"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
)

// ModelCacheReconciler reconciles a ModelCache object
type ModelCacheReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcaches/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ModelCache object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *ModelCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	logger.Info("Reconciling ModelCache", "name", req.Name, "namespace", req.Namespace)

	var modelCache praestov1alpha1.ModelCache
	if err := r.Get(ctx, req.NamespacedName, &modelCache); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch ModelCache")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	pvc, err := downloader.EnsureModelCachePVC(ctx, r.Client, r.Scheme, &modelCache)
	if err != nil {
		logger.Error(err, "unable to ensure PVC for ModelCache")
		return ctrl.Result{}, err
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if _, err := downloader.EnsureDownloadJob(ctx, r.Client, r.Scheme, &modelCache, pvc); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&praestov1alpha1.ModelCache{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		Named("modelcache").
		Complete(r)
}
