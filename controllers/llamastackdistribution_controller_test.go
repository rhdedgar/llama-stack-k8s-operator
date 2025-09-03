package controllers_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	llamav1alpha1 "github.com/llamastack/llama-stack-k8s-operator/api/v1alpha1"
	"github.com/llamastack/llama-stack-k8s-operator/controllers"
	"github.com/llamastack/llama-stack-k8s-operator/pkg/cluster"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// testenvNamespaceCounter is used to generate unique namespace names for test isolation.
var testenvNamespaceCounter int

func TestStorageConfiguration(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	tests := []struct {
		name           string
		buildInstance  func(namespace string) *llamav1alpha1.LlamaStackDistribution
		expectedVolume corev1.Volume
		expectedMount  corev1.VolumeMount
	}{
		{
			name: "No storage configuration - should use emptyDir",
			buildInstance: func(namespace string) *llamav1alpha1.LlamaStackDistribution {
				return NewDistributionBuilder().
					WithName("test").
					WithNamespace(namespace).
					WithStorage(nil).
					Build()
			},
			expectedVolume: corev1.Volume{
				Name: "lls-storage",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
			expectedMount: corev1.VolumeMount{
				Name:      "lls-storage",
				MountPath: llamav1alpha1.DefaultMountPath,
			},
		},
		{
			name: "Storage with default values",
			buildInstance: func(namespace string) *llamav1alpha1.LlamaStackDistribution {
				return NewDistributionBuilder().
					WithName("test").
					WithNamespace(namespace).
					WithStorage(DefaultTestStorage()).
					Build()
			},
			expectedVolume: corev1.Volume{
				Name: "lls-storage",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "test-pvc",
					},
				},
			},
			expectedMount: corev1.VolumeMount{
				Name:      "lls-storage",
				MountPath: llamav1alpha1.DefaultMountPath,
			},
		},
		{
			name: "Storage with custom values",
			buildInstance: func(namespace string) *llamav1alpha1.LlamaStackDistribution {
				return NewDistributionBuilder().
					WithName("test").
					WithNamespace(namespace).
					WithStorage(CustomTestStorage("20Gi", "/custom/path")).
					Build()
			},
			expectedVolume: corev1.Volume{
				Name: "lls-storage",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "test-pvc",
					},
				},
			},
			expectedMount: corev1.VolumeMount{
				Name:      "lls-storage",
				MountPath: "/custom/path",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace := createTestNamespace(t, "test-storage")

			// arrange
			instance := tt.buildInstance(namespace.Name)
			require.NoError(t, k8sClient.Create(t.Context(), instance))
			t.Cleanup(func() {
				if err := k8sClient.Delete(t.Context(), instance); err != nil && !apierrors.IsNotFound(err) {
					t.Logf("Failed to delete LlamaStackDistribution instance %s/%s: %v", instance.Namespace, instance.Name, err)
				}
			})

			// act: reconcile the instance
			ReconcileDistribution(t, instance, false)

			// assert
			deployment := &appsv1.Deployment{}
			waitForResource(t, k8sClient, instance.Namespace, instance.Name, deployment)

			if tt.expectedVolume.EmptyDir != nil {
				AssertDeploymentUsesEmptyDirStorage(t, deployment)
			} else if tt.expectedVolume.PersistentVolumeClaim != nil {
				AssertDeploymentUsesPVCStorage(t, deployment, tt.expectedVolume.PersistentVolumeClaim.ClaimName)
			}

			AssertDeploymentHasVolumeMount(t, deployment, tt.expectedMount.MountPath)

			// verify PVC is created when storage is configured
			if instance.Spec.Server.Storage != nil {
				expectedPVCName := tt.expectedVolume.PersistentVolumeClaim.ClaimName
				pvc := AssertPVCExists(t, k8sClient, instance.Namespace, expectedPVCName)
				expectedSize := instance.Spec.Server.Storage.Size
				if expectedSize == nil {
					AssertPVCHasSize(t, pvc, llamav1alpha1.DefaultStorageSize.String())
				} else {
					AssertPVCHasSize(t, pvc, expectedSize.String())
				}
			}
		})
	}
}

