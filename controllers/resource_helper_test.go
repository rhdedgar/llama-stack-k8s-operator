/*
Copyright 2025.

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

package controllers

import (
	"testing"

	llamav1alpha1 "github.com/llamastack/llama-stack-k8s-operator/api/v1alpha1"
	"github.com/llamastack/llama-stack-k8s-operator/pkg/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildContainerSpec(t *testing.T) {
	testCases := []struct {
		name           string
		instance       *llamav1alpha1.LlamaStackDistribution
		image          string
		expectedResult corev1.Container
	}{
		{
			name: "default values",
			instance: &llamav1alpha1.LlamaStackDistribution{
				Spec: llamav1alpha1.LlamaStackDistributionSpec{
					Server: llamav1alpha1.ServerSpec{
						ContainerSpec: llamav1alpha1.ContainerSpec{},
					},
				},
			},
			image: "test-image:latest",
			expectedResult: corev1.Container{
				Name:  llamav1alpha1.DefaultContainerName,
				Image: "test-image:latest",
				Ports: []corev1.ContainerPort{{ContainerPort: llamav1alpha1.DefaultServerPort}},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "lls-storage",
					MountPath: llamav1alpha1.DefaultMountPath,
				}},
			},
		},
		{
			name: "custom container values",
			instance: &llamav1alpha1.LlamaStackDistribution{
				Spec: llamav1alpha1.LlamaStackDistributionSpec{
					Server: llamav1alpha1.ServerSpec{
						ContainerSpec: llamav1alpha1.ContainerSpec{
							Name: "custom-container",
							Port: 9000,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
							Env: []corev1.EnvVar{
								{Name: "TEST_ENV", Value: "test-value"},
							},
						},
						Storage: &llamav1alpha1.StorageSpec{
							MountPath: "/custom/path",
						},
					},
				},
			},
			image: "test-image:latest",
			expectedResult: corev1.Container{
				Name:  "custom-container",
				Image: "test-image:latest",
				Ports: []corev1.ContainerPort{{ContainerPort: 9000}},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				Env: []corev1.EnvVar{
					{Name: "TEST_ENV", Value: "test-value"},
				},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "lls-storage",
					MountPath: "/custom/path",
				}},
				Command: nil,
			},
		},
		{
			name: "command and args overrides",
			instance: &llamav1alpha1.LlamaStackDistribution{
				Spec: llamav1alpha1.LlamaStackDistributionSpec{
					Server: llamav1alpha1.ServerSpec{
						ContainerSpec: llamav1alpha1.ContainerSpec{
							Command: []string{"/custom/entrypoint.sh"},
							Args:    []string{"--config", "/etc/config.yaml", "--debug"},
						},
					},
				},
			},
			image: "test-image:latest",
			expectedResult: corev1.Container{
				Name:    llamav1alpha1.DefaultContainerName,
				Image:   "test-image:latest",
				Command: []string{"/custom/entrypoint.sh"},
				Args:    []string{"--config", "/etc/config.yaml", "--debug"},
				Ports:   []corev1.ContainerPort{{ContainerPort: llamav1alpha1.DefaultServerPort}},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "lls-storage",
					MountPath: llamav1alpha1.DefaultMountPath,
				}},
			},
		},
		{
			name: "with user config",
			instance: &llamav1alpha1.LlamaStackDistribution{
				Spec: llamav1alpha1.LlamaStackDistributionSpec{
					Server: llamav1alpha1.ServerSpec{
						Distribution: llamav1alpha1.DistributionType{
							Name: "ollama",
						},
						ContainerSpec: llamav1alpha1.ContainerSpec{},
						UserConfig: &llamav1alpha1.UserConfigSpec{
							ConfigMapName: "test-config",
						},
					},
				},
			},
			image: "test-image:latest",
			expectedResult: corev1.Container{
				Name:            llamav1alpha1.DefaultContainerName,
				Image:           "test-image:latest",
				ImagePullPolicy: corev1.PullAlways,
				Ports:           []corev1.ContainerPort{{ContainerPort: llamav1alpha1.DefaultServerPort}},
				Command:         []string{"python", "-m", "llama_stack.distribution.server.server"},
				Args:            []string{"--config", "/etc/llama-stack/config/run.yaml"},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "lls-storage",
						MountPath: llamav1alpha1.DefaultMountPath,
					},
					{
						Name:      "user-config",
						MountPath: "/etc/llama-stack/config",
						ReadOnly:  true,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := buildContainerSpec(tc.instance, tc.image)
			assert.Equal(t, tc.expectedResult.Name, result.Name)
			assert.Equal(t, tc.expectedResult.Image, result.Image)
			assert.Equal(t, tc.expectedResult.Ports, result.Ports)
			assert.Equal(t, tc.expectedResult.Resources, result.Resources)
			assert.Equal(t, tc.expectedResult.Env, result.Env)
			assert.Equal(t, tc.expectedResult.VolumeMounts, result.VolumeMounts)
			assert.Equal(t, tc.expectedResult.Command, result.Command)
			assert.Equal(t, tc.expectedResult.Args, result.Args)
		})
	}
}

func TestConfigurePodStorage(t *testing.T) {
	testCases := []struct {
		name              string
		instance          *llamav1alpha1.LlamaStackDistribution
		container         corev1.Container
		expectedPVCVolume bool
		expectedEmptyDir  bool
		expectedOverrides bool
	}{
		{
			name: "with PVC storage",
			instance: &llamav1alpha1.LlamaStackDistribution{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-instance",
				},
				Spec: llamav1alpha1.LlamaStackDistributionSpec{
					Server: llamav1alpha1.ServerSpec{
						Storage: &llamav1alpha1.StorageSpec{},
					},
				},
			},
			container:         corev1.Container{Name: "test-container"},
			expectedPVCVolume: true,
			expectedEmptyDir:  false,
			expectedOverrides: false,
		},
		{
			name: "with EmptyDir storage",
			instance: &llamav1alpha1.LlamaStackDistribution{
				Spec: llamav1alpha1.LlamaStackDistributionSpec{
					Server: llamav1alpha1.ServerSpec{
						Storage: nil,
					},
				},
			},
			container:         corev1.Container{Name: "test-container"},
			expectedPVCVolume: false,
			expectedEmptyDir:  true,
			expectedOverrides: false,
		},
		{
			name: "with pod overrides",
			instance: &llamav1alpha1.LlamaStackDistribution{
				Spec: llamav1alpha1.LlamaStackDistributionSpec{
					Server: llamav1alpha1.ServerSpec{
						Storage: nil,
						PodOverrides: &llamav1alpha1.PodOverrides{
							Volumes: []corev1.Volume{
								{
									Name: "test-volume",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "test-config",
											},
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "test-volume",
									MountPath: "/test/path",
								},
							},
						},
					},
				},
			},
			container:         corev1.Container{Name: "test-container"},
			expectedPVCVolume: false,
			expectedEmptyDir:  true,
			expectedOverrides: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := configurePodStorage(tc.instance, tc.container)

			// Verify container was added.
			assert.Len(t, result.Containers, 1)

			verifyStorageVolumes(t, result, tc.instance, tc.expectedPVCVolume, tc.expectedEmptyDir)

			if tc.expectedOverrides {
				verifyPodOverrides(t, result)
			}
		})
	}
}

// verifyStorageVolumes validates that the correct storage volumes are configured.
func verifyStorageVolumes(t *testing.T, podSpec corev1.PodSpec, instance *llamav1alpha1.LlamaStackDistribution,
	expectPVC, expectEmptyDir bool) {
	t.Helper()

	if expectPVC {
		pvcFound := false
		for _, vol := range podSpec.Volumes {
			if vol.Name == "lls-storage" && vol.PersistentVolumeClaim != nil {
				pvcFound = true
				assert.Equal(t, instance.Name+"-pvc", vol.PersistentVolumeClaim.ClaimName)
				break
			}
		}
		assert.True(t, pvcFound, "Expected PVC volume not found")
	}

	if expectEmptyDir {
		emptyDirFound := false
		for _, vol := range podSpec.Volumes {
			if vol.Name == "lls-storage" && vol.EmptyDir != nil {
				emptyDirFound = true
				break
			}
		}
		assert.True(t, emptyDirFound, "Expected EmptyDir volume not found")
	}
}

// verifyPodOverrides validates that pod overrides are correctly applied.
func verifyPodOverrides(t *testing.T, podSpec corev1.PodSpec) {
	t.Helper()

	// Check for overridden volume.
	volumeFound := findVolumeByName(podSpec.Volumes, "test-volume")
	assert.True(t, volumeFound, "Expected override volume not found")

	// Check for overridden volume mount.
	mountFound := findVolumeMountByNameAndPath(podSpec.Containers[0].VolumeMounts,
		"test-volume", "/test/path")
	assert.True(t, mountFound, "Expected override volume mount not found")
}

// findVolumeByName checks if a volume with the given name exists.
func findVolumeByName(volumes []corev1.Volume, name string) bool {
	for _, vol := range volumes {
		if vol.Name == name {
			return true
		}
	}
	return false
}

// findVolumeMountByNameAndPath checks if a volume mount with the given name and path exists.
func findVolumeMountByNameAndPath(mounts []corev1.VolumeMount, name, path string) bool {
	for _, mount := range mounts {
		if mount.Name == name && mount.MountPath == path {
			return true
		}
	}
	return false
}

// createLSD creates a LlamaStackDistribution instance with optional name and image.
func createLSD(name, image string) *llamav1alpha1.LlamaStackDistribution {
	return &llamav1alpha1.LlamaStackDistribution{
		Spec: llamav1alpha1.LlamaStackDistributionSpec{
			Server: llamav1alpha1.ServerSpec{
				Distribution: llamav1alpha1.DistributionType{
					Name:  name,
					Image: image,
				},
			},
		},
	}
}

// setupTestClusterInfo creates a ClusterInfo instance for testing with the specified distribution images.
// If no images are provided, it defaults to having "ollama" with "ollama-image:latest".
func setupTestClusterInfo(images map[string]string) *cluster.ClusterInfo {
	if images == nil {
		images = map[string]string{
			"ollama": "ollama-image:latest",
		}
	}
	return &cluster.ClusterInfo{
		OperatorNamespace:  "default",
		DistributionImages: images,
	}
}

func TestResolveImage(t *testing.T) {
	// Setup test cluster info
	clusterInfo := setupTestClusterInfo(map[string]string{
		"ollama": "ollama-image:latest",
	})

	testCases := []struct {
		name          string
		instance      *llamav1alpha1.LlamaStackDistribution
		expectedImage string
		expectError   bool
	}{
		{
			name:          "resolve from name",
			instance:      createLSD("ollama", ""),
			expectedImage: clusterInfo.DistributionImages["ollama"],
			expectError:   false,
		},
		{
			name:          "resolve from image",
			instance:      createLSD("", "test-image:latest"),
			expectedImage: "test-image:latest",
			expectError:   false,
		},
		{
			name:          "invalid distribution name",
			instance:      createLSD("invalid-name", ""),
			expectedImage: "",
			expectError:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &LlamaStackDistributionReconciler{ClusterInfo: clusterInfo}
			image, err := r.resolveImage(tc.instance.Spec.Server.Distribution)
			if tc.expectError {
				require.Error(t, err)
				assert.Empty(t, image)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedImage, image)
			}
		})
	}
}

func TestDistributionValidation(t *testing.T) {
	// Setup test cluster info
	clusterInfo := setupTestClusterInfo(map[string]string{
		"ollama": "lls/lls-ollama:1.0",
	})

	testCases := []struct {
		name        string
		instance    *llamav1alpha1.LlamaStackDistribution
		expectError bool
	}{
		{
			name:        "valid distribution name",
			instance:    createLSD("ollama", ""),
			expectError: false,
		},
		{
			name:        "valid direct image",
			instance:    createLSD("", "test-image:latest"),
			expectError: false,
		},
		{
			name:        "invalid distribution name",
			instance:    createLSD("invalid-name", ""),
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &LlamaStackDistributionReconciler{ClusterInfo: clusterInfo}
			err := r.validateDistribution(tc.instance)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDistributionWithoutClusterInfo(t *testing.T) {
	// Clear cluster info
	instance := createLSD("ollama", "")
	r := &LlamaStackDistributionReconciler{ClusterInfo: nil}
	err := r.validateDistribution(instance)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize cluster info")
}
