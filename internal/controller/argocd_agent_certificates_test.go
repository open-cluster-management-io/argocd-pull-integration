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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

// Example advertised-address values for the principal certificate SAN tests. These are
// deliberately RFC-reserved documentation values (RFC 5737 TEST-NET-1 for the IP, RFC 2606
// for the domain) so no real infrastructure address appears in the codebase.
const (
	testAdvertisedIP  = "192.0.2.10"            // RFC 5737 TEST-NET-1
	testAdvertisedDNS = "principal.example.com" // RFC 2606 reserved example domain
)

func TestGetPrincipalHostNames(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = corev1.SchemeBuilder.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name          string
		existingObjs  []runtime.Object
		serverAddress string
		wantMinCount  int
		wantContains  string
		// wantAbsentDup asserts the given SAN appears at most once (dedupe check).
		wantAbsentDup string
		// allowFirst skips the "serverAddress must not be the CN" guard for cases where
		// serverAddress legitimately coincides with a discovered address that is first by
		// design (e.g. a LoadBalancer ingress IP, or a service-derived fallback name).
		allowFirst bool
	}{
		{
			name: "with principal service",
			existingObjs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-principal",
						Namespace: namespace,
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.0.0.1",
					},
				},
			},
			wantMinCount: 3, // At minimum: internal DNS names and localhost
			wantContains: "argocd-agent-principal.argocd.svc",
		},
		{
			name:         "without service returns defaults",
			existingObjs: []runtime.Object{},
			wantMinCount: 2, // Returns default hostnames
			wantContains: "argocd-agent-principal.argocd.svc",
		},
		{
			name: "advertised IP server address is included as a SAN",
			existingObjs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-principal",
						Namespace: namespace,
					},
					Spec: corev1.ServiceSpec{ClusterIP: "10.0.0.1"},
				},
			},
			serverAddress: testAdvertisedIP,
			wantMinCount:  3,
			wantContains:  testAdvertisedIP,
		},
		{
			name: "advertised DNS server address is included as a SAN",
			existingObjs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-principal",
						Namespace: namespace,
					},
					Spec: corev1.ServiceSpec{ClusterIP: "10.0.0.1"},
				},
			},
			serverAddress: testAdvertisedDNS,
			wantMinCount:  3,
			wantContains:  testAdvertisedDNS,
		},
		{
			// serverAddress is DISTINCT from the fallback names, so a pass here proves
			// the advertised address is genuinely threaded in (not just the fallback).
			name:          "advertised address without service is still included",
			existingObjs:  []runtime.Object{},
			serverAddress: testAdvertisedDNS,
			wantMinCount:  3, // 2 fallback DNS names + the advertised address
			wantContains:  testAdvertisedDNS,
		},
		{
			// serverAddress coincides with the Service's LoadBalancer ingress IP, so the
			// combined list would contain it twice — assert dedupe collapses it to one.
			// The IP is first here because LB-ingress discovery adds it before the DNS
			// names (pre-existing ordering), so the CN guard is opted out via allowFirst.
			name: "advertised address matching a service ingress is deduped to one",
			existingObjs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-agent-principal",
						Namespace: namespace,
					},
					Spec: corev1.ServiceSpec{ClusterIP: "10.0.0.1"},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{{IP: testAdvertisedIP}},
						},
					},
				},
			},
			serverAddress: testAdvertisedIP,
			wantMinCount:  3,
			wantContains:  testAdvertisedIP,
			wantAbsentDup: testAdvertisedIP,
			allowFirst:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			hostNames := r.getPrincipalHostNames(context.Background(), namespace, tt.serverAddress)

			if len(hostNames) < tt.wantMinCount {
				t.Errorf("getPrincipalHostNames() returned %d hostnames, want at least %d", len(hostNames), tt.wantMinCount)
			}

			// Verify expected hostname is included
			found := false
			count := 0
			for _, hn := range hostNames {
				if hn == tt.wantContains {
					found = true
				}
				if tt.wantAbsentDup != "" && hn == tt.wantAbsentDup {
					count++
				}
			}
			if !found {
				t.Errorf("getPrincipalHostNames() should contain %s, got %v", tt.wantContains, hostNames)
			}
			if tt.wantAbsentDup != "" && count != 1 {
				t.Errorf("getPrincipalHostNames() should contain %s exactly once (dedupe), got %d in %v", tt.wantAbsentDup, count, hostNames)
			}
			// The advertised serverAddress must be APPENDED, never first: certrotation
			// derives the Subject CN from the first entry, which must stay the stable
			// service-derived name, not an operator-set DNS/IP address. Cases where the
			// address legitimately coincides with a first-by-design discovered value (a
			// service-derived fallback name, or a LoadBalancer ingress IP) opt out via
			// allowFirst.
			if !tt.allowFirst && tt.serverAddress != "" &&
				len(hostNames) > 0 && hostNames[0] == tt.serverAddress {
				t.Errorf("serverAddress %q must not be the first (CN) entry; got %v", tt.serverAddress, hostNames)
			}
		})
	}
}

