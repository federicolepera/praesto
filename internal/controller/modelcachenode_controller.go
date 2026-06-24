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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	"github.com/federicolepera/praesto/internal/downloader"
)

// ModelCacheNodeReconciler reconciles a ModelCacheNode object
type ModelCacheNodeReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	LocalCacheBasePath string
}

// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcachenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcachenodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcachenodes/finalizers,verbs=update
// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcaches,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ModelCacheNode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *ModelCacheNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	logger.Info("Reconciling ModelCacheNode", "name", req.Name)

	var modelCacheNode praestov1alpha1.ModelCacheNode
	if err := r.Get(ctx, req.NamespacedName, &modelCacheNode); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch ModelCacheNode")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !modelCacheNode.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	modelCache := &praestov1alpha1.ModelCache{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: modelCacheNode.Spec.ModelCacheRef.Namespace, Name: modelCacheNode.Spec.ModelCacheRef.Name}, modelCache); err != nil {
		logger.Error(err, "unable to fetch parent ModelCache")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	pvc, err := downloader.EnsureModelCacheNodePVC(ctx, r.Client, r.Scheme, &modelCacheNode)
	if err != nil {
		logger.Error(err, "unable to ensure PVC for ModelCacheNode")
		return ctrl.Result{}, err
	}

	pv, err := downloader.EnsureModelCacheNodePV(ctx, r.Client, r.Scheme, r.LocalCacheBasePath, &modelCacheNode, pvc)
	if err != nil {
		logger.Error(err, "unable to ensure PV for ModelCacheNode")
		return ctrl.Result{}, err
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhasePending
		modelCacheNode.Status.PvcName = pvc.Name
		modelCacheNode.Status.PvName = pv.Name
		modelCacheNode.Status.LocalPath = downloader.LocalPathForModelCacheNode(r.LocalCacheBasePath, &modelCacheNode)
		setModelCacheNodeCondition(&modelCacheNode, "PVCReady", metav1.ConditionFalse, "PVCPending", fmt.Sprintf("PVC %s/%s is not bound yet", pvc.Namespace, pvc.Name))
		if err := r.Status().Update(ctx, &modelCacheNode); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	job, err := downloader.EnsureDownloadJobModelCacheNode(ctx, r.Client, r.Scheme, modelCache, &modelCacheNode, pvc)
	if err != nil {
		logger.Error(err, "unable to ensure download Job for ModelCacheNode")
		return ctrl.Result{}, err
	}

	jobComplete, err := downloader.IsDownloadJobComplete(job)
	if err != nil {
		modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhaseFailed
		modelCacheNode.Status.PvcName = pvc.Name
		modelCacheNode.Status.PvName = pv.Name
		modelCacheNode.Status.DownloadJobName = job.Name
		modelCacheNode.Status.LocalPath = downloader.LocalPathForModelCacheNode(r.LocalCacheBasePath, &modelCacheNode)
		setModelCacheNodeCondition(&modelCacheNode, "PVCReady", metav1.ConditionTrue, "PVCBound", fmt.Sprintf("PVC %s/%s is bound", pvc.Namespace, pvc.Name))
		setModelCacheNodeCondition(&modelCacheNode, "DownloadComplete", metav1.ConditionFalse, "JobFailed", err.Error())
		setModelCacheNodeCondition(&modelCacheNode, "Ready", metav1.ConditionFalse, "DownloadFailed", "Model is not ready because download job failed")
		if updateErr := r.Status().Update(ctx, &modelCacheNode); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, err
	}

	modelCacheNode.Status.PvcName = pvc.Name
	modelCacheNode.Status.PvName = pv.Name
	modelCacheNode.Status.DownloadJobName = job.Name
	modelCacheNode.Status.LocalPath = downloader.LocalPathForModelCacheNode(r.LocalCacheBasePath, &modelCacheNode)
	setModelCacheNodeCondition(&modelCacheNode, "PVCReady", metav1.ConditionTrue, "PVCBound", fmt.Sprintf("PVC %s/%s is bound", pvc.Namespace, pvc.Name))
	if jobComplete {
		modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhaseReady
		setModelCacheNodeCondition(&modelCacheNode, "DownloadComplete", metav1.ConditionTrue, "JobSucceeded", "Download job completed successfully")
		setModelCacheNodeCondition(&modelCacheNode, "Ready", metav1.ConditionTrue, "ModelReady", "Model is ready on this node")
	} else {
		modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhaseDownloading
		setModelCacheNodeCondition(&modelCacheNode, "DownloadComplete", metav1.ConditionFalse, "JobRunning", "Download job is still running")
		setModelCacheNodeCondition(&modelCacheNode, "Ready", metav1.ConditionFalse, "Downloading", "Model is downloading on this node")
	}
	if err := r.Status().Update(ctx, &modelCacheNode); err != nil {
		return ctrl.Result{}, err
	}
	if !jobComplete {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func setModelCacheNodeCondition(modelCacheNode *praestov1alpha1.ModelCacheNode, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&modelCacheNode.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             conditionStatus,
		ObservedGeneration: modelCacheNode.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelCacheNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&praestov1alpha1.ModelCacheNode{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.PersistentVolume{}).
		Owns(&batchv1.Job{}).
		Named("modelcachenode").
		Complete(r)
}
