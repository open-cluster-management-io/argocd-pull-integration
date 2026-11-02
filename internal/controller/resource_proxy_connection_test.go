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
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestResourceProxyServerURL(t *testing.T) {
	for _, tc := range []struct {
		name, server, want string
		wantErr            bool
	}{
		{name: "legacy", want: "https://cluster-a-control-plane"},
		{name: "service", server: "https://proxy.argocd.svc:9090", want: "https://proxy.argocd.svc:9090?agentName=cluster-a"},
		{name: "default TLS port", server: "https://proxy.example.com", want: "https://proxy.example.com?agentName=cluster-a"},
		{name: "IPv6", server: "https://[::1]:9090", want: "https://[::1]:9090?agentName=cluster-a"},
		{name: "plain HTTP", server: "http://proxy:9090", wantErr: true},
		{name: "missing scheme", server: "proxy:9090", wantErr: true},
		{name: "missing host", server: "https:///", wantErr: true},
		{name: "credentials", server: "https://user:password@proxy:9090", wantErr: true},
		{name: "path prefix", server: "https://proxy:9090/prefix", wantErr: true},
		{name: "query", server: "https://proxy:9090?agentName=other", wantErr: true},
		{name: "fragment", server: "https://proxy:9090#fragment", wantErr: true},
		{name: "invalid port", server: "https://proxy:bad", wantErr: true},
		{name: "port out of range", server: "https://proxy:65536", wantErr: true},
		{name: "zero port", server: "https://proxy:0", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resourceProxyServerURL(tc.server, "cluster-a")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("server URL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClusterConnectionDataPreservesClusterIdentity(t *testing.T) {
	r := &GitOpsClusterReconciler{
		ResourceProxyServer:        "https://proxy.argocd.svc:9090",
		ResourceProxyTLSServerName: "principal.argocd.svc",
	}
	servers := map[string]bool{}
	for _, clusterName := range []string{"cluster-a", "cluster-b"} {
		cert, key, ca := []byte(clusterName+"-cert"), []byte(clusterName+"-key"), []byte("test-ca")
		data, err := r.clusterConnectionData(clusterName, cert, key, ca)
		if err != nil {
			t.Fatal(err)
		}
		if string(data["name"]) != clusterName {
			t.Fatal("cluster name changed")
		}
		server, err := url.Parse(string(data["server"]))
		if err != nil {
			t.Fatal(err)
		}
		if servers[server.String()] || server.Query().Get("agentName") != clusterName {
			t.Fatal("cluster registration URLs must remain distinct and identify their agent")
		}
		servers[server.String()] = true
		var config ClusterConfig
		if err := json.Unmarshal(data["config"], &config); err != nil {
			t.Fatal(err)
		}
		if config.Username != clusterName || config.Password != clusterName {
			t.Fatal("cluster credentials changed")
		}
		tls := config.TLSClientConfig
		if tls == nil || tls.Insecure || tls.ServerName != "principal.argocd.svc" ||
			!bytes.Equal(tls.CertData, cert) || !bytes.Equal(tls.KeyData, key) || !bytes.Equal(tls.CAData, ca) {
			t.Fatal("TLS verification, server name or client certificate data changed")
		}
		// The same desired connection must survive repeated generation, including
		// after a controller restart with the same configuration.
		for i := 0; i < 2; i++ {
			restarted := &GitOpsClusterReconciler{
				ResourceProxyServer:        r.ResourceProxyServer,
				ResourceProxyTLSServerName: r.ResourceProxyTLSServerName,
			}
			again, err := restarted.clusterConnectionData(clusterName, cert, key, ca)
			if err != nil || !reflect.DeepEqual(data, again) {
				t.Fatalf("connection data changed on repeated generation: %v", err)
			}
		}
	}
}

func TestClusterConnectionDataOverrideChangeAndRemoval(t *testing.T) {
	r := &GitOpsClusterReconciler{}
	for _, server := range []string{"https://first.example.com:9090", "https://second.example.com:9090", ""} {
		r.ResourceProxyServer = server
		data, err := r.clusterConnectionData("cluster-a", []byte("cert"), []byte("key"), []byte("ca"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := resourceProxyServerURL(server, "cluster-a")
		if err != nil {
			t.Fatal(err)
		}
		if string(data["server"]) != want {
			t.Fatalf("server = %q, want %q", data["server"], want)
		}
		var config map[string]json.RawMessage
		if err := json.Unmarshal(data["config"], &config); err != nil {
			t.Fatal(err)
		}
		var tls map[string]json.RawMessage
		if err := json.Unmarshal(config["tlsClientConfig"], &tls); err != nil {
			t.Fatal(err)
		}
		if _, exists := tls["serverName"]; exists {
			t.Fatal("empty serverName must retain normal URL hostname verification")
		}
	}
	r.ResourceProxyServer = "http://insecure.example.com"
	if _, err := r.clusterConnectionData("cluster-a", nil, nil, nil); err == nil {
		t.Fatal("invalid endpoint must not produce Secret data")
	}
}
