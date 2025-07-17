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
	"errors"
	"fmt"

	llamav1alpha1 "github.com/llamastack/llama-stack-k8s-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// buildContainerSpec creates the container specification.
func buildContainerSpec(instance *llamav1alpha1.LlamaStackDistribution, image string) corev1.Container {
	container := corev1.Container{
		Name:            getContainerName(instance),
		Image:           image,
		Resources:       instance.Spec.Server.ContainerSpec.Resources,
		ImagePullPolicy: corev1.PullAlways,
		Ports:           []corev1.ContainerPort{{ContainerPort: getContainerPort(instance)}},
	}

	// Configure environment variables and mounts
	configureContainerEnvironment(instance, &container)
	configureContainerMounts(instance, &container)
	configureContainerCommands(instance, &container)

	return container
}

// getContainerName returns the container name, using custom name if specified.
func getContainerName(instance *llamav1alpha1.LlamaStackDistribution) string {
	if instance.Spec.Server.ContainerSpec.Name != "" {
		return instance.Spec.Server.ContainerSpec.Name
	}
	return llamav1alpha1.DefaultContainerName
}

// getContainerPort returns the container port, using custom port if specified.
func getContainerPort(instance *llamav1alpha1.LlamaStackDistribution) int32 {
	if instance.Spec.Server.ContainerSpec.Port != 0 {
		return instance.Spec.Server.ContainerSpec.Port
	}
	return llamav1alpha1.DefaultServerPort
}

// configureContainerEnvironment sets up environment variables for the container.
func configureContainerEnvironment(instance *llamav1alpha1.LlamaStackDistribution, container *corev1.Container) {
	mountPath := getMountPath(instance)

	// Add HF_HOME variable to our mount path so that downloaded models and datasets are stored
	// on the same volume as the storage. This is not critical but useful if the server is
	// restarted so the models and datasets are not lost and need to be downloaded again.
	// For more information, see https://huggingface.co/docs/datasets/en/cache
	container.Env = append(container.Env, corev1.EnvVar{
		Name:  "HF_HOME",
		Value: mountPath,
	})

	// Add CA bundle environment variable if TLS config is specified or ODH CA bundle is available
	var caBundleKey string
	var hasCABundle bool

	if instance.Spec.Server.TLSConfig != nil && instance.Spec.Server.TLSConfig.CABundle != nil {
		// Use explicit CA bundle configuration
		caBundleKey = instance.Spec.Server.TLSConfig.CABundle.ConfigMapKey
		if caBundleKey == "" {
			caBundleKey = "ca-bundle.crt"
		}
		hasCABundle = true
	} else {
		// Check for ODH CA bundle availability
		// We'll use a predictable key name, preferring odh-ca-bundle.crt over ca-bundle.crt
		caBundleKey = "odh-ca-bundle.crt"
		hasCABundle = true // We'll assume it exists if ODH CA bundle is available
	}

	if hasCABundle {
		// Set SSL_CERT_FILE to point to the specific CA bundle file
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  "SSL_CERT_FILE",
			Value: "/etc/ssl/certs/" + caBundleKey,
		})

		// Also set SSL_CERT_DIR for applications that prefer directory-based CA lookup
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  "SSL_CERT_DIR",
			Value: "/etc/ssl/certs",
		})
	}

	// Finally, add the user provided env vars
	container.Env = append(container.Env, instance.Spec.Server.ContainerSpec.Env...)
}

// configureContainerMounts sets up volume mounts for the container.
func configureContainerMounts(instance *llamav1alpha1.LlamaStackDistribution, container *corev1.Container) {
	mountPath := getMountPath(instance)

	// Add volume mount for storage
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      "lls-storage",
		MountPath: mountPath,
	})

	// Add ConfigMap volume mount if user config is specified
	if instance.Spec.Server.UserConfig != nil && instance.Spec.Server.UserConfig.ConfigMapName != "" {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      "user-config",
			MountPath: "/etc/llama-stack/",
			ReadOnly:  true,
		})
	}

	// Add CA bundle volume mount if TLS config is specified or ODH CA bundle is available
	var caBundleKey string
	var hasCABundle bool

	if instance.Spec.Server.TLSConfig != nil && instance.Spec.Server.TLSConfig.CABundle != nil {
		// Use explicit CA bundle configuration
		caBundleKey = instance.Spec.Server.TLSConfig.CABundle.ConfigMapKey
		if caBundleKey == "" {
			caBundleKey = "ca-bundle.crt"
		}
		hasCABundle = true
	} else {
		// Use ODH CA bundle if available
		caBundleKey = "odh-ca-bundle.crt"
		hasCABundle = true // We'll assume it exists if ODH CA bundle is available
	}

	if hasCABundle {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      "ca-bundle",
			MountPath: "/etc/ssl/certs/" + caBundleKey,
			SubPath:   caBundleKey,
			ReadOnly:  true,
		})
	}
}

// configureContainerCommands sets up container commands and args.
func configureContainerCommands(instance *llamav1alpha1.LlamaStackDistribution, container *corev1.Container) {
	// Override the container entrypoint to use the custom config file if user config is specified
	if instance.Spec.Server.UserConfig != nil && instance.Spec.Server.UserConfig.ConfigMapName != "" {
		container.Command = []string{"python", "-m", "llama_stack.distribution.server.server"}
		container.Args = []string{"--config", "/etc/llama-stack/run.yaml"}
	}

	// Apply user-specified command and args (takes precedence)
	if len(instance.Spec.Server.ContainerSpec.Command) > 0 {
		container.Command = instance.Spec.Server.ContainerSpec.Command
	}

	if len(instance.Spec.Server.ContainerSpec.Args) > 0 {
		container.Args = instance.Spec.Server.ContainerSpec.Args
	}
}

