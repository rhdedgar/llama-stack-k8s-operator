//nolint:testpackage
package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/llamastack/llama-stack-k8s-operator/api/v1alpha1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"
)

const (
	vllmNS              = "vllm-dist"
	vllmTestTimeout     = 10 * time.Minute
	vllmHealthCheckPath = "/health"
	vllmModelsPath      = "/v1/models"
	vllmCompletionsPath = "/v1/completions"
	llsTestNS           = "llama-stack-test"
)

var (
	projectRoot, _                 = filepath.Abs("../..")
	vllmOpenShiftPrerequisitesPath = filepath.Join(projectRoot, "config", "samples", "vllm", "openshift", "00_prerequisites.yaml")
	vllmKubernetesConfigPath       = filepath.Join(projectRoot, "config", "samples", "vllm", "vllm-local-model.yaml")
	certificateScriptPath          = filepath.Join(projectRoot, "config", "samples", "generate_certificates.sh")
	serverCertPath                 = filepath.Join(projectRoot, "config", "samples", "vllm-certs", "server.crt")
	serverKeyPath                  = filepath.Join(projectRoot, "config", "samples", "vllm-certs", "server.key")
	caBundlePath                   = filepath.Join(projectRoot, "config", "samples", "vllm-ca-certs", "ca-bundle.crt")
	llamaStackTLSTestConfigPath    = filepath.Join(projectRoot, "config", "samples", "vllm", "example-with-vllm-tls.yaml")
)

func TestVLLMTLSSuite(t *testing.T) {
	if TestOpts.SkipCreation {
		t.Skip("Skipping vLLM TLS test suite")
	}

	// Generate certificates before running any tests
	t.Run("should generate certificates", func(t *testing.T) {
		generateCertificates(t)
	})

	t.Run("should copy vllm-ca-certs secret to vllm-dist namespace", func(t *testing.T) {
		copyTLSSecretsToNamespace(t, llsTestNS)
	})

	t.Run("should setup vLLM TLS infrastructure", func(t *testing.T) {
		testVLLMTLSSetup(t)
	})

	t.Run("should create vLLM server with TLS", func(t *testing.T) {
		testVLLMServerDeployment(t)
	})

	t.Run("should create LlamaStackDistribution with CA bundle", func(t *testing.T) {
		testLlamaStackWithCABundle(t)
	})

	t.Run("should validate secure connection from LlamaStack to vLLM", func(t *testing.T) {
		testSecureConnectionValidation(t)
	})

	t.Run("should cleanup vLLM TLS resources", func(t *testing.T) {
		testVLLMTLSCleanup(t)
	})
}

func testVLLMTLSSetup(t *testing.T) {
	t.Helper()

	// Create vLLM namespace
	vllmNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: vllmNS,
		},
	}
	err := TestEnv.Client.Create(TestEnv.Ctx, vllmNs)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}

	// Create test namespace
	testNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: llsTestNS,
		},
	}
	err = TestEnv.Client.Create(TestEnv.Ctx, testNs)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}

	// Create secrets needed for vLLM server (like in GitHub workflow)
	err = createVLLMTLSSecrets(t)
	require.NoError(t, err)

	// Create CA bundle configmap in test namespace
	err = createCABundleConfigMap(t, llsTestNS)
	require.NoError(t, err)

	// Verify the CA bundle ConfigMap was created correctly
	err = verifyCABundleConfigMap(t, llsTestNS)
	require.NoError(t, err)
}

func testVLLMServerDeployment(t *testing.T) {
	t.Helper()

	// Deploy vLLM server
	err := deployVLLMServer(t)
	require.NoError(t, err)

	// Wait for vLLM deployment to be ready
	err = EnsureResourceReady(t, TestEnv, schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, "vllm-server", vllmNS, vllmTestTimeout, isDeploymentReady)
	require.NoError(t, err, "vLLM deployment should be ready")

	// Wait for vLLM service to be ready
	err = EnsureResourceReady(t, TestEnv, schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Service",
	}, "vllm-server", vllmNS, ResourceReadyTimeout, func(u *unstructured.Unstructured) bool {
		spec, specFound, _ := unstructured.NestedMap(u.Object, "spec")
		return specFound && spec != nil
	})
	require.NoError(t, err, "vLLM service should be ready")
}

