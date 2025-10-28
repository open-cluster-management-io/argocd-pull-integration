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
	"strconv"

	"k8s.io/klog/v2"

	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

const (
	// ArgoCDAgentPrincipalServiceName is the name of the principal service
	ArgoCDAgentPrincipalServiceName = "argocd-agent-principal"

	// Fallback service name
	OpenshiftGitOpsAgentPrincipalServiceName = "openshift-gitops-agent-principal"
)

// EnsureServerAddressAndPort discovers and populates server address and port if they are empty
// Returns true if the GitOpsCluster spec was updated, false otherwise
func (r *GitOpsClusterReconciler) EnsureServerAddressAndPort(
	ctx context.Context,
	gitOpsCluster *appsv1alpha1.GitOpsCluster,
	argoCDNamespace string) (bool, error) {

	// Check if serverAddress and serverPort are already set in the GitOpsCluster spec
	if gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerAddress != "" &&
		gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerPort != "" {
		klog.V(2).InfoS("Server address and port already set in GitOpsCluster spec, using them",
			"address", gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerAddress,
			"port", gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerPort)
		return false, nil
	}

	// Discover server address and port from the ArgoCD agent principal service
	serverAddress, serverPort, err := r.discoverServerAddressAndPort(ctx, argoCDNamespace)
	if err != nil {
		return false, fmt.Errorf("failed to discover server address and port: %w", err)
	}

	// Update the GitOpsCluster spec with discovered values
	gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerAddress = serverAddress
	gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerPort = serverPort

	klog.InfoS("Auto-discovered and populated server address and port",
		"address", serverAddress, "port", serverPort,
		"namespace", gitOpsCluster.Namespace, "name", gitOpsCluster.Name)

	return true, nil
}

// discoverServerAddressAndPort discovers the external server address and port from the ArgoCD agent principal service
func (r *GitOpsClusterReconciler) discoverServerAddressAndPort(
	ctx context.Context,
	argoCDNamespace string) (string, string, error) {

	// Find the ArgoCD agent principal service
	service, err := r.findArgoCDAgentPrincipalService(ctx, argoCDNamespace)
	if err != nil {
		return "", "", fmt.Errorf("failed to find ArgoCD agent principal service: %w", err)
	}

	// Look for LoadBalancer external endpoints
	var serverAddress string
	var serverPort string = "443" // Default HTTPS port

	// Try to get external LoadBalancer hostname or IP
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.Hostname != "" {
			serverAddress = ingress.Hostname
			klog.InfoS("Discovered server address from LoadBalancer hostname", "hostname", serverAddress)
			break
		}
		if ingress.IP != "" {
			serverAddress = ingress.IP
			klog.InfoS("Discovered server address from LoadBalancer IP", "ip", serverAddress)
			break
		}
	}

	if serverAddress == "" {
		return "", "", fmt.Errorf("no external LoadBalancer IP or hostname found for service %s in namespace %s", service.Name, argoCDNamespace)
	}

	// Try to get the actual port from the service spec
	for _, port := range service.Spec.Ports {
		if port.Name == "https" || port.Port == 443 || port.Port == 8443 {
			serverPort = strconv.Itoa(int(port.Port))
			klog.InfoS("Discovered server port from service spec", "port", serverPort)
			break
		}
	}

	klog.InfoS("Discovered ArgoCD agent server endpoint", "address", serverAddress, "port", serverPort)
	return serverAddress, serverPort, nil
}
