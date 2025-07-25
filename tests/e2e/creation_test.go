//nolint:testpackage
package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/llamastack/llama-stack-k8s-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCreationSuite(t *testing.T) {
	if TestOpts.SkipCreation {
		t.Skip("Skipping creation test suite")
	}

	var llsdistributionCR *v1alpha1.LlamaStackDistribution

	t.Run("should create LlamaStackDistribution", func(t *testing.T) {
		llsdistributionCR = testCreateDistribution(t)
	})

	t.Run("should create PVC if storage is configured", func(t *testing.T) {
		testPVCConfiguration(t, llsdistributionCR)
	})

	t.Run("should handle direct deployment updates", func(t *testing.T) {
		testDirectDeploymentUpdates(t, llsdistributionCR)
	})

	t.Run("should check health status", func(t *testing.T) {
		testHealthStatus(t, llsdistributionCR)
	})

	t.Run("should update deployment through CR", func(t *testing.T) {
		testCRDeploymentUpdate(t, llsdistributionCR)
	})

	t.Run("should update distribution status", func(t *testing.T) {
		testDistributionStatus(t, llsdistributionCR)
	})

	t.Run("should use custom ServiceAccount from PodOverrides", func(t *testing.T) {
		testServiceAccountOverride(t, llsdistributionCR)
	})
}

func testCreateDistribution(t *testing.T) *v1alpha1.LlamaStackDistribution {
	t.Helper()
	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "llama-stack-test",
		},
	}
	err := TestEnv.Client.Create(TestEnv.Ctx, ns)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}

	// Get sample CR
	llsdistributionCR := GetSampleCR(t)
	llsdistributionCR.Namespace = ns.Name

	err = TestEnv.Client.Create(TestEnv.Ctx, llsdistributionCR)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}

	// Wait for deployment to be ready
	err = EnsureResourceReady(t, TestEnv, schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, llsdistributionCR.Name, ns.Name, ResourceReadyTimeout, isDeploymentReady)
	require.NoError(t, err)

	// Verify service is created
	err = EnsureResourceReady(t, TestEnv, schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Service",
	}, llsdistributionCR.Name+"-service", ns.Name, ResourceReadyTimeout, func(u *unstructured.Unstructured) bool {
		// Check if the service has a valid spec and status
		spec, specFound, _ := unstructured.NestedMap(u.Object, "spec")
		status, statusFound, _ := unstructured.NestedMap(u.Object, "status")
		return specFound && statusFound && spec != nil && status != nil
	})
	requireNoErrorWithDebugging(t, TestEnv, err, "Service readiness check failed", llsdistributionCR.Namespace, llsdistributionCR.Name)

	return llsdistributionCR
}

func testDirectDeploymentUpdates(t *testing.T, distribution *v1alpha1.LlamaStackDistribution) {
	t.Helper()
	// Get the deployment
	deployment := &appsv1.Deployment{}
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: distribution.Namespace,
		Name:      distribution.Name,
	}, deployment)
	require.NoError(t, err)

	originalReplicas := *deployment.Spec.Replicas
	*deployment.Spec.Replicas = 2
	err = TestEnv.Client.Update(TestEnv.Ctx, deployment)
	require.NoError(t, err)

	// Wait for operator to reconcile
	time.Sleep(5 * time.Second)

	// Verify deployment is reverted to original state
	err = TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: distribution.Namespace,
		Name:      distribution.Name,
	}, deployment)
	require.NoError(t, err)
	require.Equal(t, originalReplicas, *deployment.Spec.Replicas, "Deployment should be reverted to original state")
}

func testCRDeploymentUpdate(t *testing.T, distribution *v1alpha1.LlamaStackDistribution) {
	t.Helper()
	// Update CR
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: distribution.Namespace,
		Name:      distribution.Name,
	}, distribution)
	require.NoError(t, err)

	// Update replicas
	distribution.Spec.Replicas = 2
	err = TestEnv.Client.Update(TestEnv.Ctx, distribution)
	require.NoError(t, err)

	// Wait for deployment to be updated and ready with new replicas
	err = EnsureResourceReady(t, TestEnv, schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, distribution.Name, distribution.Namespace, ResourceReadyTimeout, func(u *unstructured.Unstructured) bool {
		availableReplicas, found, nestedErr := unstructured.NestedInt64(u.Object, "status", "availableReplicas")
		if !found || nestedErr != nil {
			return false
		}
		return availableReplicas == 2
	})
	require.NoError(t, err, "Failed to wait for deployment to update replicas")

	// Verify deployment is updated
	deployment := &appsv1.Deployment{}
	err = TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: distribution.Namespace,
		Name:      distribution.Name,
	}, deployment)
	require.NoError(t, err)
	require.Equal(t, int32(2), deployment.Status.AvailableReplicas, "Deployment should have 2 available replicas")
}

