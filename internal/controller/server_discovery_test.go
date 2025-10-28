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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
)

func TestEnsureServerAddressAndPort(t *testing.T) {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = appsv1alpha1.AddToScheme(s)

	namespace := "argocd"

	tests := []struct {
		name            string
		existingObjs    []runtime.Object
		gitOpsCluster   *appsv1alpha1.GitOpsCluster
		wantUpdated     bool
		wantErr         bool
		expectedAddress string
		expectedPort    string
	}{
		{
			name: "uses existing address and port",
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{
						PrincipalServerAddress: "existing.example.com",
						PrincipalServerPort:    "443",
					},
				},
			},
			wantUpdated: false,
			wantErr:     false,
		},
		{
			name: "discovers LoadBalancer hostname",
			existingObjs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentPrincipalServiceName,
						Namespace: namespace,
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
						Ports: []corev1.ServicePort{
							{
								Name:       "https",
								Port:       443,
								TargetPort: intstr.FromInt(8443),
							},
						},
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									Hostname: "lb.example.com",
								},
							},
						},
					},
				},
			},
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
				},
			},
			wantUpdated:     true,
			wantErr:         false,
			expectedAddress: "lb.example.com",
			expectedPort:    "443",
		},
		{
			name: "discovers LoadBalancer IP",
			existingObjs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ArgoCDAgentPrincipalServiceName,
						Namespace: namespace,
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
						Ports: []corev1.ServicePort{
							{
								Name:       "https",
								Port:       8443,
								TargetPort: intstr.FromInt(8443),
							},
						},
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "192.168.1.100",
								},
							},
						},
					},
				},
			},
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
				},
			},
			wantUpdated:     true,
			wantErr:         false,
			expectedAddress: "192.168.1.100",
			expectedPort:    "8443",
		},
		{
			name:         "fails when service not found",
			existingObjs: []runtime.Object{},
			gitOpsCluster: &appsv1alpha1.GitOpsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				Spec: appsv1alpha1.GitOpsClusterSpec{
					ArgoCDAgentAddon: appsv1alpha1.ArgoCDAgentAddonSpec{},
				},
			},
			wantUpdated: false,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &GitOpsClusterReconciler{
				Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.existingObjs...).Build(),
				Scheme: s,
			}

			updated, err := r.EnsureServerAddressAndPort(context.Background(), tt.gitOpsCluster, namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureServerAddressAndPort() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if updated != tt.wantUpdated {
				t.Errorf("EnsureServerAddressAndPort() updated = %v, want %v", updated, tt.wantUpdated)
			}

			if !tt.wantErr && tt.wantUpdated {
				if tt.gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerAddress != tt.expectedAddress {
					t.Errorf("PrincipalServerAddress = %v, want %v",
						tt.gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerAddress,
						tt.expectedAddress)
				}
				if tt.gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerPort != tt.expectedPort {
					t.Errorf("PrincipalServerPort = %v, want %v",
						tt.gitOpsCluster.Spec.ArgoCDAgentAddon.PrincipalServerPort,
						tt.expectedPort)
				}
			}
		})
	}
}
