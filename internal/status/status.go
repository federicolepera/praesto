package status

import (
	"context"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ConditionPVCReady         = "PVCReady"
	ConditionDownloadComplete = "DownloadComplete"
	ConditionReady            = "Ready"
)

func Update(ctx context.Context, c client.Client, modelCache *praestov1alpha1.ModelCache) error {
	return c.Status().Update(ctx, modelCache)
}

func SetCondition(modelCache *praestov1alpha1.ModelCache, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&modelCache.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             conditionStatus,
		ObservedGeneration: modelCache.Generation,
		Reason:             reason,
		Message:            message,
	})
}
