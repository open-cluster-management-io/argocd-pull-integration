// Package images provides centralized image defaults for external dependencies
package images

import "testing"

func TestGetFullImageReference(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		tag      string
		expected string
	}{
		{
			name:     "standard image with tag",
			image:    "quay.io/argoprojlabs/argocd-operator",
			tag:      "v0.17.0",
			expected: "quay.io/argoprojlabs/argocd-operator:v0.17.0",
		},
		{
			name:     "image with version tag",
			image:    "quay.io/argoprojlabs/argocd-agent",
			tag:      "v0.5.3",
			expected: "quay.io/argoprojlabs/argocd-agent:v0.5.3",
		},
		{
			name:     "empty image and tag",
			image:    "",
			tag:      "",
			expected: ":",
		},
		{
			name:     "image without registry",
			image:    "nginx",
			tag:      "1.21",
			expected: "nginx:1.21",
		},
		{
			name:     "image with empty tag",
			image:    "quay.io/test/image",
			tag:      "",
			expected: "quay.io/test/image:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetFullImageReference(tt.image, tt.tag)
			if result != tt.expected {
				t.Errorf("GetFullImageReference(%q, %q) = %q, want %q",
					tt.image, tt.tag, result, tt.expected)
			}
		})
	}
}

func TestDefaultImageConstants(t *testing.T) {
	// Just verify the constants are defined and not empty
	// Don't hardcode expected values so tests don't break when versions are updated
	tests := []struct {
		name  string
		value string
	}{
		{
			name:  "DefaultOperatorImage",
			value: DefaultOperatorImage,
		},
		{
			name:  "DefaultOperatorTag",
			value: DefaultOperatorTag,
		},
		{
			name:  "DefaultAgentImage",
			value: DefaultAgentImage,
		},
		{
			name:  "DefaultAgentTag",
			value: DefaultAgentTag,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				t.Errorf("%s should not be empty", tt.name)
			}
		})
	}
}

func TestDefaultImageReferences(t *testing.T) {
	// Verify that GetFullImageReference works with the default constants
	// Don't hardcode expected results so tests don't break when versions are updated
	tests := []struct {
		name  string
		image string
		tag   string
	}{
		{
			name:  "full operator image reference",
			image: DefaultOperatorImage,
			tag:   DefaultOperatorTag,
		},
		{
			name:  "full agent image reference",
			image: DefaultAgentImage,
			tag:   DefaultAgentTag,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetFullImageReference(tt.image, tt.tag)
			// Verify the result has the expected format: "image:tag"
			expected := tt.image + ":" + tt.tag
			if result != expected {
				t.Errorf("GetFullImageReference(%q, %q) = %q, want %q", tt.image, tt.tag, result, expected)
			}
			// Verify the result is not empty
			if result == "" {
				t.Error("GetFullImageReference() returned empty string")
			}
		})
	}
}
