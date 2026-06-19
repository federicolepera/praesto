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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
)

var _ = Describe("ModelCacheNode Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		modelcachenode := &praestov1alpha1.ModelCacheNode{}

		BeforeEach(func() {
			praestoNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "praesto-system"}}
			if err := k8sClient.Create(ctx, praestoNamespace); err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}
			modelCache := &praestov1alpha1.ModelCache{
				ObjectMeta: metav1.ObjectMeta{Name: "tinyllama", Namespace: "default"},
				Spec: praestov1alpha1.ModelCacheSpec{
					Source:  praestov1alpha1.Source{Huggingface: praestov1alpha1.HuggingfaceSource{Repo: "org/model"}},
					Storage: praestov1alpha1.Storage{Size: "1Gi"},
				},
			}
			if err := k8sClient.Create(ctx, modelCache); err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}

			By("creating the custom resource for the Kind ModelCacheNode")
			err := k8sClient.Get(ctx, typeNamespacedName, modelcachenode)
			if err != nil && errors.IsNotFound(err) {
				resource := &praestov1alpha1.ModelCacheNode{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					Spec: praestov1alpha1.ModelCacheNodeSpec{
						ModelCacheRef: praestov1alpha1.ModelCacheNodeModelCacheRef{Namespace: "default", Name: "tinyllama"},
						NodeName:      "worker-1",
						Storage: praestov1alpha1.StorageNode{
							Size: "1Gi",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &praestov1alpha1.ModelCacheNode{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ModelCacheNode")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ModelCacheNodeReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
