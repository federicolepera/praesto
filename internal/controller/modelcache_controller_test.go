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
	"github.com/federicolepera/praesto/internal/status"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
)

var _ = Describe("ModelCache Controller", func() {
	const namespace = "default"

	ctx := context.Background()
	var resourceName string
	var storageClassName string
	var typeNamespacedName types.NamespacedName

	BeforeEach(func() {
		resourceName = fmt.Sprintf("test-resource-%s", rand.String(8))
		storageClassName = fmt.Sprintf("%s-rwx", resourceName)
		typeNamespacedName = types.NamespacedName{Name: resourceName, Namespace: namespace}
		Expect(k8sClient.Create(ctx, testStorageClass(storageClassName))).To(Succeed())

		mc := &praestov1alpha1.ModelCache{
			ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: namespace},
			Spec: praestov1alpha1.ModelCacheSpec{
				Source: praestov1alpha1.Source{Huggingface: praestov1alpha1.HuggingfaceSource{
					Repo:     "org/model",
					Revision: "main",
					Token:    &praestov1alpha1.Token{SecretRef: praestov1alpha1.SecretRef{Name: "hf-secret", Key: "token"}},
				}},
				Storage: praestov1alpha1.Storage{
					StorageClassName: storageClassName,
					Size:             "1Gi",
				},
			},
		}
		Expect(k8sClient.Create(ctx, mc)).To(Succeed())
	})

	AfterEach(func() {
		job := &batchv1.Job{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: downloader.JobNameForModelCache(resourceName), Namespace: namespace}, job)
		if err == nil {
			Expect(k8sClient.Delete(ctx, job)).To(Succeed())
		} else {
			Expect(errors.IsNotFound(err)).To(BeTrue())
		}

		pvc := &corev1.PersistentVolumeClaim{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: downloader.PVCNameForModelCache(resourceName), Namespace: namespace}, pvc)
		if err == nil {
			Expect(k8sClient.Delete(ctx, pvc)).To(Succeed())
		}

		mc := &praestov1alpha1.ModelCache{}
		if err := k8sClient.Get(ctx, typeNamespacedName, mc); err == nil {
			Expect(k8sClient.Delete(ctx, mc)).To(Succeed())
		}

		storageClass := &storagev1.StorageClass{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: storageClassName}, storageClass); err == nil {
			Expect(k8sClient.Delete(ctx, storageClass)).To(Succeed())
		}
	})

	It("creates a pvc when it does not exist", func() {
		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		createdPVC := &corev1.PersistentVolumeClaim{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: downloader.PVCNameForModelCache(resourceName), Namespace: namespace}, createdPVC)
		}).Should(Succeed())

		Expect(createdPVC.Spec.AccessModes).To(ContainElement(corev1.ReadWriteMany))
		Expect(createdPVC.Spec.StorageClassName).NotTo(BeNil())
		Expect(*createdPVC.Spec.StorageClassName).To(Equal(storageClassName))
		Expect(createdPVC.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("1Gi")))
		Expect(createdPVC.OwnerReferences).NotTo(BeEmpty())
		Expect(createdPVC.OwnerReferences[0].Kind).To(Equal("ModelCache"))

		job := &batchv1.Job{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: downloader.JobNameForModelCache(resourceName), Namespace: namespace}, job)
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("creates a download job when the pvc is bound", func() {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      downloader.PVCNameForModelCache(resourceName),
				Namespace: namespace,
				Labels:    downloader.ModelCacheLabels(resourceName),
			},
			Spec: validPVCSpec(),
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		pvc.Status.Phase = corev1.ClaimBound
		Expect(k8sClient.Status().Update(ctx, pvc)).To(Succeed())

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		createdJob := &batchv1.Job{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: downloader.JobNameForModelCache(resourceName), Namespace: namespace}, createdJob)
		}).Should(Succeed())

		Expect(createdJob.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
		Expect(*createdJob.Spec.ActiveDeadlineSeconds).To(Equal(int64(7200)))
		Expect(createdJob.Spec.BackoffLimit).NotTo(BeNil())
		Expect(*createdJob.Spec.BackoffLimit).To(Equal(int32(3)))
		Expect(createdJob.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyOnFailure))
		container := createdJob.Spec.Template.Spec.Containers[0]
		Expect(container.Name).To(Equal("downloader"))
		Expect(container.Image).To(Equal(downloader.DefaultDownloaderImage))
		Expect(container.Env).To(ContainElements(
			corev1.EnvVar{Name: "HF_REPO", Value: "org/model"},
			corev1.EnvVar{Name: "SOURCE_TYPE", Value: "huggingface"},
			corev1.EnvVar{Name: "TARGET_PATH", Value: "/model"},
			corev1.EnvVar{Name: "MODELCACHE_NAME", Value: resourceName},
			corev1.EnvVar{Name: "MODELCACHE_NAMESPACE", Value: namespace},
			corev1.EnvVar{Name: "HF_REVISION", Value: "main"},
		))
		Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{Name: "model-storage", MountPath: "/model"}))
		Expect(createdJob.Spec.Template.Spec.Volumes).To(ContainElement(corev1.Volume{
			Name:         "model-storage",
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: downloader.PVCNameForModelCache(resourceName)}},
		}))
		Expect(createdJob.OwnerReferences).NotTo(BeEmpty())
		Expect(createdJob.OwnerReferences[0].Kind).To(Equal("ModelCache"))
	})

	It("does not create a job when the pvc is pending", func() {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      downloader.PVCNameForModelCache(resourceName),
				Namespace: namespace,
				Labels:    downloader.ModelCacheLabels(resourceName),
			},
			Spec: validPVCSpec(),
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		job := &batchv1.Job{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: downloader.JobNameForModelCache(resourceName), Namespace: namespace}, job)
		Expect(errors.IsNotFound(err)).To(BeTrue())

		mc := &praestov1alpha1.ModelCache{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mc)).To(Succeed())
		Expect(pvcReadyConditionReason(mc.Status.Conditions)).To(Equal("PVCPending"))
		Expect(pvcReadyConditionMessage(mc.Status.Conditions)).To(ContainSubstring("ReadWriteMany"))
		Expect(pvcReadyConditionMessage(mc.Status.Conditions)).To(ContainSubstring(storageClassName))
	})

	It("marks a ModelCache failed when its StorageClass does not exist", func() {
		storageClass := &storagev1.StorageClass{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: storageClassName}, storageClass)).To(Succeed())
		Expect(k8sClient.Delete(ctx, storageClass)).To(Succeed())

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		mc := &praestov1alpha1.ModelCache{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mc)).To(Succeed())
		Expect(mc.Status.Phase).To(Equal(praestov1alpha1.ModelCachePhaseFailed))
		Expect(pvcReadyConditionReason(mc.Status.Conditions)).To(Equal("StorageClassNotFound"))
		Expect(pvcReadyConditionMessage(mc.Status.Conditions)).To(ContainSubstring(storageClassName))

		pvc := &corev1.PersistentVolumeClaim{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: downloader.PVCNameForModelCache(resourceName), Namespace: namespace}, pvc)
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("returns an error when an existing pvc is not managed by praesto", func() {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: downloader.PVCNameForModelCache(resourceName), Namespace: namespace},
			Spec:       validPVCSpec(),
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not managed by praesto"))
	})

	It("marks a ready ModelCache failed when its PVC is missing", func() {
		mc := markModelCacheReady(ctx, typeNamespacedName, downloader.PVCNameForModelCache(resourceName))

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())

		Expect(k8sClient.Get(ctx, typeNamespacedName, mc)).To(Succeed())
		Expect(mc.Status.Phase).To(Equal(praestov1alpha1.ModelCachePhaseFailed))
		Expect(mc.Status.PvcName).To(BeEmpty())
		Expect(pvcReadyConditionReason(mc.Status.Conditions)).To(Equal("PVCLost"))
	})

	It("marks a ready ModelCache failed when its PVC is being deleted", func() {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:       downloader.PVCNameForModelCache(resourceName),
				Namespace:  namespace,
				Labels:     downloader.ModelCacheLabels(resourceName),
				Finalizers: []string{"praesto.io/test-finalizer"},
			},
			Spec: validPVCSpec(),
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		pvc.Status.Phase = corev1.ClaimBound
		Expect(k8sClient.Status().Update(ctx, pvc)).To(Succeed())
		Expect(k8sClient.Delete(ctx, pvc)).To(Succeed())

		mc := markModelCacheReady(ctx, typeNamespacedName, pvc.Name)

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("being deleted"))

		Expect(k8sClient.Get(ctx, typeNamespacedName, mc)).To(Succeed())
		Expect(mc.Status.Phase).To(Equal(praestov1alpha1.ModelCachePhaseFailed))
		Expect(mc.Status.PvcName).To(Equal(pvc.Name))
		Expect(pvcReadyConditionReason(mc.Status.Conditions)).To(Equal("PVCDeleting"))

		terminatingPVC := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: namespace}, terminatingPVC)).To(Succeed())
		terminatingPVC.Finalizers = nil
		Expect(k8sClient.Update(ctx, terminatingPVC)).To(Succeed())
	})

	It("evicts an unused ready PVC ModelCache after TTL", func() {
		ttlModelCacheName := fmt.Sprintf("ttl-cache-%s", rand.String(8))
		ttlModelCacheKey := types.NamespacedName{Name: ttlModelCacheName, Namespace: namespace}
		mc := &praestov1alpha1.ModelCache{
			ObjectMeta: metav1.ObjectMeta{Name: ttlModelCacheName, Namespace: namespace},
			Spec: praestov1alpha1.ModelCacheSpec{
				Eviction: praestov1alpha1.Eviction{UnusedTTL: "1h"},
				Source:   praestov1alpha1.Source{Huggingface: praestov1alpha1.HuggingfaceSource{Repo: "org/model"}},
				Storage:  praestov1alpha1.Storage{StorageClassName: storageClassName, Size: "1Gi"},
			},
		}
		Expect(k8sClient.Create(ctx, mc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, mc) })

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      downloader.PVCNameForModelCache(ttlModelCacheName),
				Namespace: namespace,
				Labels:    downloader.ModelCacheLabels(ttlModelCacheName),
			},
			Spec: validPVCSpec(),
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		pvc.Status.Phase = corev1.ClaimBound
		Expect(k8sClient.Status().Update(ctx, pvc)).To(Succeed())

		mc = markModelCacheReady(ctx, ttlModelCacheKey, pvc.Name)
		mc.Status.LastUsedTime = metav1.NewTime(time.Now().Add(-2 * time.Hour))
		Expect(k8sClient.Status().Update(ctx, mc)).To(Succeed())

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: ttlModelCacheKey})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, ttlModelCacheKey, mc)).To(Succeed())
		Expect(mc.Status.Phase).To(Equal(praestov1alpha1.ModelCachePhaseEvicted))
		Expect(mc.Status.PvcName).To(Equal(pvc.Name))
		Expect(pvcReadyConditionReason(mc.Status.Conditions)).To(Equal("CacheEvicted"))
	})

	It("rehydrates an evicted PVC ModelCache when a Pod requests it", func() {
		mc := &praestov1alpha1.ModelCache{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mc)).To(Succeed())
		mc.Status.Phase = praestov1alpha1.ModelCachePhaseEvicted
		mc.Status.PvcName = downloader.PVCNameForModelCache(resourceName)
		Expect(k8sClient.Status().Update(ctx, mc)).To(Succeed())

		pod := podRequestingModelCache(namespace, resourceName)
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		createdPVC := &corev1.PersistentVolumeClaim{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: downloader.PVCNameForModelCache(resourceName), Namespace: namespace}, createdPVC)
		}).Should(Succeed())

		Expect(k8sClient.Get(ctx, typeNamespacedName, mc)).To(Succeed())
		Expect(mc.Status.Phase).To(Equal(praestov1alpha1.ModelCachePhasePending))
	})

	It("creates cluster-scoped ModelCacheNodes with logical ModelCache references", func() {
		localModelCacheName := fmt.Sprintf("node-cache-%s", rand.String(8))
		localModelCacheKey := types.NamespacedName{Name: localModelCacheName, Namespace: namespace}
		nodeName := fmt.Sprintf("worker-%s", rand.String(8))

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nodeName,
				Labels: map[string]string{"praesto.io/cache-node": "true"},
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, node) })

		mc := &praestov1alpha1.ModelCache{
			ObjectMeta: metav1.ObjectMeta{Name: localModelCacheName, Namespace: namespace},
			Spec: praestov1alpha1.ModelCacheSpec{
				Source: praestov1alpha1.Source{Huggingface: praestov1alpha1.HuggingfaceSource{
					Repo:  "org/model",
					Token: &praestov1alpha1.Token{SecretRef: praestov1alpha1.SecretRef{Name: "hf-secret", Key: "token"}},
				}},
				Storage: praestov1alpha1.Storage{
					Size: "1Gi",
				},
				NodeSelector: map[string]string{"praesto.io/cache-node": "true"},
			},
		}
		Expect(k8sClient.Create(ctx, mc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, mc) })

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: localModelCacheKey})
		Expect(err).NotTo(HaveOccurred())

		createdNode := &praestov1alpha1.ModelCacheNode{}
		modelCacheNodeKey := types.NamespacedName{Name: modelCacheNodeName(mc, nodeName)}
		Eventually(func() error {
			return k8sClient.Get(ctx, modelCacheNodeKey, createdNode)
		}).Should(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, createdNode) })

		Expect(createdNode.Namespace).To(BeEmpty())
		Expect(createdNode.Spec.ModelCacheRef.Namespace).To(Equal(namespace))
		Expect(createdNode.Spec.ModelCacheRef.Name).To(Equal(localModelCacheName))
		Expect(createdNode.Spec.ModelCacheRef.UID).To(Equal(string(mc.UID)))
		Expect(createdNode.Spec.NodeName).To(Equal(nodeName))
		Expect(createdNode.Labels).To(HaveKeyWithValue(modelCacheNodeModelNamespaceLabel, kubeident.LabelValue(namespace)))
		Expect(createdNode.Labels).To(HaveKeyWithValue(modelCacheNodeModelNameLabel, kubeident.LabelValue(localModelCacheName)))

		Expect(k8sClient.Get(ctx, localModelCacheKey, mc)).To(Succeed())
		Expect(mc.Status.TotalNodes).To(Equal(int32(1)))
		Expect(mc.Status.PendingNodes).To(Equal(int32(1)))
		Expect(mc.Status.Phase).To(Equal(praestov1alpha1.ModelCachePhasePending))
	})

	It("deletes ModelCacheNodes and waits before removing the ModelCache finalizer", func() {
		localModelCacheName := fmt.Sprintf("delete-cache-%s", rand.String(8))
		localModelCacheKey := types.NamespacedName{Name: localModelCacheName, Namespace: namespace}
		nodeName := fmt.Sprintf("worker-%s", rand.String(8))

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   nodeName,
				Labels: map[string]string{"praesto.io/cache-node": "true"},
			},
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, node) })

		mc := &praestov1alpha1.ModelCache{
			ObjectMeta: metav1.ObjectMeta{Name: localModelCacheName, Namespace: namespace},
			Spec: praestov1alpha1.ModelCacheSpec{
				Source: praestov1alpha1.Source{Huggingface: praestov1alpha1.HuggingfaceSource{
					Repo:  "org/model",
					Token: &praestov1alpha1.Token{SecretRef: praestov1alpha1.SecretRef{Name: "hf-secret", Key: "token"}},
				}},
				Storage: praestov1alpha1.Storage{
					Size: "1Gi",
				},
				NodeSelector: map[string]string{"praesto.io/cache-node": "true"},
			},
		}
		Expect(k8sClient.Create(ctx, mc)).To(Succeed())

		controllerReconciler := &ModelCacheReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: localModelCacheKey})
		Expect(err).NotTo(HaveOccurred())

		modelCacheNodeKey := types.NamespacedName{Name: modelCacheNodeName(mc, nodeName)}
		createdNode := &praestov1alpha1.ModelCacheNode{}
		Eventually(func() error {
			return k8sClient.Get(ctx, modelCacheNodeKey, createdNode)
		}).Should(Succeed())
		createdNode.Finalizers = []string{"praesto.io/test-node-finalizer"}
		Expect(k8sClient.Update(ctx, createdNode)).To(Succeed())

		Expect(k8sClient.Get(ctx, localModelCacheKey, mc)).To(Succeed())
		Expect(mc.Finalizers).To(ContainElement(modelCacheFinalizer))
		Expect(k8sClient.Delete(ctx, mc)).To(Succeed())

		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: localModelCacheKey})
		Expect(err).NotTo(HaveOccurred())

		terminatingNode := &praestov1alpha1.ModelCacheNode{}
		Expect(k8sClient.Get(ctx, modelCacheNodeKey, terminatingNode)).To(Succeed())
		Expect(terminatingNode.DeletionTimestamp.IsZero()).To(BeFalse())

		terminatingModelCache := &praestov1alpha1.ModelCache{}
		Expect(k8sClient.Get(ctx, localModelCacheKey, terminatingModelCache)).To(Succeed())
		Expect(terminatingModelCache.Finalizers).To(ContainElement(modelCacheFinalizer))

		terminatingNode.Finalizers = nil
		Expect(k8sClient.Update(ctx, terminatingNode)).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, modelCacheNodeKey, &praestov1alpha1.ModelCacheNode{})
			return errors.IsNotFound(err)
		}).Should(BeTrue())

		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: localModelCacheKey})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, localModelCacheKey, &praestov1alpha1.ModelCache{})
			return errors.IsNotFound(err)
		}).Should(BeTrue())
	})
})

