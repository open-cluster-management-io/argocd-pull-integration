// Package images provides centralized image defaults for external dependencies
package images

const (
	// DefaultOperatorImage is the default ArgoCD operator image
	// Used when not specified in GitOpsCluster spec
	DefaultOperatorImage = "quay.io/argoprojlabs/argocd-operator"
	DefaultOperatorTag   = "latest"

	// DefaultAgentImage is the default ArgoCD agent image
	// Used when not specified in GitOpsCluster spec
	DefaultAgentImage = "quay.io/argoprojlabs/argocd-agent"
	DefaultAgentTag   = "v0.5.0"
)

// GetFullImageReference returns the full image reference with tag
func GetFullImageReference(image, tag string) string {
	return image + ":" + tag
}
