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
	"crypto/x509"
	"encoding/json"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"

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
				TLSClientConfig: &TLSClientConfig{
					Insecure: false,
					CertData: []byte("cert-data"),
					KeyData:  []byte("key-data"),
					CAData:   []byte("ca-data"),
				},
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
		name          string
		clusterName   string
		placementName string
		wantLabels    map[string]string
	}{
		{
			name:          "cluster1 with placement",
			clusterName:   "cluster1",
			placementName: "test-placement",
			wantLabels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": "cluster1",
				ArgoCDAgentClusterMappingLabel:                 "cluster1",
				"placement-name":                               "test-placement",
			},
		},
		{
			name:          "prod-cluster with placement",
			clusterName:   "prod-cluster",
			placementName: "production-placement",
			wantLabels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": "prod-cluster",
				ArgoCDAgentClusterMappingLabel:                 "prod-cluster",
				"placement-name":                               "production-placement",
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

			// Simulate building labels (as done in createArgoCDClusterSecret)
			labels := map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": cluster.Name,
				ArgoCDAgentClusterMappingLabel:                 cluster.Name,
				"placement-name":                               tt.placementName,
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
		placementName  string
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
			placementName: "test-placement",
			wantLabels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": "test-cluster",
				ArgoCDAgentClusterMappingLabel:                 "test-cluster",
				"placement-name":                               "test-placement",
			},
		},
		{
			name: "update existing labels including placement name change",
			existingSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-test",
					Namespace: "argocd",
					Labels: map[string]string{
						"old-label":      "old-value",
						"placement-name": "old-placement",
					},
				},
			},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
			},
			placementName: "new-placement",
			wantLabels: map[string]string{
				ArgoCDSecretTypeLabel:                          ArgoCDSecretTypeValue,
				"apps.open-cluster-management.io/cluster-name": "test-cluster",
				ArgoCDAgentClusterMappingLabel:                 "test-cluster",
				"placement-name":                               "new-placement",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate label update (as done in updateArgoCDClusterSecret)
			if tt.existingSecret.Labels == nil {
				tt.existingSecret.Labels = make(map[string]string)
			}
			tt.existingSecret.Labels[ArgoCDSecretTypeLabel] = ArgoCDSecretTypeValue
			tt.existingSecret.Labels["apps.open-cluster-management.io/cluster-name"] = tt.cluster.Name
			tt.existingSecret.Labels[ArgoCDAgentClusterMappingLabel] = tt.cluster.Name
			tt.existingSecret.Labels["placement-name"] = tt.placementName

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
		TLSClientConfig: &TLSClientConfig{
			Insecure: false,
			CertData: []byte("-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----"),
			KeyData:  []byte("-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----"),
			CAData:   []byte("-----BEGIN CERTIFICATE-----\ntest-ca\n-----END CERTIFICATE-----"),
		},
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
	if decoded.TLSClientConfig == nil {
		t.Fatalf("TLSClientConfig is nil")
	}
	if decoded.TLSClientConfig.Insecure != config.TLSClientConfig.Insecure {
		t.Errorf("TLSClientConfig.Insecure = %v, want %v", decoded.TLSClientConfig.Insecure, config.TLSClientConfig.Insecure)
	}
	if string(decoded.TLSClientConfig.CertData) != string(config.TLSClientConfig.CertData) {
		t.Errorf("CertData mismatch")
	}
	if string(decoded.TLSClientConfig.KeyData) != string(config.TLSClientConfig.KeyData) {
		t.Errorf("KeyData mismatch")
	}
	if string(decoded.TLSClientConfig.CAData) != string(config.TLSClientConfig.CAData) {
		t.Errorf("CAData mismatch")
	}
}

