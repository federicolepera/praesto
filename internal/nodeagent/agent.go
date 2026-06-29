package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	"github.com/federicolepera/praesto/internal/downloader"
	"github.com/federicolepera/praesto/internal/modeldownload"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	OwnerFileName    = ".praesto-owner"
	ManifestFileName = ".praesto-manifest.json"
	CompleteFileName = ".praesto-complete"
	FinalizerName    = "praesto.io/node-agent-finalizer"
)

type Reconciler struct {
	client.Client
	NodeName  string
	CacheRoot string
	DirMode   fs.FileMode

	Downloader modeldownload.Downloader
}

type OwnerFile struct {
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	UID            string `json:"uid,omitempty"`
	ModelCacheNode string `json:"modelCacheNode"`
	Node           string `json:"node"`
}

type ManifestFile struct {
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	UID            string `json:"uid,omitempty"`
	ModelCacheNode string `json:"modelCacheNode"`
	Node           string `json:"node"`
	Repo           string `json:"repo,omitempty"`
	Revision       string `json:"revision,omitempty"`
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var modelCacheNode praestov1alpha1.ModelCacheNode
	if err := r.Get(ctx, req.NamespacedName, &modelCacheNode); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if modelCacheNode.Spec.NodeName != r.NodeName {
		return ctrl.Result{}, nil
	}

	localPath := downloader.LocalPathForModelCacheNode(r.CacheRoot, &modelCacheNode)
	if !modelCacheNode.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &modelCacheNode, localPath)
	}

	if !controllerutil.ContainsFinalizer(&modelCacheNode, FinalizerName) {
		patch := client.MergeFrom(modelCacheNode.DeepCopy())
		controllerutil.AddFinalizer(&modelCacheNode, FinalizerName)
		if err := r.Patch(ctx, &modelCacheNode, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	if modelCacheNode.Status.Phase == praestov1alpha1.ModelCacheNodePhaseReady {
		return ctrl.Result{}, nil
	}

	var modelCache praestov1alpha1.ModelCache
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: modelCacheNode.Spec.ModelCacheRef.Namespace,
		Name:      modelCacheNode.Spec.ModelCacheRef.Name,
	}, &modelCache); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ModelCache not found, skipping", "namespace", modelCacheNode.Spec.ModelCacheRef.Namespace, "name", modelCacheNode.Spec.ModelCacheRef.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get ModelCache: %w", err)
	}

	original := modelCacheNode.DeepCopy()
	modelCacheNode.Status.LocalPath = localPath

	if err := r.prepareDirectory(localPath, ownerFor(&modelCacheNode)); err != nil {
		reason, message := conditionForPrepareError(err)
		meta.SetStatusCondition(&modelCacheNode.Status.Conditions, metav1.Condition{
			Type:               praestov1alpha1.ModelCacheNodeConditionDirectoryReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: modelCacheNode.Generation,
			Reason:             reason,
			Message:            message,
		})
		if modelCacheNode.Status.Phase == "" {
			modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhasePending
		}
		if updateErr := r.Status().Patch(ctx, &modelCacheNode, client.MergeFrom(original)); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		logger.Error(err, "unable to prepare model cache directory", "path", localPath)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	meta.SetStatusCondition(&modelCacheNode.Status.Conditions, metav1.Condition{
		Type:               praestov1alpha1.ModelCacheNodeConditionDirectoryReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: modelCacheNode.Generation,
		Reason:             "DirectoryPrepared",
		Message:            fmt.Sprintf("Model cache directory %s is ready on node %s", localPath, r.NodeName),
	})

	if err := r.Status().Patch(ctx, &modelCacheNode, client.MergeFrom(original)); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	if r.Downloader == nil {
		return ctrl.Result{}, nil
	}
	complete, err := cacheComplete(localPath)
	if err != nil {
		return ctrl.Result{}, err
	}
	if complete {
		logger.Info("model cache is already complete on node", "modelCache", client.ObjectKeyFromObject(&modelCache), "modelCacheNode", modelCacheNode.Name, "targetPath", localPath)
		markModelCacheNodeReady(&modelCacheNode, localPath)
		if err := r.Status().Patch(ctx, &modelCacheNode, client.MergeFrom(original)); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	logger.Info("starting model download on node", "modelCache", client.ObjectKeyFromObject(&modelCache), "modelCacheNode", modelCacheNode.Name, "targetPath", localPath)
	if err := r.Downloader.Download(ctx, modeldownload.Request{
		ModelCache:     &modelCache,
		ModelCacheNode: &modelCacheNode,
		TargetPath:     localPath,
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("download model: %w", err)
	}
	if err := writeManifest(localPath, manifestFor(&modelCache, &modelCacheNode)); err != nil {
		return ctrl.Result{}, err
	}
	if err := writeCompleteMarker(localPath); err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("completed model download on node", "modelCache", client.ObjectKeyFromObject(&modelCache), "modelCacheNode", modelCacheNode.Name, "targetPath", localPath)
	markModelCacheNodeReady(&modelCacheNode, localPath)
	if err := r.Status().Patch(ctx, &modelCacheNode, client.MergeFrom(original)); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) reconcileDelete(ctx context.Context, modelCacheNode *praestov1alpha1.ModelCacheNode, localPath string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(modelCacheNode, FinalizerName) {
		return ctrl.Result{}, nil
	}

	if err := os.RemoveAll(localPath); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove model cache directory %q: %w", localPath, err)
	}
	logger.Info("removed model cache directory from node", "modelCacheNode", modelCacheNode.Name, "path", localPath)

	patch := client.MergeFrom(modelCacheNode.DeepCopy())
	controllerutil.RemoveFinalizer(modelCacheNode, FinalizerName)
	if err := r.Patch(ctx, modelCacheNode, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&praestov1alpha1.ModelCacheNode{}).
		Named("node-agent").
		Complete(r)
}

type basePathMissingError struct{ path string }

func (e basePathMissingError) Error() string {
	return fmt.Sprintf("base path %q does not exist", e.path)
}

func (r *Reconciler) prepareDirectory(localPath string, owner OwnerFile) error {
	info, err := os.Stat(r.CacheRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return basePathMissingError{path: r.CacheRoot}
		}
		return fmt.Errorf("stat base path %q: %w", r.CacheRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("base path %q is not a directory", r.CacheRoot)
	}

	if err := os.MkdirAll(localPath, r.DirMode); err != nil {
		return fmt.Errorf("create model cache directory %q: %w", localPath, err)
	}
	if err := os.Chmod(filepath.Dir(localPath), r.DirMode); err != nil {
		return fmt.Errorf("chmod model cache namespace directory %q: %w", filepath.Dir(localPath), err)
	}
	if err := os.Chmod(localPath, r.DirMode); err != nil {
		return fmt.Errorf("chmod model cache directory %q: %w", localPath, err)
	}

	return writeJSONFile(filepath.Join(localPath, OwnerFileName), owner)
}

func ownerFor(modelCacheNode *praestov1alpha1.ModelCacheNode) OwnerFile {
	return OwnerFile{
		Namespace:      modelCacheNode.Spec.ModelCacheRef.Namespace,
		Name:           modelCacheNode.Spec.ModelCacheRef.Name,
		UID:            modelCacheNode.Spec.ModelCacheRef.UID,
		ModelCacheNode: modelCacheNode.Name,
		Node:           modelCacheNode.Spec.NodeName,
	}
}

func manifestFor(modelCache *praestov1alpha1.ModelCache, modelCacheNode *praestov1alpha1.ModelCacheNode) ManifestFile {
	revision := modelCache.Spec.Source.Huggingface.Revision
	if revision == "" {
		revision = "main"
	}
	return ManifestFile{
		Namespace:      modelCacheNode.Spec.ModelCacheRef.Namespace,
		Name:           modelCacheNode.Spec.ModelCacheRef.Name,
		UID:            modelCacheNode.Spec.ModelCacheRef.UID,
		ModelCacheNode: modelCacheNode.Name,
		Node:           modelCacheNode.Spec.NodeName,
		Repo:           modelCache.Spec.Source.Huggingface.Repo,
		Revision:       revision,
	}
}

func writeManifest(localPath string, manifest ManifestFile) error {
	return writeJSONFile(filepath.Join(localPath, ManifestFileName), manifest)
}

func writeCompleteMarker(localPath string) error {
	if err := os.WriteFile(filepath.Join(localPath, CompleteFileName), []byte("ok\n"), 0o644); err != nil {
		return fmt.Errorf("write complete marker: %w", err)
	}
	return nil
}

func cacheComplete(localPath string) (bool, error) {
	_, err := os.Stat(filepath.Join(localPath, CompleteFileName))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat complete marker: %w", err)
}

func markModelCacheNodeReady(modelCacheNode *praestov1alpha1.ModelCacheNode, localPath string) {
	modelCacheNode.Status.LocalPath = localPath
	modelCacheNode.Status.Phase = praestov1alpha1.ModelCacheNodePhaseReady
	meta.SetStatusCondition(&modelCacheNode.Status.Conditions, metav1.Condition{
		Type:               praestov1alpha1.ModelCacheNodeConditionDownloadComplete,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: modelCacheNode.Generation,
		Reason:             "DownloadComplete",
		Message:            "Model artifacts are present on this node",
	})
	meta.SetStatusCondition(&modelCacheNode.Status.Conditions, metav1.Condition{
		Type:               praestov1alpha1.ModelCacheNodeConditionReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: modelCacheNode.Generation,
		Reason:             "ModelReady",
		Message:            "Model is ready on this node",
	})
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func conditionForPrepareError(err error) (string, string) {
	var missing basePathMissingError
	if errors.As(err, &missing) {
		return "BasePathMissing", missing.Error()
	}
	if errors.Is(err, os.ErrPermission) {
		return "PermissionDenied", err.Error()
	}
	return "PrepareFailed", err.Error()
}