func validPVCSpec() corev1.PersistentVolumeClaimSpec {
	return corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Gi"),
			},
		},
	}
}

func testStorageClass(name string) *storagev1.StorageClass {
	provisioner := "praesto.test/no-provisioner"
	volumeBindingMode := storagev1.VolumeBindingImmediate
	return &storagev1.StorageClass{
		ObjectMeta:        metav1.ObjectMeta{Name: name},
		Provisioner:       provisioner,
		VolumeBindingMode: &volumeBindingMode,
	}
}

func markModelCacheReady(ctx context.Context, key types.NamespacedName, pvcName string) *praestov1alpha1.ModelCache {
	mc := &praestov1alpha1.ModelCache{}
	Expect(k8sClient.Get(ctx, key, mc)).To(Succeed())
	mc.Status.Phase = praestov1alpha1.ModelCachePhaseReady
	mc.Status.PvcName = pvcName
	mc.Status.DownloadJobName = downloader.JobNameForModelCache(mc.Name)
	Expect(k8sClient.Status().Update(ctx, mc)).To(Succeed())
	return mc
}

func podRequestingModelCache(namespace, modelCacheName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("consumer-%s", rand.String(8)),
			Namespace: namespace,
			Labels: map[string]string{
				usesModelCacheLabelKey: "true",
			},
			Annotations: map[string]string{
				modelMountsAnnotationKey: fmt.Sprintf(`[{"modelCache":"%s","mountPath":"/models"}]`, modelCacheName),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "busybox:1.36"}},
		},
	}
}

func pvcReadyConditionReason(conditions []metav1.Condition) string {
	for _, condition := range conditions {
		if condition.Type == status.ConditionPVCReady {
			return condition.Reason
		}
	}

	return ""
}

func pvcReadyConditionMessage(conditions []metav1.Condition) string {
	for _, condition := range conditions {
		if condition.Type == status.ConditionPVCReady {
			return condition.Message
		}
	}

	return ""
}