func testLlamaStackWithCABundle(t *testing.T) {
	t.Helper()

	// Deploy LlamaStackDistribution with CA bundle
	err := deployLlamaStackWithCABundle(t)
	require.NoError(t, err)

	// The YAML file creates a placeholder ConfigMap, so we need to update it with the actual CA bundle
	err = updateCABundleConfigMap(t, llsTestNS)
	require.NoError(t, err)

	// Verify the CA bundle ConfigMap has the correct content after update
	err = verifyCABundleConfigMap(t, llsTestNS)
	require.NoError(t, err)

	// Verify the LlamaStack distribution is configured with TLS
	err = verifyLlamaStackTLSConfig(t, llsTestNS, "vllm-tls-test")
	require.NoError(t, err)

	// Restart the deployment to pick up the updated ConfigMap
	err = restartDeployment(t, llsTestNS, "vllm-tls-test")
	require.NoError(t, err)

	// Wait for LlamaStack deployment to be ready
	err = EnsureResourceReady(t, TestEnv, schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, "vllm-tls-test", llsTestNS, vllmTestTimeout, isDeploymentReady)
	require.NoError(t, err, "LlamaStack deployment should be ready")
}

func testSecureConnectionValidation(t *testing.T) {
	t.Helper()

	// First check if the vLLM server is still healthy
	err := waitForVLLMHealth(t)
	require.NoError(t, err, "vLLM server should be healthy")

	// First check if the LlamaStack deployment is ready
	err = EnsureResourceReady(t, TestEnv, schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, "vllm-tls-test", llsTestNS, vllmTestTimeout, isDeploymentReady)
	require.NoError(t, err, "LlamaStack deployment should be ready")

	// Get deployment logs for debugging
	err = debugLlamaStackDeployment(t)
	if err != nil {
		t.Logf("Failed to get deployment logs: %v", err)
	}

	// Check network connectivity from llama-stack-test namespace to vllm-dist namespace
	err = checkNetworkConnectivity(t)
	if err != nil {
		t.Logf("Network connectivity check failed: %v", err)
	}

	// Wait for LlamaStack to be healthy and report provider status with shorter timeout for debugging
	err = waitForLlamaStackHealthWithTimeout(t, 5*time.Minute)
	if err != nil {
		t.Logf("LlamaStack health check failed after 5 minutes, getting final status...")
		distribution := &v1alpha1.LlamaStackDistribution{}
		if getErr := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
			Namespace: llsTestNS,
			Name:      "vllm-tls-test",
		}, distribution); getErr == nil {
			t.Logf("Final distribution status: Phase=%s", distribution.Status.Phase)
		}
		require.NoError(t, err, "LlamaStack should be healthy")
	}

	// Validate that the vLLM provider is accessible
	err = validateVLLMProviderStatus(t)
	require.NoError(t, err, "vLLM provider should be accessible through LlamaStack")
}

func testVLLMTLSCleanup(t *testing.T) {
	t.Helper()

	// Delete LlamaStackDistribution
	distribution := &v1alpha1.LlamaStackDistribution{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vllm-tls-test",
			Namespace: llsTestNS,
		},
	}
	err := TestEnv.Client.Delete(TestEnv.Ctx, distribution)
	if err != nil && !k8serrors.IsNotFound(err) {
		require.NoError(t, err)
	}

	// Wait for LlamaStack resources to be cleaned up
	err = EnsureResourceDeleted(t, TestEnv, schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, "vllm-tls-test", llsTestNS, ResourceReadyTimeout)
	require.NoError(t, err, "LlamaStack deployment should be deleted")
}

// Helper functions