func TestCustomConfigFunctionality(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// Create a test namespace
	namespace := createTestNamespace(t, "test-custom-config")

	customConfig := `version: '2'
image_name: ollama
apis:
- inference
providers:
  inference:
  - provider_id: ollama
    provider_type: "remote::ollama"
    config:
      url: "http://ollama-server:11434"
models:
  - model_id: "llama3.2:1b"
    provider_id: ollama
    model_type: llm
server:
  port: 8321`

	// Create a LlamaStackDistribution with inline CustomConfig
	instance := NewDistributionBuilder().
		WithName("test-custom-config").
		WithNamespace(namespace.Name).
		WithUserConfig(customConfig).
		Build()
	require.NoError(t, k8sClient.Create(t.Context(), instance))

	// Reconcile to create initial deployment and ConfigMap
	ReconcileDistribution(t, instance, false)

	// Verify that the combined ConfigMap was created with the CustomConfig data
	configMap := &corev1.ConfigMap{}
	configMapKey := types.NamespacedName{Name: instance.Name + "-config", Namespace: instance.Namespace}
	waitForResourceWithKey(t, k8sClient, configMapKey, configMap)

	require.Equal(t, customConfig, configMap.Data["run.yaml"], "Operator-managed ConfigMap should contain the CustomConfig data")

	// Verify that the deployment mounts the ConfigMap
	deployment := &appsv1.Deployment{}
	deploymentKey := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
	waitForResourceWithKey(t, k8sClient, deploymentKey, deployment)

	// Check that the deployment has the combined-config volume
	combinedConfigVolumeFound := false
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == controllers.CombinedConfigVolumeName {
			combinedConfigVolumeFound = true
			require.NotNil(t, volume.ConfigMap, "combined-config volume should be a ConfigMap volume")
			require.Equal(t, configMap.Name, volume.ConfigMap.Name, "combined-config volume should reference the created ConfigMap")
			break
		}
	}
	require.True(t, combinedConfigVolumeFound, "deployment should have combined-config volume")

	// Check that the container has the combined-config volume mount for run.yaml
	require.NotEmpty(t, deployment.Spec.Template.Spec.Containers, "deployment should have containers")
	container := deployment.Spec.Template.Spec.Containers[0]

	userConfigMountFound := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == controllers.CombinedConfigVolumeName && mount.SubPath == "run.yaml" {
			userConfigMountFound = true
			require.Equal(t, "/etc/llama-stack/run.yaml", mount.MountPath, "run.yaml should be mounted at /etc/llama-stack/run.yaml")
			require.True(t, mount.ReadOnly, "run.yaml mount should be read-only")
			break
		}
	}
	require.True(t, userConfigMountFound, "container should have combined-config volume mount for run.yaml")

	// Verify that the container command is configured to use the custom config with the startup script
	require.Equal(t, []string{"/bin/sh", "-c", controllers.StartupScript}, container.Command, "container command should be set for custom config")
	require.Empty(t, container.Args, "container args should be empty when using startup script")

	// Test updating the CustomConfig
	updatedConfig := customConfig + "\n# Updated configuration"

	// Refresh the instance to avoid conflicts
	require.NoError(t, k8sClient.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, instance))
	instance.Spec.Server.UserConfig.CustomConfig = updatedConfig
	require.NoError(t, k8sClient.Update(t.Context(), instance))

	// Reconcile again to update the ConfigMap
	ReconcileDistribution(t, instance, false)

	// Verify the ConfigMap was updated
	require.NoError(t, k8sClient.Get(t.Context(), configMapKey, configMap))
	require.Equal(t, updatedConfig, configMap.Data["run.yaml"], "ConfigMap should be updated with new CustomConfig data")
}

