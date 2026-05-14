//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/federicolepera/praesto/test/utils"
)

// namespace where the project is deployed in
const namespace = "praesto-system"

// namespace used by Praesto workload-level e2e checks.
const workloadNamespace = "praesto-e2e"

const modelCacheVolumeName = "praesto-model-cache"

const workloadNamespaceWithoutInjection = "praesto-e2e-no-injection"

const realDownloadModelCacheName = "real-download-cache"

const realDownloadPVCName = "praesto-real-download-cache"

const realDownloadJobName = "praesto-download-real-download-cache"

const realDownloadPVName = "praesto-e2e-real-download-cache"

const realDownloadStorageClass = "praesto-e2e-rwx"

// serviceAccountName created for the project
const serviceAccountName = "praesto-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "praesto-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "praesto-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("creating workload namespace")
		_, err = kubectlApplyYAML(fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    praesto.io/model-cache-injection: enabled
`, workloadNamespace))
		Expect(err).NotTo(HaveOccurred(), "Failed to create workload namespace")

		By("creating workload namespace without Praesto injection")
		_, err = kubectlApplyYAML(fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, workloadNamespaceWithoutInjection))
		Expect(err).NotTo(HaveOccurred(), "Failed to create workload namespace without injection")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("cleaning up the metrics ClusterRoleBinding")
		cmd = exec.Command("kubectl", "delete", "clusterrolebinding", metricsRoleBindingName, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing workload namespace")
		cmd = exec.Command("kubectl", "delete", "ns", workloadNamespace, "--ignore-not-found", "--wait=false")
		_, _ = utils.Run(cmd)

		By("removing workload namespace without injection")
		cmd = exec.Command("kubectl", "delete", "ns", workloadNamespaceWithoutInjection, "--ignore-not-found", "--wait=false")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace, "--wait=false")
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching workload namespace diagnostics")
			for _, args := range [][]string{
				{"get", "modelcache,pvc,job,pod", "-n", workloadNamespace, "-o", "wide"},
				{"get", "events", "-n", workloadNamespace, "--sort-by=.lastTimestamp"},
				{"logs", "job/praesto-download-real-download-cache", "-n", workloadNamespace, "--all-containers=true"},
				{"describe", "job", "praesto-download-real-download-cache", "-n", workloadNamespace},
				{"describe", "pvc", "praesto-real-download-cache", "-n", workloadNamespace},
				{"describe", "pod", "real-download-consumer", "-n", workloadNamespace},
			} {
				cmd = exec.Command("kubectl", args...)
				output, err := utils.Run(cmd)
				if err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "kubectl %v:\n%s\n", args, output)
				}
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod is running and ready.
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")

				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.containerStatuses[0].ready}",
					"-n", namespace,
				)
				output, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"), "controller-manager container is not ready")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			_, err := kubectlApplyYAML(fmt.Sprintf(`
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: praesto-metrics-reader
subjects:
- kind: ServiceAccount
  name: %s
  namespace: %s
`, metricsRoleBindingName, serviceAccountName, namespace))
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd := exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		It("should provisioned cert-manager", func() {
			By("validating that cert-manager has the certificate Secret")
			verifyCertManager := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "secrets", "webhook-server-cert", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}
			Eventually(verifyCertManager).Should(Succeed())
		})

		It("should have CA injection for validating webhooks", func() {
			By("checking CA injection for validating webhooks")
			verifyCAInjection := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"validatingwebhookconfigurations.admissionregistration.k8s.io",
					"praesto-validating-webhook-configuration",
					"-o", "go-template={{ range .webhooks }}{{ .clientConfig.caBundle }}{{ end }}")
				vwhOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(vwhOutput)).To(BeNumerically(">", 10))
			}
			Eventually(verifyCAInjection).Should(Succeed())
		})

		It("should reject invalid ModelCache resources through the validating webhook", func() {
			By("applying an invalid ModelCache")
			output, err := kubectlApplyYAML(fmt.Sprintf(`
apiVersion: praesto.praesto.io/v1alpha1
kind: ModelCache
metadata:
  name: invalid-modelcache
  namespace: %s
spec:
  source:
    huggingface:
      repo: ""
  storage:
    storageClassName: standard
    size: "0"
`, workloadNamespace))

			Expect(err).To(HaveOccurred(), "invalid ModelCache should be rejected")
			Expect(output).To(ContainSubstring("spec.source.huggingface.repo"))
			Expect(output).To(ContainSubstring("spec.storage.size"))
		})

		It("should reconcile a ModelCache by creating its managed PVC", func() {
			By("creating a valid ModelCache")
			_, err := kubectlApplyYAML(fmt.Sprintf(`
apiVersion: praesto.praesto.io/v1alpha1
kind: ModelCache
metadata:
  name: pvc-lifecycle-cache
  namespace: %s
spec:
  source:
    huggingface:
      repo: TinyLlama/TinyLlama-1.1B-Chat-v1.0
      revision: main
  storage:
    storageClassName: standard
    size: 10Gi
`, workloadNamespace))
			Expect(err).NotTo(HaveOccurred(), "valid ModelCache should be accepted")

			By("verifying the controller creates the managed PVC and updates status")
			Eventually(func(g Gomega) {
				pvcName := kubectlJSONPath(g, "modelcache", "pvc-lifecycle-cache", workloadNamespace, `{.status.pvcName}`)
				g.Expect(pvcName).To(Equal("praesto-pvc-lifecycle-cache"))

				managedLabel := kubectlJSONPath(g, "pvc", "praesto-pvc-lifecycle-cache", workloadNamespace, `{.metadata.labels.praesto\.io/managed}`)
				g.Expect(managedLabel).To(Equal("true"))

				modelLabel := kubectlJSONPath(g, "pvc", "praesto-pvc-lifecycle-cache", workloadNamespace, `{.metadata.labels.praesto\.io/model}`)
				g.Expect(modelLabel).To(Equal("pvc-lifecycle-cache"))

				accessMode := kubectlJSONPath(g, "pvc", "praesto-pvc-lifecycle-cache", workloadNamespace, `{.spec.accessModes[0]}`)
				g.Expect(accessMode).To(Equal("ReadWriteMany"))

				storageClassName := kubectlJSONPath(g, "pvc", "praesto-pvc-lifecycle-cache", workloadNamespace, `{.spec.storageClassName}`)
				g.Expect(storageClassName).To(Equal("standard"))
			}).Should(Succeed())
		})

		It("should complete a real downloader flow and expose downloaded files to a consumer Pod", func() {
			const consumerPod = "real-download-consumer"

			By("creating a static RWX PersistentVolume for the real download PVC")
			_, err := kubectlApplyYAML(fmt.Sprintf(`
apiVersion: v1
kind: PersistentVolume
metadata:
  name: %s
spec:
  capacity:
    storage: 1Gi
  accessModes:
  - ReadWriteMany
  storageClassName: %s
  persistentVolumeReclaimPolicy: Delete
  claimRef:
    namespace: %s
    name: %s
  hostPath:
    path: /tmp/%s
    type: DirectoryOrCreate
`, realDownloadPVName, realDownloadStorageClass, workloadNamespace, realDownloadPVCName, realDownloadPVName))
			Expect(err).NotTo(HaveOccurred(), "static RWX PV should be created")

			By("creating a ModelCache that uses the e2e downloader image")
			_, err = kubectlApplyYAML(fmt.Sprintf(`
apiVersion: praesto.praesto.io/v1alpha1
kind: ModelCache
metadata:
  name: %s
  namespace: %s
spec:
  source:
    huggingface:
      repo: hf-internal-testing/tiny-random-bert
      revision: main
  storage:
    storageClassName: %s
    size: 1Gi
  downloader:
    image: %s
`, realDownloadModelCacheName, workloadNamespace, realDownloadStorageClass, downloaderImage))
			Expect(err).NotTo(HaveOccurred(), "real ModelCache should be accepted")

			By("waiting for the PVC to bind")
			Eventually(func(g Gomega) {
				phase := kubectlJSONPath(g, "pvc", realDownloadPVCName, workloadNamespace, `{.status.phase}`)
				g.Expect(phase).To(Equal("Bound"))
			}, 2*time.Minute).Should(Succeed())

			By("waiting for the downloader Job to complete")
			cmd := exec.Command("kubectl", "wait", "--for=condition=complete", "job/"+realDownloadJobName,
				"-n", workloadNamespace, "--timeout=10m")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "downloader Job should complete")

			ensureRealDownloadCacheReady()

			By("creating a consumer Pod that reads a downloaded file from the injected volume")
			_, err = kubectlApplyYAML(fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  annotations:
    praesto.io/model-cache: %s
    praesto.io/model-mount-path: /models
spec:
  restartPolicy: Never
  containers:
  - name: app
    image: busybox:1.36
    command: ["sh", "-c", "test -f /models/config.json && test -f /models/.praesto-complete"]
`, consumerPod, workloadNamespace, realDownloadModelCacheName))
			Expect(err).NotTo(HaveOccurred(), "consumer Pod should be admitted and mutated")

			By("waiting for the consumer Pod to read the downloaded files successfully")
			Eventually(func(g Gomega) {
				phase := kubectlJSONPath(g, "pod", consumerPod, workloadNamespace, `{.status.phase}`)
				g.Expect(phase).To(Equal("Succeeded"))
			}, 2*time.Minute).Should(Succeed())
		})

		It("should inject a ready ModelCache volume into an annotated Pod", func() {
			ensureRealDownloadCacheReady()

			By("creating an annotated Pod")
			_, err := kubectlApplyYAML(fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: single-container-consumer
  namespace: %s
  annotations:
    praesto.io/model-cache: %s
    praesto.io/model-mount-path: /models
spec:
  containers:
  - name: app
    image: busybox:1.36
    command: ["sh", "-c", "sleep 3600"]
`, workloadNamespace, realDownloadModelCacheName))
			Expect(err).NotTo(HaveOccurred(), "annotated Pod should be admitted and mutated")

			By("verifying the injected read-only PVC volume and mount")
			Eventually(func(g Gomega) {
				claimName := kubectlJSONPath(g, "pod", "single-container-consumer", workloadNamespace,
					fmt.Sprintf(`{.spec.volumes[?(@.name=="%s")].persistentVolumeClaim.claimName}`, modelCacheVolumeName))
				g.Expect(claimName).To(Equal(realDownloadPVCName))

				mountPath := kubectlJSONPath(g, "pod", "single-container-consumer", workloadNamespace,
					fmt.Sprintf(`{.spec.containers[0].volumeMounts[?(@.name=="%s")].mountPath}`, modelCacheVolumeName))
				g.Expect(mountPath).To(Equal("/models"))

				readOnly := kubectlJSONPath(g, "pod", "single-container-consumer", workloadNamespace,
					fmt.Sprintf(`{.spec.containers[0].volumeMounts[?(@.name=="%s")].readOnly}`, modelCacheVolumeName))
				g.Expect(readOnly).To(Equal("true"))
			}).Should(Succeed())
		})

		It("should inject a ready ModelCache volume only into the selected target container", func() {
			ensureRealDownloadCacheReady()

			By("creating an annotated multi-container Pod")
			_, err := kubectlApplyYAML(fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: target-container-consumer
  namespace: %s
  annotations:
    praesto.io/model-cache: %s
    praesto.io/model-mount-path: /models
    praesto.io/target-container: app
spec:
  containers:
  - name: sidecar
    image: busybox:1.36
    command: ["sh", "-c", "sleep 3600"]
  - name: app
    image: busybox:1.36
    command: ["sh", "-c", "sleep 3600"]
`, workloadNamespace, realDownloadModelCacheName))
			Expect(err).NotTo(HaveOccurred(), "annotated multi-container Pod should be admitted and mutated")

			By("verifying only the selected container receives the mount")
			Eventually(func(g Gomega) {
				appMountPath := kubectlJSONPath(g, "pod", "target-container-consumer", workloadNamespace,
					fmt.Sprintf(`{.spec.containers[?(@.name=="app")].volumeMounts[?(@.name=="%s")].mountPath}`, modelCacheVolumeName))
				g.Expect(appMountPath).To(Equal("/models"))

				sidecarMountPath := kubectlJSONPath(g, "pod", "target-container-consumer", workloadNamespace,
					fmt.Sprintf(`{.spec.containers[?(@.name=="sidecar")].volumeMounts[?(@.name=="%s")].mountPath}`, modelCacheVolumeName))
				g.Expect(sidecarMountPath).To(BeEmpty())
			}).Should(Succeed())
		})

		It("should not call the mutating webhook in namespaces without injection enabled", func() {
			By("creating an annotated Pod that references a missing ModelCache in a non-opt-in namespace")
			_, err := kubectlApplyYAML(fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: no-injection-consumer
  namespace: %s
  annotations:
    praesto.io/model-cache: missing-cache
    praesto.io/model-mount-path: /models
spec:
  containers:
  - name: app
    image: busybox:1.36
    command: ["sh", "-c", "sleep 3600"]
`, workloadNamespaceWithoutInjection))
			Expect(err).NotTo(HaveOccurred(), "Pod in non-opt-in namespace should not be rejected by the mutating webhook")

			By("verifying no Praesto volume was injected")
			Eventually(func(g Gomega) {
				claimName := kubectlJSONPath(g, "pod", "no-injection-consumer", workloadNamespaceWithoutInjection,
					fmt.Sprintf(`{.spec.volumes[?(@.name=="%s")].persistentVolumeClaim.claimName}`, modelCacheVolumeName))
				g.Expect(claimName).To(BeEmpty())
			}).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		// TODO: Customize the e2e test suite with scenarios specific to your project.
		// Consider applying sample/CR(s) and check their status and/or verifying
		// the reconciliation by using the metrics, i.e.:
		// metricsOutput, err := getMetricsOutput()
		// Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
		// Expect(metricsOutput).To(ContainSubstring(
		//    fmt.Sprintf(`controller_runtime_reconcile_total{controller="%s",result="success"} 1`,
		//    strings.ToLower(<Kind>),
		// ))
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

func kubectlApplyYAML(yaml string) (string, error) {
	manifestFile, err := os.CreateTemp("", "praesto-e2e-*.yaml")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = os.Remove(manifestFile.Name())
	}()

	if _, err := manifestFile.WriteString(yaml); err != nil {
		_ = manifestFile.Close()
		return "", err
	}
	if err := manifestFile.Close(); err != nil {
		return "", err
	}

	cmd := exec.Command("kubectl", "apply", "-f", manifestFile.Name())
	return utils.Run(cmd)
}

func ensureRealDownloadCacheReady() {
	By("verifying the real download ModelCache is Ready")
	Eventually(func(g Gomega) {
		phase := kubectlJSONPath(g, "modelcache", realDownloadModelCacheName, workloadNamespace, `{.status.phase}`)
		g.Expect(phase).To(Equal("Ready"))

		statusPVCName := kubectlJSONPath(g, "modelcache", realDownloadModelCacheName, workloadNamespace, `{.status.pvcName}`)
		g.Expect(statusPVCName).To(Equal(realDownloadPVCName))
	}, 2*time.Minute).Should(Succeed())
}

func kubectlJSONPath(g Gomega, resourceType, name, resourceNamespace, jsonPath string) string {
	cmd := exec.Command("kubectl", "get", resourceType, name,
		"-n", resourceNamespace,
		"-o", fmt.Sprintf("jsonpath=%s", jsonPath),
	)
	output, err := utils.Run(cmd)
	g.Expect(err).NotTo(HaveOccurred())
	return output
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