func generateCertificates(t *testing.T) {
	t.Helper()

	// Check if both certificate files exist
	if _, err := os.Stat(serverCertPath); err == nil {
		if _, err = os.Stat(caBundlePath); err == nil {
			t.Log("Certificates already exist, skipping generation")
			return
		}
	}

	// Run the certificate generation script
	t.Logf("Running certificate generation script: %s", certificateScriptPath)

	// Change to the project root directory to run the script
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer func() {
		err = os.Chdir(originalDir)
		require.NoError(t, err, "Failed to restore original directory")
	}()

	err = os.Chdir(projectRoot)
	require.NoError(t, err, "Failed to change to project root")

	// Execute the script
	cmd := exec.Command("bash", certificateScriptPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Certificate generation script output: %s", string(output))
		require.NoError(t, err, "Failed to run certificate generation script")
	}

	t.Log("Certificates generated successfully")
}

func copyTLSSecretsToNamespace(t *testing.T, targetNS string) error {
	t.Helper()

	// Copy vllm-certs secret
	vllmCerts := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vllm-certs",
			Namespace: targetNS,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"server.crt": {}, // Will be populated by certificate generation
			"server.key": {}, // Will be populated by certificate generation
		},
	}

	// Read certificate files
	serverCrt, err := os.ReadFile(serverCertPath)
	if err != nil {
		return fmt.Errorf("failed to read server certificate: %w", err)
	}
	serverKey, err := os.ReadFile(serverKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read server key: %w", err)
	}

	vllmCerts.Data["server.crt"] = serverCrt
	vllmCerts.Data["server.key"] = serverKey

	err = TestEnv.Client.Create(TestEnv.Ctx, vllmCerts)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create vllm-certs secret: %w", err)
	}

	return nil
}

func createCABundleConfigMap(t *testing.T, targetNS string) error {
	t.Helper()

	// Read CA bundle
	caBundle, err := os.ReadFile(caBundlePath)
	if err != nil {
		return fmt.Errorf("failed to read CA bundle: %w", err)
	}

	caBundleConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vllm-ca-bundle",
			Namespace: targetNS,
		},
		Data: map[string]string{
			"ca-bundle.crt": string(caBundle),
		},
	}

	// Try to create, if it exists, update it
	err = TestEnv.Client.Create(TestEnv.Ctx, caBundleConfigMap)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			// ConfigMap exists, update it
			existingConfigMap := &corev1.ConfigMap{}
			err = TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
				Namespace: targetNS,
				Name:      "vllm-ca-bundle",
			}, existingConfigMap)
			if err != nil {
				return fmt.Errorf("failed to get existing ConfigMap: %w", err)
			}

			existingConfigMap.Data["ca-bundle.crt"] = string(caBundle)
			err = TestEnv.Client.Update(TestEnv.Ctx, existingConfigMap)
			if err != nil {
				return fmt.Errorf("failed to update existing ConfigMap: %w", err)
			}
			t.Logf("Updated existing CA bundle ConfigMap with %d bytes", len(caBundle))
		} else {
			return fmt.Errorf("failed to create CA bundle configmap: %w", err)
		}
	} else {
		t.Logf("Created CA bundle ConfigMap with %d bytes", len(caBundle))
	}

	return nil
}

func verifyCABundleConfigMap(t *testing.T, targetNS string) error {
	t.Helper()

	// Get the ConfigMap
	configMap := &corev1.ConfigMap{}
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: targetNS,
		Name:      "vllm-ca-bundle",
	}, configMap)
	if err != nil {
		return fmt.Errorf("failed to get CA bundle ConfigMap: %w", err)
	}

	// Verify the CA bundle content exists
	caBundle, exists := configMap.Data["ca-bundle.crt"]
	if !exists {
		return errors.New("CA bundle ConfigMap does not contain key named ca-bundle.crt")
	}

	if len(caBundle) == 0 {
		return errors.New("CA bundle ConfigMap ca-bundle.crt is empty")
	}

	t.Logf("CA bundle ConfigMap verified: found %d bytes of CA bundle data", len(caBundle))

	// Check if CA bundle appears to be a placeholder
	if len(caBundle) < 100 || !strings.Contains(caBundle, "BEGIN CERTIFICATE") {
		t.Logf("WARNING: CA bundle appears to be a placeholder or invalid")
		t.Logf("CA bundle content: %s", caBundle)

		// Try to update the ConfigMap with the actual CA bundle from the file
		err := updateCABundleConfigMap(t, targetNS)
		if err != nil {
			t.Logf("Failed to update CA bundle ConfigMap: %v", err)
		}
	}

	return nil
}