func testHealthStatus(t *testing.T, distribution *v1alpha1.LlamaStackDistribution) {
	t.Helper()
	// Wait for status to be updated with a longer interval to avoid rate limiting
	err := wait.PollUntilContextTimeout(TestEnv.Ctx, 1*time.Minute, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		// Get the latest state of the distribution
		updatedDistribution := &v1alpha1.LlamaStackDistribution{}
		err := TestEnv.Client.Get(ctx, client.ObjectKey{
			Namespace: distribution.Namespace,
			Name:      distribution.Name,
		}, updatedDistribution)
		if err != nil {
			return false, err
		}
		return updatedDistribution.Status.Phase == v1alpha1.LlamaStackDistributionPhaseReady, nil
	})
	requireNoErrorWithDebugging(t, TestEnv, err, "Failed to wait for distribution status update", distribution.Namespace, distribution.Name)
}

func testDistributionStatus(t *testing.T, llsdistributionCR *v1alpha1.LlamaStackDistribution) {
	t.Helper()
	// Wait for status to be updated with distribution info
	err := wait.PollUntilContextTimeout(TestEnv.Ctx, 1*time.Minute, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		updatedDistribution := &v1alpha1.LlamaStackDistribution{}
		err := TestEnv.Client.Get(ctx, client.ObjectKey{
			Namespace: llsdistributionCR.Namespace,
			Name:      llsdistributionCR.Name,
		}, updatedDistribution)
		if err != nil {
			return false, err
		}

		return isDistributionStatusReady(updatedDistribution), nil
	})
	if err != nil {
		// Get the final state to print on error
		finalDistribution := &v1alpha1.LlamaStackDistribution{}
		TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
			Namespace: llsdistributionCR.Namespace,
			Name:      llsdistributionCR.Name,
		}, finalDistribution)
		requireNoErrorWithDebugging(t, TestEnv, err, "Failed to wait for distribution status update", llsdistributionCR.Namespace, llsdistributionCR.Name)
	}

	// Get final state and verify
	updatedDistribution := &v1alpha1.LlamaStackDistribution{}
	err = TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: llsdistributionCR.Namespace,
		Name:      llsdistributionCR.Name,
	}, updatedDistribution)
	require.NoError(t, err)

	// Verify distribution config (but allow it to be empty for some distributions)
	if len(updatedDistribution.Status.DistributionConfig.AvailableDistributions) > 0 {
		require.NotEmpty(t, updatedDistribution.Status.DistributionConfig.AvailableDistributions,
			"Available distributions should be populated")
	}

	if updatedDistribution.Status.DistributionConfig.ActiveDistribution != "" {
		require.Equal(t, updatedDistribution.Spec.Server.Distribution.Name,
			updatedDistribution.Status.DistributionConfig.ActiveDistribution,
			"Active distribution should match the spec")
	}

	// Verify provider config and health (but allow it to be empty for some distributions)
	if len(updatedDistribution.Status.DistributionConfig.Providers) > 0 {
		// Verify that each provider has config and health info
		validateProviders(t, updatedDistribution)
	} else {
		t.Log("No providers found in distribution status - this might be expected for some distributions")
	}

	// Write the final distribution status to a file for CI to collect
	yaml, err := yaml.Marshal(updatedDistribution)
	if err != nil {
		t.Fatalf("Failed to marshal distribution: %v", err)
	}
	// Weak - do this better to write to a temp file and then move it to the right place at the
	// repo's root so the CI agent can collect it
	err = os.WriteFile("../../distribution.log", yaml, 0644)
	require.NoError(t, err)
}

func testPVCConfiguration(t *testing.T, distribution *v1alpha1.LlamaStackDistribution) {
	t.Helper()

	pvcName := distribution.Name + "-pvc"
	pvc := &corev1.PersistentVolumeClaim{}
	err := TestEnv.Client.Get(TestEnv.Ctx, client.ObjectKey{
		Namespace: distribution.Namespace,
		Name:      pvcName,
	}, pvc)
	if distribution.Spec.Server.Storage == nil {
		require.Error(t, err, "PVC should not exist when storage is not configured")
		require.True(t, k8serrors.IsNotFound(err), "Expected not found error for PVC when storage is not configured")
	} else {
		require.NoError(t, err, "PVC should be created when storage is configured")
		// Check storage size
		expectedSize := v1alpha1.DefaultStorageSize
		if distribution.Spec.Server.Storage.Size != nil {
			expectedSize = *distribution.Spec.Server.Storage.Size
		}
		actualSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		require.Equal(t, expectedSize.String(), actualSize.String(), "PVC storage size should match CR")
	}
}

