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

package addon

import (
	"testing"
)

func TestParseImageReference(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		wantRepo string
		wantTag  string
		wantErr  bool
	}{
		{
			name:     "image with tag",
			imageRef: "quay.io/argocd/operator:v1.0.0",
			wantRepo: "quay.io/argocd/operator",
			wantTag:  "v1.0.0",
			wantErr:  false,
		},
		{
			name:     "image without tag",
			imageRef: "quay.io/argocd/operator",
			wantRepo: "quay.io/argocd/operator",
			wantTag:  "latest",
			wantErr:  false,
		},
		{
			name:     "image with digest",
			imageRef: "quay.io/argocd/operator@sha256:1234567890abcdef",
			wantRepo: "quay.io/argocd/operator",
			wantTag:  "sha256:1234567890abcdef",
			wantErr:  false,
		},
		{
			name:     "simple image with tag",
			imageRef: "nginx:alpine",
			wantRepo: "nginx",
			wantTag:  "alpine",
			wantErr:  false,
		},
		{
			name:     "simple image without tag",
			imageRef: "nginx",
			wantRepo: "nginx",
			wantTag:  "latest",
			wantErr:  false,
		},
		{
			name:     "image with port in registry",
			imageRef: "localhost:5000/myimage:v1",
			wantRepo: "localhost:5000/myimage",
			wantTag:  "v1",
			wantErr:  false,
		},
		{
			name:     "image with latest tag explicitly",
			imageRef: "quay.io/argocd/operator:latest",
			wantRepo: "quay.io/argocd/operator",
			wantTag:  "latest",
			wantErr:  false,
		},
		{
			name:     "complex registry path",
			imageRef: "gcr.io/project/subpath/image:v2.3.4",
			wantRepo: "gcr.io/project/subpath/image",
			wantTag:  "v2.3.4",
			wantErr:  false,
		},
		{
			name:     "image with numeric tag",
			imageRef: "redis:7",
			wantRepo: "redis",
			wantTag:  "7",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRepo, gotTag, err := ParseImageReference(tt.imageRef)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseImageReference() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("ParseImageReference() gotRepo = %v, want %v", gotRepo, tt.wantRepo)
			}
			if gotTag != tt.wantTag {
				t.Errorf("ParseImageReference() gotTag = %v, want %v", gotTag, tt.wantTag)
			}
		})
	}
}