// getMountPath returns the mount path, using custom path if specified.
func getMountPath(instance *llamav1alpha1.LlamaStackDistribution) string {
	if instance.Spec.Server.Storage != nil && instance.Spec.Server.Storage.MountPath != "" {
		return instance.Spec.Server.Storage.MountPath
	}
	return llamav1alpha1.DefaultMountPath
}

// configurePodStorage configures the pod storage and returns the complete pod spec.
func configurePodStorage(instance *llamav1alpha1.LlamaStackDistribution, container corev1.Container) corev1.PodSpec {
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
	}

	// Add storage volume
	if instance.Spec.Server.Storage != nil {
		// Use PVC for persistent storage
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "lls-storage",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: instance.Name + "-pvc",
				},
			},
		})

		// Add init container to fix permissions on the PVC mount
		mountPath := llamav1alpha1.DefaultMountPath
		if instance.Spec.Server.Storage.MountPath != "" {
			mountPath = instance.Spec.Server.Storage.MountPath
		}

		initContainer := corev1.Container{
			Name:  "update-pvc-permissions",
			Image: "registry.access.redhat.com/ubi9/ubi-minimal:latest",
			Command: []string{
				"/bin/sh",
				"-c",
				fmt.Sprintf("chown --verbose --recursive 1001:0 %s", mountPath),
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "lls-storage",
					MountPath: mountPath,
				},
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:  ptr.To(int64(0)), // Run as root to be able to change ownership
				RunAsGroup: ptr.To(int64(0)),
			},
		}

		podSpec.InitContainers = append(podSpec.InitContainers, initContainer)
	} else {
		// Use emptyDir for non-persistent storage
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "lls-storage",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// Add ConfigMap volume if user config is specified
	if instance.Spec.Server.UserConfig != nil && instance.Spec.Server.UserConfig.ConfigMapName != "" {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "user-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: instance.Spec.Server.UserConfig.ConfigMapName,
					},
				},
			},
		})
	}

	// Add CA bundle ConfigMap volume if TLS config is specified
	if instance.Spec.Server.TLSConfig != nil && instance.Spec.Server.TLSConfig.CABundle != nil {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: instance.Spec.Server.TLSConfig.CABundle.ConfigMapName,
					},
				},
			},
		})
	} else {
		// Note: ODH CA bundle will be mounted directly from the namespace-local copy
		// The OpenShift AI operator copies the odh-trusted-ca-bundle ConfigMap to all namespaces
		// We mount it directly without creating our own copy
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "odh-trusted-ca-bundle", // Standard name used by OpenShift AI operator
					},
					Optional: ptr.To(true), // Make it optional in case OpenShift AI operator is not installed
				},
			},
		})
	}

	// Apply pod overrides including ServiceAccount, volumes, and volume mounts
	configurePodOverrides(instance, &podSpec)

	return podSpec
}

// configurePodOverrides applies pod-level overrides from the LlamaStackDistribution spec.
func configurePodOverrides(instance *llamav1alpha1.LlamaStackDistribution, podSpec *corev1.PodSpec) {
	if instance.Spec.Server.PodOverrides != nil {
		// Set ServiceAccount name if specified
		if instance.Spec.Server.PodOverrides.ServiceAccountName != "" {
			podSpec.ServiceAccountName = instance.Spec.Server.PodOverrides.ServiceAccountName
		}

		// Add volumes if specified
		if len(instance.Spec.Server.PodOverrides.Volumes) > 0 {
			podSpec.Volumes = append(podSpec.Volumes, instance.Spec.Server.PodOverrides.Volumes...)
		}

		// Add volume mounts if specified
		if len(instance.Spec.Server.PodOverrides.VolumeMounts) > 0 {
			if len(podSpec.Containers) > 0 {
				podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, instance.Spec.Server.PodOverrides.VolumeMounts...)
			}
		}
	}
}

// validateDistribution validates the distribution configuration.
func (r *LlamaStackDistributionReconciler) validateDistribution(instance *llamav1alpha1.LlamaStackDistribution) error {
	// If using distribution name, validate it exists in clusterInfo
	if instance.Spec.Server.Distribution.Name != "" {
		if r.ClusterInfo == nil {
			return errors.New("failed to initialize cluster info")
		}
		if _, exists := r.ClusterInfo.DistributionImages[instance.Spec.Server.Distribution.Name]; !exists {
			return fmt.Errorf("failed to validate distribution: %s. Distribution name not supported", instance.Spec.Server.Distribution.Name)
		}
	}

	return nil
}

// resolveImage determines the container image to use based on the distribution configuration.
// It returns the resolved image and any error encountered.
func (r *LlamaStackDistributionReconciler) resolveImage(distribution llamav1alpha1.DistributionType) (string, error) {
	distributionMap := r.ClusterInfo.DistributionImages
	switch {
	case distribution.Name != "":
		if _, exists := distributionMap[distribution.Name]; !exists {
			return "", fmt.Errorf("failed to validate distribution name: %s", distribution.Name)
		}
		return distributionMap[distribution.Name], nil
	case distribution.Image != "":
		return distribution.Image, nil
	default:
		return "", errors.New("failed to validate distribution: either distribution.name or distribution.image must be set")
	}
}
