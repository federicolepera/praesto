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
	"fmt"
	"time"

	"github.com/federicolepera/praesto/internal/downloader"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	"github.com/federicolepera/praesto/internal/status"
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

	if modelCache.Status.Phase == praestov1alpha1.ModelCachePhaseReady {
		return ctrl.Result{}, nil
	}

	pvc, err := downloader.EnsureModelCachePVC(ctx, r.Client, r.Scheme, &modelCache)
	if err != nil {
		logger.Error(err, "unable to ensure PVC for ModelCache")
		return ctrl.Result{}, err
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhasePending
		modelCache.Status.PvcName = pvc.Name
		status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionFalse, "PVCPending", fmt.Sprintf("PVC %s is not bound yet", pvc.Name))
		status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "DownloadPending", "Download job has not started yet")
		status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "WaitingForPVC", "Model is waiting for PVC to be bound")
		if err := status.Update(ctx, r.Client, &modelCache); err != nil {
			logger.Error(err, "unable to update ModelCache status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	job, err := downloader.EnsureDownloadJob(ctx, r.Client, r.Scheme, &modelCache, pvc)
	if err != nil {
		logger.Error(err, "unable to ensure download Job for ModelCache")
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
		modelCache.Status.PvcName = pvc.Name
		status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionTrue, "PVCBound", fmt.Sprintf("PVC %s is bound", pvc.Name))
		status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "JobCreateFailed", err.Error())
		status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "DownloadFailed", "Model is not ready because download job could not be created")
		if job != nil {
			modelCache.Status.DownloadJobName = job.Name
		}
		if err := status.Update(ctx, r.Client, &modelCache); err != nil {
			logger.Error(err, "unable to update ModelCache status")
		}
		return ctrl.Result{}, err
	}

	jobStatus, err := downloader.IsDownloadJobComplete(job)
	if err != nil {
		logger.Error(err, "error checking download Job status for ModelCache")
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
		modelCache.Status.PvcName = pvc.Name
		modelCache.Status.DownloadJobName = job.Name
		status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionTrue, "PVCBound", fmt.Sprintf("PVC %s is bound", pvc.Name))
		status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "JobFailed", err.Error())
		status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "DownloadFailed", "Model is not ready because download job failed")
		if updateErr := status.Update(ctx, r.Client, &modelCache); updateErr != nil {
			logger.Error(updateErr, "unable to update ModelCache status")
		}
		return ctrl.Result{}, err
	}

	if jobStatus {
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseReady
		modelCache.Status.PvcName = pvc.Name
		modelCache.Status.DownloadJobName = job.Name
		status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionTrue, "PVCBound", fmt.Sprintf("PVC %s is bound", pvc.Name))
		status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionTrue, "JobSucceeded", "Download job completed successfully")
		status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionTrue, "ModelReady", "Model is ready to be mounted")
		if err := status.Update(ctx, r.Client, &modelCache); err != nil {
			logger.Error(err, "unable to update ModelCache status")
			return ctrl.Result{}, err
		}
	} else {
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseDownloading
		modelCache.Status.PvcName = pvc.Name
		modelCache.Status.DownloadJobName = job.Name
		status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionTrue, "PVCBound", fmt.Sprintf("PVC %s is bound", pvc.Name))
		status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "JobRunning", "Download job is still running")
		status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "Downloading", "Model is downloading")
		if err := status.Update(ctx, r.Client, &modelCache); err != nil {
			logger.Error(err, "unable to update ModelCache status")
			return ctrl.Result{}, err
		}
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
