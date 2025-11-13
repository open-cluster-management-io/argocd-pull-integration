/*
Copyright 2025 Open Cluster Management.

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
	"os"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
	"open-cluster-management.io/argocd-pull-integration/internal/pkg/images"
)

const (
	// ControllerImageEnvVar is the environment variable name for the controller image
	ControllerImageEnvVar = "CONTROLLER_IMAGE"
)

// getControllerImage retrieves the controller's image from environment variable
// This follows K8s standard practice of using downward API to inject pod metadata
func (r *GitOpsClusterReconciler) getControllerImage(ctx context.Context, namespace string) (string, error) {
	// Get image from environment variable (set via downward API in deployment)
	image := os.Getenv(ControllerImageEnvVar)
	if image == "" {
		return "", fmt.Errorf("CONTROLLER_IMAGE environment variable is not set - this must be configured in the deployment")
	}

	klog.V(2).InfoS("Found controller image from environment", "image", image)
	return image, nil
}

// getAddOnTemplateName generates a unique AddOnTemplate name for the GitOpsCluster
func getAddOnTemplateName(gitOpsCluster *appsv1alpha1.GitOpsCluster) string {
	// Use namespace-name format for uniqueness
	return fmt.Sprintf("argocd-agent-addon-%s-%s", gitOpsCluster.Namespace, gitOpsCluster.Name)
}

// EnsureAddOnTemplate creates or updates the AddOnTemplate for a GitOpsCluster
func (r *GitOpsClusterReconciler) EnsureAddOnTemplate(ctx context.Context, gitOpsCluster *appsv1alpha1.GitOpsCluster) error {
	templateName := getAddOnTemplateName(gitOpsCluster)

	// Get the controller image
	addonImage, err := r.getControllerImage(ctx, gitOpsCluster.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get controller image: %w", err)
	}

	// Get operator and agent images from GitOpsCluster spec or use defaults
	operatorImage := gitOpsCluster.Spec.ArgoCDAgentAddon.OperatorImage
	if operatorImage == "" {
		operatorImage = images.GetFullImageReference(images.DefaultOperatorImage, images.DefaultOperatorTag)
	}

	agentImage := gitOpsCluster.Spec.ArgoCDAgentAddon.AgentImage
	if agentImage == "" {
		agentImage = images.GetFullImageReference(images.DefaultAgentImage, images.DefaultAgentTag)
	}

	// Build the AddOnTemplate
	addonTemplate := &addonv1alpha1.AddOnTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: templateName,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":                            "argocd-pull-integration",
				"app.kubernetes.io/component":                             "addon-template",
				"gitopscluster.apps.open-cluster-management.io/name":      gitOpsCluster.Name,
				"gitopscluster.apps.open-cluster-management.io/namespace": gitOpsCluster.Namespace,
			},
		},
		Spec: addonv1alpha1.AddOnTemplateSpec{
			AddonName: ArgoCDAgentAddonName,
			AgentSpec: workv1.ManifestWorkSpec{
				Workload: workv1.ManifestsTemplate{
					Manifests: buildAddonManifests(addonImage, operatorImage, agentImage),
				},
			},
			Registration: []addonv1alpha1.RegistrationSpec{
				{
					Type: addonv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "open-cluster-management.io/argocd-agent-addon",
						SigningCA: addonv1alpha1.SigningCARef{
							Name:      "argocd-agent-ca",
							Namespace: gitOpsCluster.Namespace, // Use GitOpsCluster namespace
						},
					},
				},
			},
		},
	}

	// Check if AddOnTemplate already exists
	existing := &addonv1alpha1.AddOnTemplate{}
	err = r.Get(ctx, types.NamespacedName{Name: templateName}, existing)
	if err == nil {
		// AddOnTemplate exists, update it
		klog.V(2).InfoS("Updating AddOnTemplate", "name", templateName)
		existing.Spec = addonTemplate.Spec
		existing.Labels = addonTemplate.Labels
		return r.Update(ctx, existing)
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to get AddOnTemplate: %w", err)
	}

	// Create new AddOnTemplate
	klog.InfoS("Creating AddOnTemplate", "name", templateName)
	return r.Create(ctx, addonTemplate)
}

// buildAddonManifests builds the manifest list for the AddOnTemplate
func buildAddonManifests(addonImage, operatorImage, agentImage string) []workv1.Manifest {
	manifests := []workv1.Manifest{
		// Pre-delete cleanup job
		{
			RawExtension: runtime.RawExtension{
				Object: &batchv1.Job{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "batch/v1",
						Kind:       "Job",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-addon-cleanup",
						Namespace: "open-cluster-management-agent-addon",
						Annotations: map[string]string{
							"addon.open-cluster-management.io/addon-pre-delete": "",
						},
					},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								ServiceAccountName: "argocd-agent-addon",
								RestartPolicy:      corev1.RestartPolicyNever,
								Containers: []corev1.Container{
									{
										Name:            "cleanup",
										Image:           addonImage,
										ImagePullPolicy: corev1.PullIfNotPresent,
										Command:         []string{"/manager"},
										Args:            []string{"--mode=cleanup"},
										Env: []corev1.EnvVar{
											{
												Name:  "ARGOCD_OPERATOR_IMAGE",
												Value: operatorImage,
											},
											{
												Name:  "ARGOCD_AGENT_IMAGE",
												Value: agentImage,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		// ServiceAccount
		{
			RawExtension: runtime.RawExtension{
				Object: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-addon",
						Namespace: "open-cluster-management-agent-addon",
					},
				},
			},
		},
		// ClusterRoleBinding
		{
			RawExtension: runtime.RawExtension{
				Object: &rbacv1.ClusterRoleBinding{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "rbac.authorization.k8s.io/v1",
						Kind:       "ClusterRoleBinding",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "argocd-agent-addon",
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     "cluster-admin",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind:      "ServiceAccount",
							Name:      "argocd-agent-addon",
							Namespace: "open-cluster-management-agent-addon",
						},
					},
				},
			},
		},
		// Deployment
		{
			RawExtension: runtime.RawExtension{
				Object: &appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-addon",
						Namespace: "open-cluster-management-agent-addon",
						Labels: map[string]string{
							"app": "argocd-agent-addon",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(1),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "argocd-agent-addon",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "argocd-agent-addon",
								},
							},
							Spec: corev1.PodSpec{
								ServiceAccountName: "argocd-agent-addon",
								Containers: []corev1.Container{
									{
										Name:            "argocd-agent-addon",
										Image:           addonImage,
										ImagePullPolicy: corev1.PullIfNotPresent,
										Command:         []string{"/manager"},
										Args:            []string{"--mode=addon"},
										Env: []corev1.EnvVar{
											{
												Name:  "ARGOCD_OPERATOR_IMAGE",
												Value: operatorImage,
											},
											{
												Name:  "ARGOCD_AGENT_IMAGE",
												Value: agentImage,
											},
											{
												Name:  "ARGOCD_AGENT_SERVER_ADDRESS",
												Value: "{{ARGOCD_AGENT_SERVER_ADDRESS}}",
											},
											{
												Name:  "ARGOCD_AGENT_SERVER_PORT",
												Value: "{{ARGOCD_AGENT_SERVER_PORT}}",
											},
											{
												Name:  "ARGOCD_AGENT_MODE",
												Value: "{{ARGOCD_AGENT_MODE}}",
											},
										},
										SecurityContext: &corev1.SecurityContext{
											ReadOnlyRootFilesystem:   boolPtr(true),
											AllowPrivilegeEscalation: boolPtr(false),
											RunAsNonRoot:             boolPtr(true),
											Capabilities: &corev1.Capabilities{
												Drop: []corev1.Capability{"ALL"},
											},
										},
										VolumeMounts: []corev1.VolumeMount{
											{
												MountPath: "/tmp",
												Name:      "tmp-volume",
											},
										},
									},
								},
								Volumes: []corev1.Volume{
									{
										Name: "tmp-volume",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return manifests
}

// Helper functions
func int32Ptr(i int32) *int32 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}