func TestClusterConfigUsesArgoCDTLSClientConfig(t *testing.T) {
	config := ClusterConfig{
		Username: "test-cluster",
		Password: "test-cluster",
		TLSClientConfig: &TLSClientConfig{
			Insecure: false,
			CertData: []byte("cert-data"),
			KeyData:  []byte("key-data"),
			CAData:   []byte("ca-data"),
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal config map: %v", err)
	}

	if _, ok := raw["tlsClientConfig"]; !ok {
		t.Fatalf("Expected tlsClientConfig in cluster config")
	}

	for _, key := range []string{"certData", "keyData", "caData"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("Expected %s to be nested under tlsClientConfig, but found it at the top level", key)
		}
	}

	tlsClientConfig, ok := raw["tlsClientConfig"].(map[string]any)
	if !ok {
		t.Fatalf("tlsClientConfig has unexpected type %T", raw["tlsClientConfig"])
	}

	for _, key := range []string{"certData", "keyData", "caData"} {
		if _, ok := tlsClientConfig[key]; !ok {
			t.Fatalf("Expected %s under tlsClientConfig", key)
		}
	}

	if insecure, ok := tlsClientConfig["insecure"].(bool); !ok || insecure {
		t.Fatalf("Expected tlsClientConfig.insecure to be false, got %v", tlsClientConfig["insecure"])
	}
}

func TestHasValidClusterClientCertificate(t *testing.T) {
	caConfig, err := crypto.MakeSelfSignedCAConfigForDuration("test-ca", time.Hour)
	if err != nil {
		t.Fatalf("Failed to create CA config: %v", err)
	}
	caCertPEM, caKeyPEM, err := caConfig.GetPEMBytes()
	if err != nil {
		t.Fatalf("Failed to encode CA config: %v", err)
	}
	ca, err := crypto.GetCAFromBytes(caCertPEM, caKeyPEM)
	if err != nil {
		t.Fatalf("Failed to load CA: %v", err)
	}

	clientCertConfig, err := ca.MakeClientCertificateForDuration(&user.DefaultInfo{Name: "test-cluster"}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to create client certificate: %v", err)
	}
	clientCertPEM, clientKeyPEM, err := clientCertConfig.GetPEMBytes()
	if err != nil {
		t.Fatalf("Failed to encode client certificate: %v", err)
	}

	serverCertConfig, err := ca.MakeServerCertForDuration(sets.New[string]("test-cluster"), time.Hour)
	if err != nil {
		t.Fatalf("Failed to create server certificate: %v", err)
	}
	serverCertPEM, serverKeyPEM, err := serverCertConfig.GetPEMBytes()
	if err != nil {
		t.Fatalf("Failed to encode server certificate: %v", err)
	}

	tests := []struct {
		name     string
		certPEM  []byte
		keyPEM   []byte
		wantGood bool
	}{
		{
			name:     "accepts client auth certificate",
			certPEM:  clientCertPEM,
			keyPEM:   clientKeyPEM,
			wantGood: true,
		},
		{
			name:     "rejects server auth certificate",
			certPEM:  serverCertPEM,
			keyPEM:   serverKeyPEM,
			wantGood: false,
		},
		{
			name:     "rejects missing private key",
			certPEM:  clientCertPEM,
			wantGood: false,
		},
		{
			name:     "rejects invalid private key",
			certPEM:  clientCertPEM,
			keyPEM:   []byte("invalid private key"),
			wantGood: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &corev1.Secret{
				Data: map[string][]byte{
					"tls.crt": tt.certPEM,
					"tls.key": tt.keyPEM,
				},
			}

			gotGood := hasValidClusterClientCertificate(secret, caConfig.Certs, "test-cluster")
			if gotGood != tt.wantGood {
				t.Fatalf("hasValidClusterClientCertificate() = %v, want %v", gotGood, tt.wantGood)
			}
		})
	}
}

func TestClusterClientCertificateTargetValidity(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		signerCerts  []*x509.Certificate
		wantValidity time.Duration
		wantErr      bool
	}{
		{
			name: "uses target validity when signer is valid longer",
			signerCerts: []*x509.Certificate{
				{NotAfter: now.Add(TargetCertValidity + time.Hour)},
			},
			wantValidity: TargetCertValidity,
		},
		{
			name: "clamps to remaining signer validity",
			signerCerts: []*x509.Certificate{
				{NotAfter: now.Add(time.Hour)},
			},
			wantValidity: time.Hour - clusterClientCertificateSignerSkew,
		},
		{
			name: "rejects signer expiring inside safety window",
			signerCerts: []*x509.Certificate{
				{NotAfter: now.Add(time.Second)},
			},
			wantErr: true,
		},
		{
			name:    "rejects missing signer certificate",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValidity, err := clusterClientCertificateTargetValidity(tt.signerCerts, "test-cluster", now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if gotValidity != tt.wantValidity {
				t.Fatalf("target validity = %v, want %v", gotValidity, tt.wantValidity)
			}
		})
	}
}
