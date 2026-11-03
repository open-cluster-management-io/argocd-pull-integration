/*
Copyright 2026 Open Cluster Management.

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
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var _ = Describe("Resource Proxy cluster registration", func() {
	It("reapplies configured endpoints and preserves two cluster identities after restart", func(ctx SpecContext) {
		const namespace = "resource-proxy-test"
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())
		r := &GitOpsClusterReconciler{Client: k8sClient, Config: cfg}
		Expect(r.EnsureArgoCDAgentCASecret(ctx, namespace)).To(Succeed())

		clientCerts := map[string]string{}
		for _, endpoint := range []string{"https://proxy.example.com:9090", "https://proxy.example.com:9090", "https://replacement.example.com:9090"} {
			// Reconstruct the reconciler as a restarted controller would.
			r = &GitOpsClusterReconciler{
				Client: k8sClient, Config: cfg,
				ResourceProxyServer:        endpoint,
				ResourceProxyTLSServerName: "principal.example.com",
			}
			urls := map[string]bool{}
			for _, clusterName := range []string{"cluster-a", "cluster-b"} {
				cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName}}
				Expect(r.importManagedClusterToArgoCD(ctx, namespace, cluster, "placement")).To(Succeed())
				secret := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "cluster-" + clusterName}, secret)).To(Succeed())
				Expect(string(secret.Data["name"])).To(Equal(clusterName))
				Expect(secret.Labels[ArgoCDAgentClusterMappingLabel]).To(Equal(clusterName))
				server, err := url.Parse(string(secret.Data["server"]))
				Expect(err).NotTo(HaveOccurred())
				Expect(server.Scheme + "://" + server.Host).To(Equal(endpoint))
				Expect(server.Query().Get("agentName")).To(Equal(clusterName))
				Expect(urls[server.String()]).To(BeFalse())
				urls[server.String()] = true
				var config ClusterConfig
				Expect(json.Unmarshal(secret.Data["config"], &config)).To(Succeed())
				Expect(config.Username).To(Equal(clusterName))
				Expect(config.TLSClientConfig).NotTo(BeNil())
				Expect(config.TLSClientConfig.Insecure).To(BeFalse())
				Expect(config.TLSClientConfig.ServerName).To(Equal("principal.example.com"))
				Expect(len(config.TLSClientConfig.CertData)).To(BeNumerically(">", 0))
				Expect(len(config.TLSClientConfig.KeyData)).To(BeNumerically(">", 0))
				Expect(len(config.TLSClientConfig.CAData)).To(BeNumerically(">", 0))
				if previous := clientCerts[clusterName]; previous != "" {
					Expect(string(config.TLSClientConfig.CertData) == previous).To(BeTrue())
				}
				clientCerts[clusterName] = string(config.TLSClientConfig.CertData)

				// A drifted generated Secret should be restored from desired settings.
				secret.Data["server"] = []byte("https://manual-override.invalid")
				Expect(k8sClient.Update(ctx, secret)).To(Succeed())
			}
		}
		Expect(clientCerts["cluster-a"] == clientCerts["cluster-b"]).To(BeFalse())
	})
})