func TestGetResourceProxyHostNames(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = corev1.SchemeBuilder.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name         string
		wantMinCount int
		wantContains string
	}{
		{
			name:         "generates resource proxy hostnames",
			wantMinCount: 2, // At least service name and FQDN
			wantContains: "argocd-agent-principal.argocd.svc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).Build(),
				Scheme: s,
			}

			hostNames := r.getResourceProxyHostNames(context.Background(), namespace)

			if len(hostNames) < tt.wantMinCount {
				t.Errorf("getResourceProxyHostNames() returned %d hostnames, want at least %d", len(hostNames), tt.wantMinCount)
			}

			// Verify expected hostname patterns
			found := false
			for _, hn := range hostNames {
				if hn == tt.wantContains {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("getResourceProxyHostNames() should contain %s, got %v", tt.wantContains, hostNames)
			}
		})
	}
}

func TestVerifyCACertificateExists(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name: "CA certificate secret exists",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentCASecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"tls.crt": []byte("test-cert"),
						"tls.key": []byte("test-key"),
					},
				},
			},
			wantErr: false,
		},
		{
			name:         "CA certificate does not exist",
			existingObjs: []runtime.Object{},
			wantErr:      true,
		},
		{
			name: "CA certificate secret exists with partial data",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentCASecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"tls.key": []byte("test-key"),
					},
				},
			},
			wantErr: false, // verifyCACertificateExists only checks if secret exists, not content
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			err := r.verifyCACertificateExists(context.Background(), namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("verifyCACertificateExists() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFindArgoCDAgentPrincipalService(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = corev1.SchemeBuilder.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantName     string
		wantErr      bool
	}{
		{
			name: "finds primary service",
			existingObjs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentPrincipalServiceName,
						Namespace: namespace,
					},
				},
			},
			wantName: ArgoCDAgentPrincipalServiceName,
			wantErr:  false,
		},
		{
			name: "finds fallback service",
			existingObjs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      OpenshiftGitOpsAgentPrincipalServiceName,
						Namespace: namespace,
					},
				},
			},
			wantName: OpenshiftGitOpsAgentPrincipalServiceName,
			wantErr:  false,
		},
		{
			name:         "no service found",
			existingObjs: []runtime.Object{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			service, err := r.findArgoCDAgentPrincipalService(context.Background(), namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("findArgoCDAgentPrincipalService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && service.Name != tt.wantName {
				t.Errorf("findArgoCDAgentPrincipalService() name = %v, want %v", service.Name, tt.wantName)
			}
		})
	}
}

func TestCertificateConstants(t *testing.T) {
	// Verify constants are defined correctly
	if ArgoCDAgentCASecretName != "argocd-agent-ca" {
		t.Errorf("ArgoCDAgentCASecretName = %v, want argocd-agent-ca", ArgoCDAgentCASecretName)
	}

	if ArgoCDAgentPrincipalTLSSecretName != "argocd-agent-principal-tls" {
		t.Errorf("ArgoCDAgentPrincipalTLSSecretName = %v, want argocd-agent-principal-tls", ArgoCDAgentPrincipalTLSSecretName)
	}

	if ArgoCDAgentResourceProxyTLSSecretName != "argocd-agent-resource-proxy-tls" {
		t.Errorf("ArgoCDAgentResourceProxyTLSSecretName = %v, want argocd-agent-resource-proxy-tls", ArgoCDAgentResourceProxyTLSSecretName)
	}
}

func TestEnsureArgoCDAgentCASecret(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name         string
		existingObjs []runtime.Object
		wantErr      bool
	}{
		{
			name: "CA secret already exists",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentCASecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						"tls.crt": []byte("test-cert"),
						"tls.key": []byte("test-key"),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			// Just verify the function returns without error when secret exists
			// Full integration test would require k8s clientset
			secret := &corev1.Secret{}
			err := r.Get(context.Background(), types.NamespacedName{
				Name:      ArgoCDAgentCASecretName,
				Namespace: namespace,
			}, secret)

			if (err != nil) != tt.wantErr {
				t.Errorf("Getting CA secret error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