func TestReconcile(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// --- arrange ---
	instanceName := "llamastackdistribution-sample"
	instancePort := llamav1alpha1.DefaultServerPort
	expectedSelector := map[string]string{
		llamav1alpha1.DefaultLabelKey: llamav1alpha1.DefaultLabelValue,
		"app.kubernetes.io/instance":  instanceName,
	}
	expectedPort := corev1.ServicePort{
		Name:       llamav1alpha1.DefaultServicePortName,
		Port:       instancePort,
		TargetPort: intstr.FromInt(int(instancePort)),
		Protocol:   corev1.ProtocolTCP,
	}
	operatorNamespaceName := "test-operator-namespace"

	// set operator namespace to avoid service account file dependency
	t.Setenv("OPERATOR_NAMESPACE", operatorNamespaceName)

	namespace := createTestNamespace(t, operatorNamespaceName)
	instance := NewDistributionBuilder().
		WithName(instanceName).
		WithNamespace(namespace.Name).
		WithDistribution("starter").
		WithPort(instancePort).
		Build()
	require.NoError(t, k8sClient.Create(t.Context(), instance))

	// --- act ---
	ReconcileDistribution(t, instance, true)

	service := &corev1.Service{}
	waitForResource(t, k8sClient, instance.Namespace, instance.Name+"-service", service)
	deployment := &appsv1.Deployment{}
	waitForResource(t, k8sClient, instance.Namespace, instance.Name, deployment)
	networkpolicy := &networkingv1.NetworkPolicy{}
	waitForResource(t, k8sClient, instance.Namespace, instance.Name+"-network-policy",
		networkpolicy)
	serviceAccount := &corev1.ServiceAccount{}
	waitForResource(t, k8sClient, instance.Namespace, instance.Name+"-sa",
		serviceAccount)

	// --- assert ---
	// Service behaviors
	AssertServicePortMatches(t, service, expectedPort)
	AssertServiceAndDeploymentPortsAlign(t, service, deployment)
	AssertServiceSelectorMatches(t, service, expectedSelector)
	AssertServiceAndDeploymentSelectorsAlign(t, service, deployment)

	// ServiceAccount behaviors
	AssertServiceAccountDeploymentAlign(t, deployment, serviceAccount)

	// NetworkPolicy behaviors
	AssertNetworkPolicyTargetsDeploymentPods(t, networkpolicy, deployment)
	AssertNetworkPolicyAllowsDeploymentPort(t, networkpolicy, deployment, operatorNamespaceName)
	AssertNetworkPolicyIsIngressOnly(t, networkpolicy)

	// Resource ownership behaviors
	AssertResourceOwnedByInstance(t, service, instance)
	AssertResourceOwnedByInstance(t, deployment, instance)
	AssertResourceOwnedByInstance(t, networkpolicy, instance)
	AssertResourceOwnedByInstance(t, serviceAccount, instance)
}

// Define a custom roundtripper type for testing.
type mockRoundTripper struct {
	RoundTripFunc func(req *http.Request) (*http.Response, error)
}

// RoundTrip satisfies the http.RoundTripper interface and calls the mock function.
func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.RoundTripFunc(req)
}

