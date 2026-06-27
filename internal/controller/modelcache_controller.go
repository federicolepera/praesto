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
	"github.com/federicolepera/praesto/internal/kubeident"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	"github.com/federicolepera/praesto/internal/status"
)

const (
	modelCacheFinalizer               = "praesto.io/modelcache-finalizer"
	modelCacheNodeModelNamespaceLabel = "praesto.io/model-cache-namespace"
	modelCacheNodeModelNameLabel      = "praesto.io/model-cache-name"
	modelCacheNodeModelUIDLabel       = "praesto.io/model-cache-uid"
	modelCacheNodeNodeLabel           = "praesto.io/node"
)

// ModelCacheReconciler reconciles a ModelCache object
type ModelCacheReconciler struct {
	client.Client
	APIReader client.Reader
	Scheme    *runtime.Scheme
}

// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcaches/finalizers,verbs=update
// +kubebuilder:rbac:groups=praesto.praesto.io,resources=modelcachenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get
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
//
//nolint:gocyclo // Reconcile still contains both legacy PVC and local ModelCacheNode flows; split in a follow-up refactor.
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

	if !modelCache.DeletionTimestamp.IsZero() {
		return r.reconcileModelCacheDelete(ctx, &modelCache)
	}

	if !controllerutil.ContainsFinalizer(&modelCache, modelCacheFinalizer) {
		patch := client.MergeFrom(modelCache.DeepCopy())
		controllerutil.AddFinalizer(&modelCache, modelCacheFinalizer)
		if err := r.Patch(ctx, &modelCache, patch); err != nil {
			logger.Error(err, "unable to add ModelCache finalizer")
			return ctrl.Result{}, err
		}
	}

	if modelCache.Status.Phase == praestov1alpha1.ModelCachePhaseReady && !isLocalModelCache(&modelCache) {
		pvc, err := downloader.GetManagedModelCachePVC(ctx, r.readyPVCReader(), &modelCache)
		if err != nil {
			logger.Error(err, "unable to get PVC for ready ModelCache")
			modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
			modelCache.Status.PvcName = ""
			modelCache.Status.DownloadJobName = ""
			status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionFalse, "PVCLost", "PVC for ready ModelCache is missing")
			status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "DownloadJobUnknown", "Download job status is unknown because PVC is missing")
			status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "ModelNotReady", "Model is not ready because PVC is missing")
			if updateErr := status.Update(ctx, r.Client, &modelCache); updateErr != nil {
				logger.Error(updateErr, "unable to update ModelCache status after PVC loss")
			}
			return ctrl.Result{}, err
		}
		if !pvc.DeletionTimestamp.IsZero() {
			logger.Info("PVC for ready ModelCache is being deleted", "pvc", pvc.Name)
			modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
			modelCache.Status.PvcName = pvc.Name
			modelCache.Status.DownloadJobName = ""
			status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionFalse, "PVCDeleting", "PVC for ready ModelCache is being deleted")
			status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "DownloadJobUnknown", "Download job status is unknown because PVC is being deleted")
			status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "ModelNotReady", "Model is not ready because PVC is being deleted")
			if updateErr := status.Update(ctx, r.Client, &modelCache); updateErr != nil {
				logger.Error(updateErr, "unable to update ModelCache status after PVC deletion started")
			}
			return ctrl.Result{}, fmt.Errorf("PVC %s for ready ModelCache is being deleted", pvc.Name)
		}
		if pvc.Status.Phase != corev1.ClaimBound {
			logger.Info("PVC for ready ModelCache is not bound anymore", "pvc", pvc.Name)
			modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
			modelCache.Status.PvcName = pvc.Name
			modelCache.Status.DownloadJobName = ""
			status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionFalse, "PVCLost", "PVC for ready ModelCache is no longer bound")
			status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "DownloadJobUnknown", "Download job status is unknown because PVC is not bound")
			status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "ModelNotReady", "Model is not ready because PVC is not bound")
			if updateErr := status.Update(ctx, r.Client, &modelCache); updateErr != nil {
				logger.Error(updateErr, "unable to update ModelCache status after PVC unbound")
			}
			return ctrl.Result{}, fmt.Errorf("PVC %s for ready ModelCache is not bound anymore", pvc.Name)
		}
		logger.Info("ModelCache is already ready", "pvc", pvc.Name)
		return ctrl.Result{}, nil
	}
	storageClassExists, err := r.ensureStorageClassExists(ctx, &modelCache)
	if err != nil {
		logger.Error(err, "unable to validate StorageClass for ModelCache")
		return ctrl.Result{}, err
	}
	if !storageClassExists {
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
		modelCache.Status.PvcName = ""
		modelCache.Status.DownloadJobName = ""
		status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionFalse, "StorageClassNotFound", fmt.Sprintf("StorageClass %q does not exist", modelCache.Spec.Storage.StorageClassName))
		status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "DownloadPending", "Download job has not started because storage provisioning failed")
		status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "StorageUnavailable", "Model is not ready because its StorageClass does not exist")
		if err := status.Update(ctx, r.Client, &modelCache); err != nil {
			logger.Error(err, "unable to update ModelCache status after StorageClass validation failed")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if isLocalModelCache(&modelCache) {
		logger.Info("ModelCache is using local ModelCacheNode storage", "name", modelCache.Name)
		nodes := &corev1.NodeList{}
		labelSelector := modelCache.Spec.NodeSelector
		if err := r.List(ctx, nodes, client.MatchingLabels(labelSelector)); err != nil {
			logger.Error(err, "unable to list Nodes for local ModelCache")
			return ctrl.Result{}, err
		}
		if len(nodes.Items) == 0 {
			logger.Info("No Nodes found matching nodeSelector for local ModelCache", "nodeSelector", labelSelector)
			modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
			modelCache.Status.PvcName = ""
			modelCache.Status.DownloadJobName = ""
			status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionFalse, "NoNodesFound", fmt.Sprintf("No Nodes found matching nodeSelector %v", labelSelector))
			status.SetCondition(&modelCache, status.ConditionDownloadComplete, metav1.ConditionFalse, "DownloadPending", "Download job has not started because no suitable nodes were found")
			status.SetCondition(&modelCache, status.ConditionReady, metav1.ConditionFalse, "NoNodesAvailable", "Model is not ready because no suitable nodes were found")
			if err := status.Update(ctx, r.Client, &modelCache); err != nil {
				logger.Error(err, "unable to update ModelCache status after no Nodes found for local storage")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		logger.Info("Found Nodes matching nodeSelector for local ModelCache", "nodeSelector", labelSelector, "nodes", len(nodes.Items))

		modelCacheNodes := make([]praestov1alpha1.ModelCacheNode, 0, len(nodes.Items))
		for _, node := range nodes.Items {
			modelCacheNode, err := r.ensureModelCacheNode(ctx, &modelCache, node.Name)
			if err != nil {
				logger.Error(err, "unable to ensure ModelCacheNode for Node", "node", node.Name)
				return ctrl.Result{}, err
			}
			modelCacheNodes = append(modelCacheNodes, *modelCacheNode)
			logger.Info("Ensured ModelCacheNode for Node", "modelCacheNode", modelCacheNode.Name, "node", node.Name)
		}

		if err := r.updateModelCacheStatusFromNodes(ctx, &modelCache, modelCacheNodes); err != nil {
			logger.Error(err, "unable to update ModelCache status from ModelCacheNodes")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil

	} else {
		pvc, err := downloader.EnsureModelCachePVC(ctx, r.Client, r.Scheme, &modelCache)
		if err != nil {
			logger.Error(err, "unable to ensure PVC for ModelCache")
			return ctrl.Result{}, err
		}

		if pvc.Status.Phase != corev1.ClaimBound {
			modelCache.Status.Phase = praestov1alpha1.ModelCachePhasePending
			modelCache.Status.PvcName = pvc.Name
			status.SetCondition(&modelCache, status.ConditionPVCReady, metav1.ConditionFalse, "PVCPending", fmt.Sprintf("PVC %s is not bound yet; check that StorageClass %q can provision ReadWriteMany volumes", pvc.Name, modelCache.Spec.Storage.StorageClassName))
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
}

func isLocalModelCache(modelCache *praestov1alpha1.ModelCache) bool {
	return modelCache.Spec.Storage.StorageClassName == ""
}

func (r *ModelCacheReconciler) reconcileModelCacheDelete(ctx context.Context, modelCache *praestov1alpha1.ModelCache) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(modelCache, modelCacheFinalizer) {
		return ctrl.Result{}, nil
	}

	modelCacheNodes, err := r.listModelCacheNodesForModelCache(ctx, modelCache)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(modelCacheNodes.Items) > 0 {
		for i := range modelCacheNodes.Items {
			modelCacheNode := &modelCacheNodes.Items[i]
			if modelCacheNode.DeletionTimestamp.IsZero() {
				if err := r.Delete(ctx, modelCacheNode); err != nil && !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				logger.Info("Deleted ModelCacheNode while deleting ModelCache", "modelCacheNode", modelCacheNode.Name)
			}
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	patch := client.MergeFrom(modelCache.DeepCopy())
	controllerutil.RemoveFinalizer(modelCache, modelCacheFinalizer)
	if err := r.Patch(ctx, modelCache, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ModelCacheReconciler) listModelCacheNodesForModelCache(ctx context.Context, modelCache *praestov1alpha1.ModelCache) (*praestov1alpha1.ModelCacheNodeList, error) {
	modelCacheNodes := &praestov1alpha1.ModelCacheNodeList{}
	labels := client.MatchingLabels{
		modelCacheNodeModelNamespaceLabel: kubeident.LabelValue(modelCache.Namespace),
		modelCacheNodeModelNameLabel:      kubeident.LabelValue(modelCache.Name),
		modelCacheNodeModelUIDLabel:       kubeident.LabelValue(string(modelCache.UID)),
	}
	if err := r.List(ctx, modelCacheNodes, labels); err != nil {
		return nil, err
	}
	return modelCacheNodes, nil
}

func (r *ModelCacheReconciler) ensureModelCacheNode(ctx context.Context, modelCache *praestov1alpha1.ModelCache, nodeName string) (*praestov1alpha1.ModelCacheNode, error) {
	desired := desiredModelCacheNode(modelCache, nodeName)
	current := &praestov1alpha1.ModelCacheNode{}
	if err := r.Get(ctx, client.ObjectKey{Name: desired.Name}, current); err != nil {
		if apierrors.IsNotFound(err) {
			return desired, r.Create(ctx, desired)
		}
		return nil, err
	}

	if current.Spec.ModelCacheRef != desired.Spec.ModelCacheRef || current.Spec.NodeName != desired.Spec.NodeName || current.Spec.Storage != desired.Spec.Storage {
		current.Labels = desired.Labels
		current.Spec = desired.Spec
		if err := r.Update(ctx, current); err != nil {
			return nil, err
		}
	}

	return current, nil
}

func desiredModelCacheNode(modelCache *praestov1alpha1.ModelCache, nodeName string) *praestov1alpha1.ModelCacheNode {
	return &praestov1alpha1.ModelCacheNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: modelCacheNodeName(modelCache, nodeName),
			Labels: map[string]string{
				modelCacheNodeModelNamespaceLabel: kubeident.LabelValue(modelCache.Namespace),
				modelCacheNodeModelNameLabel:      kubeident.LabelValue(modelCache.Name),
				modelCacheNodeModelUIDLabel:       kubeident.LabelValue(string(modelCache.UID)),
				modelCacheNodeNodeLabel:           kubeident.LabelValue(nodeName),
			},
		},
		Spec: praestov1alpha1.ModelCacheNodeSpec{
			ModelCacheRef: praestov1alpha1.ModelCacheNodeModelCacheRef{
				Namespace: modelCache.Namespace,
				Name:      modelCache.Name,
				UID:       string(modelCache.UID),
			},
			NodeName: nodeName,
			Storage: praestov1alpha1.StorageNode{
				StorageClassName: modelCache.Spec.Storage.StorageClassName,
				Size:             modelCache.Spec.Storage.Size,
			},
		},
	}
}

func modelCacheNodeName(modelCache *praestov1alpha1.ModelCache, nodeName string) string {
	return kubeident.DNS1123LabelFromRaw(fmt.Sprintf("%s-%s-%s", modelCache.Namespace, modelCache.Name, nodeName))
}

func (r *ModelCacheReconciler) updateModelCacheStatusFromNodes(ctx context.Context, modelCache *praestov1alpha1.ModelCache, modelCacheNodes []praestov1alpha1.ModelCacheNode) error {
	totalNodes := int32(len(modelCacheNodes))
	var readyNodes, downloadingNodes, failedNodes, pendingNodes int32

	for _, modelCacheNode := range modelCacheNodes {
		switch modelCacheNode.Status.Phase {
		case praestov1alpha1.ModelCacheNodePhaseReady:
			readyNodes++
		case praestov1alpha1.ModelCacheNodePhaseDownloading:
			downloadingNodes++
		case praestov1alpha1.ModelCacheNodePhaseFailed:
			failedNodes++
		default:
			pendingNodes++
		}
	}

	modelCache.Status.TotalNodes = totalNodes
	modelCache.Status.ReadyNodes = readyNodes
	modelCache.Status.DownloadingNodes = downloadingNodes
	modelCache.Status.FailedNodes = failedNodes
	modelCache.Status.PendingNodes = pendingNodes
	modelCache.Status.PvcName = ""
	modelCache.Status.DownloadJobName = ""

	switch {
	case totalNodes == 0:
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
		status.SetCondition(modelCache, status.ConditionReady, metav1.ConditionFalse, "NoNodesAvailable", "No ModelCacheNodes are available")
	case failedNodes > 0:
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseFailed
		status.SetCondition(modelCache, status.ConditionReady, metav1.ConditionFalse, "ModelCacheNodeFailed", fmt.Sprintf("%d/%d ModelCacheNodes failed", failedNodes, totalNodes))
	case readyNodes == totalNodes:
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseReady
		status.SetCondition(modelCache, status.ConditionReady, metav1.ConditionTrue, "AllModelCacheNodesReady", fmt.Sprintf("All %d ModelCacheNodes are ready", totalNodes))
	case downloadingNodes > 0 || readyNodes > 0:
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhaseDownloading
		status.SetCondition(modelCache, status.ConditionReady, metav1.ConditionFalse, "ModelCacheNodesDownloading", fmt.Sprintf("%d/%d ModelCacheNodes are ready", readyNodes, totalNodes))
	default:
		modelCache.Status.Phase = praestov1alpha1.ModelCachePhasePending
		status.SetCondition(modelCache, status.ConditionReady, metav1.ConditionFalse, "ModelCacheNodesPending", fmt.Sprintf("0/%d ModelCacheNodes are ready", totalNodes))
	}

	return status.Update(ctx, r.Client, modelCache)
}

func (r *ModelCacheReconciler) readyPVCReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}

	return r.Client
}

func (r *ModelCacheReconciler) ensureStorageClassExists(ctx context.Context, modelCache *praestov1alpha1.ModelCache) (bool, error) {
	if modelCache.Spec.Storage.StorageClassName == "" {
		return true, nil
	}

	storageClass := &storagev1.StorageClass{}
	err := r.storageClassReader().Get(ctx, client.ObjectKey{Name: modelCache.Spec.Storage.StorageClassName}, storageClass)
	if err == nil {
		return true, nil
	}
	if apierrors.IsNotFound(err) {
		return false, nil
	}

	return false, err
}

func (r *ModelCacheReconciler) storageClassReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}

	return r.Client
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&praestov1alpha1.ModelCache{}).
		Watches(&praestov1alpha1.ModelCacheNode{}, handler.EnqueueRequestsFromMapFunc(r.requestsForModelCacheNode)).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		Named("modelcache").
		Complete(r)
}

func (r *ModelCacheReconciler) requestsForModelCacheNode(ctx context.Context, object client.Object) []reconcile.Request {
	modelCacheNode, ok := object.(*praestov1alpha1.ModelCacheNode)
	if !ok || modelCacheNode.Spec.ModelCacheRef.Name == "" || modelCacheNode.Spec.ModelCacheRef.Namespace == "" {
		return nil
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: modelCacheNode.Spec.ModelCacheRef.Namespace, Name: modelCacheNode.Spec.ModelCacheRef.Name}}}
}