func verifyLlamaStackTLSConfig(t *testing.T, namespace, name string) error {
	t.Helper()

	// Get the LlamaStack distribution
	distribution := &v1alpha1.LlamaStackDistribution{}
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, distribution)
	if err != nil {
		return fmt.Errorf("failed to get LlamaStack distribution: %w", err)
	}

	// Verify TLS configuration is present
	if distribution.Spec.Server.TLSConfig == nil {
		return errors.New("LlamaStack distribution does not have TLS config")
	}

	if distribution.Spec.Server.TLSConfig.CABundle == nil {
		return errors.New("LlamaStack distribution TLS config does not have CA bundle")
	}

	t.Logf("LlamaStack distribution TLS config verified:")
	t.Logf("  CA Bundle ConfigMap: %s", distribution.Spec.Server.TLSConfig.CABundle.ConfigMapName)
	t.Logf("  CA Bundle Key: %s", distribution.Spec.Server.TLSConfig.CABundle.Key)

	return nil
}

func createVLLMTLSSecrets(t *testing.T) error {
	t.Helper()

	// Create vllm-ca-certs secret in default namespace (for external access)
	caBundleData, err := os.ReadFile(caBundlePath)
	if err != nil {
		return fmt.Errorf("failed to read CA bundle: %w", err)
	}

	caCertsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vllm-ca-certs",
			Namespace: "default",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ca-bundle.crt": caBundleData,
		},
	}

	err = TestEnv.Client.Create(TestEnv.Ctx, caCertsSecret)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create vllm-ca-certs secret: %w", err)
	}

	// Create vllm-certs secret in vllm-dist namespace (for the vLLM server)
	serverCrtData, err := os.ReadFile(serverCertPath)
	if err != nil {
		return fmt.Errorf("failed to read server certificate: %w", err)
	}

	serverKeyData, err := os.ReadFile(serverKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read server key: %w", err)
	}

	vllmCertsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vllm-certs",
			Namespace: "vllm-dist",
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"server.crt": serverCrtData,
			"server.key": serverKeyData,
		},
	}

	err = TestEnv.Client.Create(TestEnv.Ctx, vllmCertsSecret)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create vllm-certs secret: %w", err)
	}

	t.Logf("Created vllm-ca-certs secret (%d bytes) and vllm-certs secret (%d bytes)",
		len(caBundleData), len(serverCrtData)+len(serverKeyData))

	return nil
}

func updateCABundleConfigMap(t *testing.T, targetNS string) error {
	t.Helper()

	// Read the actual CA bundle from the file
	actualCABundle, err := os.ReadFile(caBundlePath)
	if err != nil {
		return fmt.Errorf("failed to read CA bundle file: %w", err)
	}

	// Get the existing ConfigMap
	configMap := &corev1.ConfigMap{}
	err = TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: targetNS,
		Name:      "vllm-ca-bundle",
	}, configMap)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Update the ConfigMap with the actual CA bundle
	configMap.Data["ca-bundle.crt"] = string(actualCABundle)

	err = TestEnv.Client.Update(TestEnv.Ctx, configMap)
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}

	t.Logf("Updated CA bundle ConfigMap with %d bytes of actual CA bundle data", len(actualCABundle))
	return nil
}

func restartDeployment(t *testing.T, namespace, name string) error {
	t.Helper()

	// Get the deployment
	deployment := &appsv1.Deployment{}
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, deployment)
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Add a restart annotation to trigger pod restart
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	// Update the deployment
	err = TestEnv.Client.Update(TestEnv.Ctx, deployment)
	if err != nil {
		return fmt.Errorf("failed to update deployment: %w", err)
	}

	t.Logf("Restarted deployment %s in namespace %s", name, namespace)
	return nil
}

