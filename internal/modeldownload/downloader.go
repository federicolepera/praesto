package modeldownload

import (
	"context"
	"fmt"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
)

type Request struct {
	ModelCache     *praestov1alpha1.ModelCache
	ModelCacheNode *praestov1alpha1.ModelCacheNode
	TargetPath     string
}

type Downloader interface {
	Download(ctx context.Context, req Request) error
}

type Router struct {
	HuggingFace Downloader
	// S3 Downloader
}

var _ Downloader = (*Router)(nil)

func (r *Router) Download(ctx context.Context, req Request) error {
	if req.ModelCache == nil {
		return fmt.Errorf("model cache is required")
	}

	if req.ModelCache.Spec.Source.Huggingface.Repo != "" {
		if r.HuggingFace == nil {
			return fmt.Errorf("huggingface downloader is not configured")
		}
		return r.HuggingFace.Download(ctx, req)
	}

	return fmt.Errorf("unsupported model source")
}