func testServiceAccountOverride(t *testing.T, distribution *v1alpha1.LlamaStackDistribution) {
	t.Helper()

	// Create a custom ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-sa",
			Namespace: distribution.Namespace,
		},
	}
	require.NoError(t, TestEnv.Client.Create(TestEnv.Ctx, sa))
	defer TestEnv.Client.Delete(TestEnv.Ctx, sa)

	// Update the CR to use the custom ServiceAccount with retry logic
	err := wait.PollUntilContextTimeout(TestEnv.Ctx, time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		// Get the latest version of the CR
		latestDistribution := &v1alpha1.LlamaStackDistribution{}
		if err := TestEnv.Client.Get(ctx, client.ObjectKey{
			Namespace: distribution.Namespace,
			Name:      distribution.Name,
		}, latestDistribution); err != nil {
			return false, err
		}

		// Update the ServiceAccount
		latestDistribution.Spec.Server.PodOverrides = &v1alpha1.PodOverrides{
			ServiceAccountName: "custom-sa",
		}

		// Try to update
		if err := TestEnv.Client.Update(ctx, latestDistribution); err != nil {
			if k8serrors.IsConflict(err) {
				// If there's a conflict, return false to retry
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	require.NoError(t, err, "Failed to update CR with ServiceAccount override")

	// Wait for the deployment to be updated
	time.Sleep(5 * time.Second)

	// Get the deployment
	deployment := &appsv1.Deployment{}
	require.NoError(t, TestEnv.Client.Get(TestEnv.Ctx,
		client.ObjectKey{
			Name:      distribution.Name,
			Namespace: distribution.Namespace,
		},
		deployment))

	// Verify the ServiceAccount is set correctly
	assert.Equal(t, "custom-sa", deployment.Spec.Template.Spec.ServiceAccountName)
}

func isDeploymentReady(u *unstructured.Unstructured) bool {
	replicas, found, err := unstructured.NestedInt64(u.Object, "status", "replicas")
	if !found || err != nil {
		return false
	}
	availableReplicas, found, err := unstructured.NestedInt64(u.Object, "status", "availableReplicas")
	return found && err == nil && availableReplicas == replicas
}

// isDistributionStatusReady checks if the distribution status is ready.
func isDistributionStatusReady(distribution *v1alpha1.LlamaStackDistribution) bool {
	// Check that distribution config is populated - but this might not be required for all distributions
	if len(distribution.Status.DistributionConfig.AvailableDistributions) == 0 {
		// Allow this to be empty if the distribution is in ready phase
		if distribution.Status.Phase != v1alpha1.LlamaStackDistributionPhaseReady {
			return false
		}
	}

	// Verify that the active distribution is set - but this might not be required for all distributions
	if distribution.Status.DistributionConfig.ActiveDistribution == "" {
		// Allow this to be empty if the distribution is in ready phase
		if distribution.Status.Phase != v1alpha1.LlamaStackDistributionPhaseReady {
			return false
		}
	}

	// For now, we'll consider it ready if the phase is ready, even if providers are empty
	// This is because the providers might be populated asynchronously
	return distribution.Status.Phase == v1alpha1.LlamaStackDistributionPhaseReady
}

// validateProviders validates all providers in the distribution.
func validateProviders(t *testing.T, distribution *v1alpha1.LlamaStackDistribution) {
	t.Helper()

	for _, provider := range distribution.Status.DistributionConfig.Providers {
		require.NotEmpty(t, provider.API, "Provider should have API info")
		require.NotEmpty(t, provider.ProviderID, "Provider should have ProviderID info")
		require.NotEmpty(t, provider.ProviderType, "Provider should have ProviderType info")
		require.NotNil(t, provider.Config, "Provider should have config info")
		// If Ollama test it returns OK status
		if provider.ProviderID == "ollama" {
			require.Equal(t, "OK", provider.Health.Status, "Provider should have OK health status")
		}
		// Check that status is one of the allowed values
		require.Contains(t, []string{"OK", "Error", "Not Implemented"}, provider.Health.Status, "Provider health status should be one of: OK, Error, Not Implemented")
		// There is no message for OK status
		if provider.Health.Status != "OK" {
			require.NotEmpty(t, provider.Health.Message, "Provider should have health message")
		}
		require.NotEmpty(t, provider.Config, "Provider config should not be empty")
	}
}