// isOpenShiftCluster detects if the current cluster is running OpenShift by checking
// for the SecurityContextConstraints resource in the security.openshift.io API group.
// This is equivalent to: kubectl api-resources --api-group=security.openshift.io | grep -iq 'SecurityContextConstraints'.
func isOpenShiftCluster(cfg *rest.Config) (bool, error) {
	// Create a discovery client
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return false, fmt.Errorf("failed to create discovery client: %w", err)
	}

	// Check if the security.openshift.io API group exists
	apiGroupList, err := discoveryClient.ServerGroups()
	if err != nil {
		return false, fmt.Errorf("failed to get server groups: %w", err)
	}

	// Look for the security.openshift.io API group
	for _, group := range apiGroupList.Groups {
		if group.Name == "security.openshift.io" {
			// Found the OpenShift security API group, now check for SecurityContextConstraints
			resourceList, err := discoveryClient.ServerResourcesForGroupVersion("security.openshift.io/v1")
			if err != nil {
				// If we can't get resources for this group version, continue checking
				continue
			}

			// Check if SecurityContextConstraints resource exists
			for _, resource := range resourceList.APIResources {
				if resource.Kind == "SecurityContextConstraints" {
					return true, nil
				}
			}
		}
	}

	return false, nil
}

// getRestConfig returns the REST config for cluster communication.
func getRestConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		// If not in cluster, get config from kubeconfig (using the same method as test setup)
		cfg, err = config.GetConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get REST config: %w", err)
		}
	}
	return cfg, nil
}

// applyYAMLFile reads a YAML file and applies all resources to the cluster.
func applyYAMLFile(t *testing.T, yamlPath string) error {
	t.Helper()

	// Read YAML file
	yamlData, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("failed to read YAML file %s: %w", yamlPath, err)
	}

	// Parse YAML into Kubernetes objects
	objects, err := parseKubernetesYAML(yamlData)
	if err != nil {
		return fmt.Errorf("failed to parse YAML file %s: %w", yamlPath, err)
	}

	// Apply each object to the cluster
	for _, obj := range objects {
		// Set namespace for namespace-scoped resources that don't have one
		if obj.GetNamespace() == "" && isNamespaceScoped(obj) {
			obj.SetNamespace(vllmNS)
		}

		err = TestEnv.Client.Create(TestEnv.Ctx, obj)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create resource from %s: %w", yamlPath, err)
		}
	}

	return nil
}

func deployVLLMServer(t *testing.T) error {
	t.Helper()

	// Get the REST config to detect OpenShift
	cfg, err := getRestConfig()
	if err != nil {
		return fmt.Errorf("failed to get REST config: %w", err)
	}

	// Detect if this is an OpenShift cluster
	isOpenShift, err := isOpenShiftCluster(cfg)
	if err != nil {
		t.Logf("Warning: failed to detect OpenShift, falling back to Kubernetes: %v", err)
		isOpenShift = false
	}

	// If this is OpenShift, apply the prerequisites first
	if isOpenShift {
		t.Logf("OpenShift cluster detected, applying prerequisites from %s", vllmOpenShiftPrerequisitesPath)
		err = applyYAMLFile(t, vllmOpenShiftPrerequisitesPath)
		if err != nil {
			return fmt.Errorf("failed to apply OpenShift prerequisites: %w", err)
		}
	} else {
		t.Logf("Kubernetes cluster detected")
	}

	// Always use the Kubernetes vLLM configuration file
	t.Logf("Applying vLLM configuration from %s", vllmKubernetesConfigPath)
	err = applyYAMLFile(t, vllmKubernetesConfigPath)
	if err != nil {
		return fmt.Errorf("failed to apply vLLM config: %w", err)
	}

	return nil
}

// isNamespaceScoped returns true if the given resource is namespace-scoped.
func isNamespaceScoped(obj client.Object) bool {
	kind := obj.GetObjectKind().GroupVersionKind().Kind

	// List of cluster-scoped resources that don't need a namespace
	clusterScopedResources := map[string]bool{
		"SecurityContextConstraints": true,
		"ClusterRole":                true,
		"ClusterRoleBinding":         true,
		"CustomResourceDefinition":   true,
		"PersistentVolume":           true,
		"StorageClass":               true,
		"Namespace":                  true,
		"Node":                       true,
	}

	return !clusterScopedResources[kind]
}

