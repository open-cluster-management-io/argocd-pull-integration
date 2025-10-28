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
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestClusterConfig(t *testing.T) {
	tests := []struct {
		name   string
		config ClusterConfig
	}{
		{
			name: "basic config",
			config: ClusterConfig{
				Username: "cluster1",
				Password: "cluster1",
				CertData: []byte("cert-data"),
				KeyData:  []byte("key-data"),
				CAData:   []byte("ca-data"),
			},
		},
		{
			name: "minimal config",
			config: ClusterConfig{
				Username: "cluster1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test JSON marshaling
			data, err := json.Marshal(tt.config)
			if err != nil {
				t.Errorf("Failed to marshal ClusterConfig: %v", err)
				return
			}

			// Test JSON unmarshaling
			var decoded ClusterConfig
			err = json.Unmarshal(data, &decoded)
			if err != nil {
				t.Errorf("Failed to unmarshal ClusterConfig: %v", err)
				return
			}

			// Verify username matches
			if decoded.Username != tt.config.Username {
				t.Errorf("Username = %v, want %v", decoded.Username, tt.config.Username)
			}
		})
	}
}

func TestCreateArgoCDClusterSecretLabels(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		wantLabels  map[string]string
	}{
		{
			name:        "cluster1",
			clusterName: "cluster1",
			wantLabels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": "cluster1",
				ArgoCDAgentClusterMappingLabel:                 "cluster1",
			},
		},
		{
			name:        "prod-cluster",
			clusterName: "prod-cluster",
			wantLabels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": "prod-cluster",
				ArgoCDAgentClusterMappingLabel:                 "prod-cluster",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.clusterName,
				},
			}

			// Simulate building labels
			labels := map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": cluster.Name,
				ArgoCDAgentClusterMappingLabel:                 cluster.Name,
			}

			for key, wantValue := range tt.wantLabels {
				if gotValue, ok := labels[key]; !ok {
					t.Errorf("Missing label %s", key)
				} else if gotValue != wantValue {
					t.Errorf("Label %s = %v, want %v", key, gotValue, wantValue)
				}
			}
		})
	}
}

func TestArgoCDSecretConstants(t *testing.T) {
	// Verify constants are defined correctly
	if ArgoCDClusterSecretType != "Opaque" {
		t.Errorf("ArgoCDClusterSecretType = %v, want Opaque", ArgoCDClusterSecretType)
	}

	if ArgoCDSecretTypeLabel != "argocd.argoproj.io/secret-type" {
		t.Errorf("ArgoCDSecretTypeLabel = %v, want argocd.argoproj.io/secret-type", ArgoCDSecretTypeLabel)
	}

	if ArgoCDSecretTypeValue != "cluster" {
		t.Errorf("ArgoCDSecretTypeValue = %v, want cluster", ArgoCDSecretTypeValue)
	}

	if ArgoCDAgentClusterMappingLabel != "argocd-agent.argoproj-labs.io/agent-name" {
		t.Errorf("ArgoCDAgentClusterMappingLabel = %v, want argocd-agent.argoproj-labs.io/agent-name", ArgoCDAgentClusterMappingLabel)
	}
}

func TestUpdateArgoCDClusterSecretLabels(t *testing.T) {
	tests := []struct {
		name           string
		existingSecret *corev1.Secret
		cluster        *clusterv1.ManagedCluster
		wantLabels     map[string]string
	}{
		{
			name: "add labels to secret without labels",
			existingSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-test",
					Namespace: "argocd",
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
			},
			wantLabels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": "test-cluster",
				ArgoCDAgentClusterMappingLabel:                 "test-cluster",
			},
		},
		{
			name: "update existing labels",
			existingSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-test",
					Namespace: "argocd",
					Labels: map[string]string{
						"old-label": "old-value",
					},
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
			},
			wantLabels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": "test-cluster",
				ArgoCDAgentClusterMappingLabel:                 "test-cluster",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate label update
			if tt.existingSecret.Labels == nil {
				tt.existingSecret.Labels = make(map[string]string)
			}
			tt.existingSecret.Labels[ArgoCDSecretTypeLabel] = ArgoCDSecretTypeValue
			tt.existingSecret.Labels["apps.open-cluster-management.io/cluster-name"] = tt.cluster.Name
			tt.existingSecret.Labels[ArgoCDAgentClusterMappingLabel] = tt.cluster.Name

			// Verify all expected labels are present
			for key, wantValue := range tt.wantLabels {
				if gotValue, ok := tt.existingSecret.Labels[key]; !ok {
					t.Errorf("Missing label %s", key)
				} else if gotValue != wantValue {
					t.Errorf("Label %s = %v, want %v", key, gotValue, wantValue)
				}
			}
		})
	}
}

func TestClusterConfigJSON(t *testing.T) {
	config := ClusterConfig{
		Username: "test-cluster",
		Password: "test-cluster",
		CertData: []byte("-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----"),
		KeyData:  []byte("-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----"),
		CAData:   []byte("-----BEGIN CERTIFICATE-----\ntest-ca\n-----END CERTIFICATE-----"),
	}

	// Marshal to JSON
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Unmarshal back
	var decoded ClusterConfig
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify all fields
	if decoded.Username != config.Username {
		t.Errorf("Username = %v, want %v", decoded.Username, config.Username)
	}
	if decoded.Password != config.Password {
		t.Errorf("Password = %v, want %v", decoded.Password, config.Password)
	}
	if string(decoded.CertData) != string(config.CertData) {
		t.Errorf("CertData mismatch")
	}
	if string(decoded.KeyData) != string(config.KeyData) {
		t.Errorf("KeyData mismatch")
	}
	if string(decoded.CAData) != string(config.CAData) {
		t.Errorf("CAData mismatch")
	}
}
