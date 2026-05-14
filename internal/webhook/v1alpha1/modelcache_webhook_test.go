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

package v1alpha1

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
)

var _ = Describe("ModelCache Webhook", func() {
	var (
		obj       *praestov1alpha1.ModelCache
		oldObj    *praestov1alpha1.ModelCache
		validator ModelCacheCustomValidator
	)

	BeforeEach(func() {
		obj = validModelCache()
		oldObj = validModelCache()
		validator = ModelCacheCustomValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating or updating ModelCache under Validating Webhook", func() {
		It("Should admit creation when required fields are present", func() {
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should admit update when required fields are present", func() {
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should admit creation when downloader resources are omitted", func() {
			obj.Spec.Downloader.Resources = praestov1alpha1.ResourceRequirements{}

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should admit creation when only some downloader resources are set", func() {
			obj.Spec.Downloader.Resources.Requests.CPU = "250m"
			obj.Spec.Downloader.Resources.Limits.Memory = "512Mi"

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deny creation when storage fields are missing", func() {
			obj.Spec.Storage.StorageClassName = ""
			obj.Spec.Storage.Size = ""

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.storage.storageClassName"))
			Expect(err.Error()).To(ContainSubstring("spec.storage.size"))
		})

		It("Should deny creation when storage size is invalid", func() {
			obj.Spec.Storage.Size = "not-a-size"

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must be a valid Kubernetes quantity"))
		})

		It("Should deny creation when storage size is zero", func() {
			obj.Spec.Storage.Size = "0"

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must be greater than zero"))
		})

		It("Should deny creation when downloader resources are invalid", func() {
			obj.Spec.Downloader.Resources.Requests.CPU = "not-a-quantity"
			obj.Spec.Downloader.Resources.Limits.Memory = "0"

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.downloader.resources.requests.cpu"))
			Expect(err.Error()).To(ContainSubstring("spec.downloader.resources.limits.memory"))
		})

		It("Should deny creation when HuggingFace repo is missing", func() {
			obj.Spec.Source.Huggingface.Repo = ""

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.source.huggingface.repo"))
		})

		It("Should deny creation when secretRef is incomplete", func() {
			obj.Spec.Source.Huggingface.Token.SecretRef.Name = "hf-token"
			obj.Spec.Source.Huggingface.Token.SecretRef.Key = ""

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.source.huggingface.token.secretRef.key"))

			obj.Spec.Source.Huggingface.Token.SecretRef.Name = ""
			obj.Spec.Source.Huggingface.Token.SecretRef.Key = "token"

			_, err = validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.source.huggingface.token.secretRef.name"))
		})

		It("Should return all simple validation errors together", func() {
			obj.Spec.Storage.StorageClassName = ""
			obj.Spec.Storage.Size = "0"
			obj.Spec.Source.Huggingface.Repo = ""

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())

			errorMessage := err.Error()
			for _, expected := range []string{
				"spec.storage.storageClassName",
				"spec.storage.size",
				"spec.source.huggingface.repo",
			} {
				Expect(strings.Contains(errorMessage, expected)).To(BeTrue(), "expected error to contain %q", expected)
			}
		})
	})

})

func validModelCache() *praestov1alpha1.ModelCache {
	return &praestov1alpha1.ModelCache{
		Spec: praestov1alpha1.ModelCacheSpec{
			Source: praestov1alpha1.Source{
				Huggingface: praestov1alpha1.HuggingfaceSource{
					Repo:     "TinyLlama/TinyLlama-1.1B-Chat-v1.0",
					Revision: "main",
				},
			},
			Storage: praestov1alpha1.Storage{
				StorageClassName: "rwx-storage",
				Size:             "10Gi",
			},
		},
	}
}