func deployLlamaStackWithCABundle(t *testing.T) error {
	t.Helper()

	// Read LlamaStack TLS test configuration
	llamaStackConfigData, err := os.ReadFile(llamaStackTLSTestConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read LlamaStack config: %w", err)
	}

	// Apply LlamaStack configuration
	objects, err := parseKubernetesYAML(llamaStackConfigData)
	if err != nil {
		return fmt.Errorf("failed to parse LlamaStack config: %w", err)
	}

	for _, obj := range objects {
		err = TestEnv.Client.Create(TestEnv.Ctx, obj)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create LlamaStack resource: %w", err)
		}
	}

	return nil
}

func waitForVLLMHealth(t *testing.T) error {
	t.Helper()

	return wait.PollUntilContextTimeout(TestEnv.Ctx, 30*time.Second, vllmTestTimeout, true, func(ctx context.Context) (bool, error) {
		// Port forward to vLLM service for health check
		// This is a simplified check - in real implementation, you'd use port forwarding
		// For now, we'll check if the pod is running and ready
		podList := &corev1.PodList{}
		err := TestEnv.Client.List(ctx, podList, client.InNamespace(vllmNS), client.MatchingLabels{"app.kubernetes.io/name": "vllm"})
		if err != nil {
			return false, err
		}

		if len(podList.Items) == 0 {
			return false, nil
		}

		pod := podList.Items[0]
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})
}

func validateVLLMProviderStatus(t *testing.T) error {
	t.Helper()

	distribution := &v1alpha1.LlamaStackDistribution{}
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: llsTestNS,
		Name:      "vllm-tls-test",
	}, distribution)
	if err != nil {
		return fmt.Errorf("failed to get LlamaStack distribution: %w", err)
	}

	// Check if vLLM provider is available and healthy
	vllmProviderFound := false
	for _, provider := range distribution.Status.DistributionConfig.Providers {
		if provider.ProviderID == "vllm" {
			vllmProviderFound = true
			if provider.Health.Status != "OK" {
				return fmt.Errorf("failed to reach a healthy vLLM provider: %s - %s", provider.Health.Status, provider.Health.Message)
			}
			break
		}
	}

	if !vllmProviderFound {
		return errors.New("failed to find vLLM provider in distribution status")
	}

	return nil
}

func parseKubernetesYAML(data []byte) ([]client.Object, error) {
	// Split YAML documents
	docs := yamlSplit(data)

	// Pre-allocate slice with expected capacity
	objects := make([]client.Object, 0, len(docs))

	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}

		obj := &unstructured.Unstructured{}
		err := yaml.Unmarshal(doc, obj)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
		}

		if obj.GetKind() == "" {
			continue
		}

		objects = append(objects, obj)
	}

	return objects, nil
}

func yamlSplit(data []byte) [][]byte {
	var docs [][]byte
	var currentDoc []byte

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if len(currentDoc) > 0 {
				docs = append(docs, currentDoc)
				currentDoc = nil
			}
		} else {
			currentDoc = append(currentDoc, []byte(line+"\n")...)
		}
	}

	if len(currentDoc) > 0 {
		docs = append(docs, currentDoc)
	}

	return docs
}

func debugLlamaStackDeployment(t *testing.T) error {
	t.Helper()

	// Get deployment details
	deployment := &appsv1.Deployment{}
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: llsTestNS,
		Name:      "vllm-tls-test",
	}, deployment)
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	t.Logf("Deployment status: Replicas=%d, ReadyReplicas=%d, AvailableReplicas=%d",
		deployment.Status.Replicas, deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas)

	// Get pods
	podList := &corev1.PodList{}
	err = TestEnv.Client.List(TestEnv.Ctx, podList, client.InNamespace(llsTestNS), client.MatchingLabels{"app": "vllm-tls-test"})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range podList.Items {
		debugPodDetails(t, &pod)
	}

	return nil
}

