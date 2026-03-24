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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestArgoCDAgentAddonReconciler(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)

	tests := []struct {
		name              string
		interval          int
		operatorImage     string
		serverAddress     string
		serverPort        string
		mode              string
		wantIntervalValid bool
	}{
		{
			name:              "default configuration",
			interval:          60,
			operatorImage:     "quay.io/operator:latest",
			serverAddress:     "argocd-server.argocd.svc",
			serverPort:        "8080",
			mode:              "managed",
			wantIntervalValid: true,
		},
		{
			name:              "autonomous mode",
			interval:          30,
			operatorImage:     "quay.io/operator:latest",
			serverAddress:     "argocd-server.argocd.svc",
			serverPort:        "8080",
			mode:              "autonomous",
			wantIntervalValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ArgoCDAgentAddonReconciler{
				Client:                   fake.NewClientBuilder().WithScheme(s).Build(),
				Scheme:                   s,
				Interval:                 tt.interval,
				OperatorImage:            tt.operatorImage,
				ArgoCDAgentServerAddress: tt.serverAddress,
				ArgoCDAgentServerPort:    tt.serverPort,
				ArgoCDAgentMode:          tt.mode,
			}

			if r.Interval != tt.interval {
				t.Errorf("Interval = %v, want %v", r.Interval, tt.interval)
			}
			if r.OperatorImage != tt.operatorImage {
				t.Errorf("OperatorImage = %v, want %v", r.OperatorImage, tt.operatorImage)
			}
			if r.ArgoCDAgentServerAddress != tt.serverAddress {
				t.Errorf("ServerAddress = %v, want %v", r.ArgoCDAgentServerAddress, tt.serverAddress)
			}
			if r.ArgoCDAgentServerPort != tt.serverPort {
				t.Errorf("ServerPort = %v, want %v", r.ArgoCDAgentServerPort, tt.serverPort)
			}
			if r.ArgoCDAgentMode != tt.mode {
				t.Errorf("Mode = %v, want %v", r.ArgoCDAgentMode, tt.mode)
			}
		})
	}
}