// newMockAPIResponse is a test helper that takes any data structure,
// marshals it to JSON, and returns a complete http response.
func newMockAPIResponse(t *testing.T, data any) *http.Response {
	t.Helper()
	jsonBytes, err := json.Marshal(data)
	require.NoError(t, err)

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(jsonBytes))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestLlamaStackProviderAndVersionInfo(t *testing.T) {
	// arrange
	enableNetworkPolicy := false
	expectedLlamaStackVersionInfo := "v-test"
	expectedProviderID := "mock-ollama"

	// define the data structure for the mock providers response
	providerData := struct {
		Data []llamav1alpha1.ProviderInfo `json:"data"`
	}{
		Data: []llamav1alpha1.ProviderInfo{
			{
				ProviderID:   expectedProviderID,
				ProviderType: "remote::ollama",
				API:          "inference",
				Health:       llamav1alpha1.ProviderHealthStatus{Status: "OK", Message: ""},
				Config:       apiextensionsv1.JSON{Raw: []byte(`{"url": "http://mock.server"}`)},
			},
		},
	}

	// define the data structure for the mock version response
	versionData := struct {
		Version string `json:"version"`
	}{
		Version: expectedLlamaStackVersionInfo,
	}

	// create the mock http client that uses our custom roundtripper
	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			// simulate the RoundTrip logic to handle different API paths
			RoundTripFunc: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/v1/providers" {
					return newMockAPIResponse(t, providerData), nil
				}
				if req.URL.Path == "/v1/version" {
					return newMockAPIResponse(t, versionData), nil
				}
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
				}, nil
			},
		},
	}

	namespace := createTestNamespace(t, "test-status")
	instance := NewDistributionBuilder().
		WithName("test-status-instance").
		WithNamespace(namespace.Name).
		Build()
	require.NoError(t, k8sClient.Create(t.Context(), instance))

	testClusterInfo := &cluster.ClusterInfo{
		DistributionImages: map[string]string{
			"starter": "docker.io/llamastack/distribution-starter:latest",
		},
	}

	reconciler := controllers.NewTestReconciler(
		k8sClient,
		scheme.Scheme,
		testClusterInfo,
		mockClient,
		enableNetworkPolicy,
	)

	// act (part 1)
	// run the first reconciliation to create the initial resources like the deployment
	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace},
	})
	require.NoError(t, err)

	// manually update the deployment's status because envtest doesn't run a real deployment controller
	// this forces the reconciler to proceed to the health check logic on its next run
	deployment := &appsv1.Deployment{}
	deploymentKey := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
	waitForResourceWithKey(t, k8sClient, deploymentKey, deployment)

	deployment.Status.ReadyReplicas = 1
	deployment.Status.Replicas = 1
	require.NoError(t, k8sClient.Status().Update(t.Context(), deployment))

	// act (part 2)
	// run the second reconciliation to trigger the status update logic
	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace},
	})
	require.NoError(t, err)

	// assert
	updatedInstance := &llamav1alpha1.LlamaStackDistribution{}
	waitForResource(t, k8sClient, namespace.Name, instance.Name, updatedInstance)

	// validate provider info
	require.Len(t, updatedInstance.Status.DistributionConfig.Providers, 1, "should find exactly one provider from the mock server")
	actualProvider := updatedInstance.Status.DistributionConfig.Providers[0]
	require.Equal(t, expectedProviderID, actualProvider.ProviderID, "provider ID should match the mock response")
	require.Equal(t, "OK", actualProvider.Health.Status, "provider health should match the mock response")
	require.NotEmpty(t, actualProvider.Config, "provider config should be populated")
	// validate llama stack version
	require.Equal(t, expectedLlamaStackVersionInfo,
		updatedInstance.Status.Version.LlamaStackServerVersion,
		"server version should match the mock response")
}

func TestNetworkPolicyConfiguration(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	tests := []struct {
		name  string
		setup func(t *testing.T, instance *llamav1alpha1.LlamaStackDistribution)
	}{
		{
			name: "enabled then disabled deletes NetworkPolicy",
			setup: func(t *testing.T, instance *llamav1alpha1.LlamaStackDistribution) {
				t.Helper()
				// ensure NetworkPolicy exists by reconciling with feature enabled.
				ReconcileDistribution(t, instance, true)
				waitForResource(t, k8sClient, instance.Namespace, instance.Name+"-network-policy", &networkingv1.NetworkPolicy{})
			},
		},
		{
			name: "disabled from start leaves NetworkPolicy absent",
			setup: func(t *testing.T, instance *llamav1alpha1.LlamaStackDistribution) {
				// no setup needed - NetworkPolicy doesn't exist
				t.Helper()
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// --- arrange ---
			operatorNamespaceName := "test-operator-namespace"
			t.Setenv("OPERATOR_NAMESPACE", operatorNamespaceName)

			namespace := createTestNamespace(t, "test-networkpolicy")
			instance := NewDistributionBuilder().
				WithName(fmt.Sprintf("np-config-%d", i)).
				WithNamespace(namespace.Name).
				WithDistribution("starter").
				Build()
			require.NoError(t, k8sClient.Create(t.Context(), instance))
			t.Cleanup(func() { _ = k8sClient.Delete(t.Context(), instance) })

			// preconditions for this scenario
			tt.setup(t, instance)

			// --- act ---
			ReconcileDistribution(t, instance, false)

			// --- assert ---
			npKey := types.NamespacedName{Name: instance.Name + "-network-policy", Namespace: instance.Namespace}
			AssertNetworkPolicyAbsent(t, k8sClient, npKey)
		})
	}
}