func debugPodDetails(t *testing.T, pod *corev1.Pod) {
	t.Helper()

	t.Logf("Pod %s status: Phase=%s, Ready=%t", pod.Name, pod.Status.Phase, isPodReady(pod))

	debugContainerStatuses(t, pod.Status.ContainerStatuses)
	debugVolumeMounts(t, pod.Spec.Containers)
	debugPodVolumes(t, pod.Spec.Volumes)
}

func debugContainerStatuses(t *testing.T, containerStatuses []corev1.ContainerStatus) {
	t.Helper()

	// Log container statuses
	for _, containerStatus := range containerStatuses {
		t.Logf("  Container %s: Ready=%t, RestartCount=%d",
			containerStatus.Name, containerStatus.Ready, containerStatus.RestartCount)

		if containerStatus.State.Waiting != nil {
			t.Logf("    Waiting: %s - %s", containerStatus.State.Waiting.Reason, containerStatus.State.Waiting.Message)
		}
		if containerStatus.State.Terminated != nil {
			t.Logf("    Terminated: %s - %s", containerStatus.State.Terminated.Reason, containerStatus.State.Terminated.Message)
		}
	}
}

func debugVolumeMounts(t *testing.T, containers []corev1.Container) {
	t.Helper()

	// Log volume mounts to verify CA bundle is mounted
	for _, container := range containers {
		t.Logf("  Container %s volume mounts:", container.Name)
		for _, mount := range container.VolumeMounts {
			t.Logf("    %s -> %s", mount.Name, mount.MountPath)
		}
	}
}

func debugPodVolumes(t *testing.T, volumes []corev1.Volume) {
	t.Helper()

	// Log volumes to verify CA bundle volume is present
	t.Logf("  Pod volumes:")
	for _, volume := range volumes {
		t.Logf("    %s", volume.Name)
		if volume.ConfigMap != nil {
			t.Logf("      ConfigMap: %s", volume.ConfigMap.Name)
		}
	}
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func checkNetworkConnectivity(t *testing.T) error {
	t.Helper()

	// Check if vLLM service is accessible from the llama-stack-test namespace
	service := &corev1.Service{}
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: "vllm-dist",
		Name:      "vllm-server",
	}, service)
	if err != nil {
		return fmt.Errorf("failed to get vLLM service: %w", err)
	}

	t.Logf("vLLM service found: %s.%s.svc.cluster.local", service.Name, service.Namespace)

	// Check if vLLM pods are running
	podList := &corev1.PodList{}
	err = TestEnv.Client.List(TestEnv.Ctx, podList, client.InNamespace("vllm-dist"), client.MatchingLabels{"app.kubernetes.io/name": "vllm"})
	if err != nil {
		return fmt.Errorf("failed to list vLLM pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return errors.New("no vLLM pods found")
	}

	for _, pod := range podList.Items {
		t.Logf("vLLM pod %s status: Phase=%s, Ready=%t", pod.Name, pod.Status.Phase, isPodReady(&pod))
	}

	return nil
}

func waitForLlamaStackHealthWithTimeout(t *testing.T, timeout time.Duration) error {
	t.Helper()

	return wait.PollUntilContextTimeout(TestEnv.Ctx, 30*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		distribution := &v1alpha1.LlamaStackDistribution{}
		err := TestEnv.Client.Get(ctx, client.ObjectKey{
			Namespace: llsTestNS,
			Name:      "vllm-tls-test",
		}, distribution)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				t.Logf("LlamaStack distribution not found yet, continuing to wait...")
				return false, nil
			}
			t.Logf("Error getting LlamaStack distribution: %v", err)
			return false, err
		}

		t.Logf("LlamaStack distribution status: Phase=%s", distribution.Status.Phase)

		// Check if distribution is ready
		if distribution.Status.Phase == v1alpha1.LlamaStackDistributionPhaseReady {
			return true, nil
		}

		// Log current phase for debugging
		t.Logf("LlamaStack distribution not ready yet, current phase: %s", distribution.Status.Phase)

		return false, nil
	})
}
