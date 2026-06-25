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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const OwnerFileName = ".praesto-owner"

type Reconciler struct {
	client.Client
	NodeName  string
	CacheRoot string
	DirMode   fs.FileMode
}

type OwnerFile struct {
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	UID            string `json:"uid,omitempty"`
	ModelCacheNode string `json:"modelCacheNode"`
	Node           string `json:"node"`
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var modelCacheNode praestov1alpha1.ModelCacheNode
	if err := r.Get(ctx, req.NamespacedName, &modelCacheNode); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !modelCacheNode.DeletionTimestamp.IsZero() || modelCacheNode.Spec.NodeName != r.NodeName {
		return ctrl.Result{}, nil
	}

	original := modelCacheNode.DeepCopy()
	localPath := downloader.LocalPathForModelCacheNode(r.CacheRoot, &modelCacheNode)
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

	data, err := json.MarshalIndent(owner, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal owner marker: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(localPath, OwnerFileName), data, 0o644); err != nil {
		return fmt.Errorf("write owner marker: %w", err)
	}
	return nil
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
