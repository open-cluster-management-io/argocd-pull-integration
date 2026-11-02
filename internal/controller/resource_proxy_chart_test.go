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
	"testing"

	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"
)

func TestResourceProxyControllerChart(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		proxy                   map[string]interface{}
		wantServer, wantTLSName string
	}{
		{name: "legacy reused chart values"},
		{
			name:  "TLS name without proxy origin",
			proxy: map[string]interface{}{"server": "", "tlsServerName": "principal.example.com"},
		},
		{
			name:       "proxy with normal hostname verification",
			proxy:      map[string]interface{}{"server": "https://proxy.example.com:9090"},
			wantServer: "https://proxy.example.com:9090",
		},
		{
			name:       "proxy with TLS name override",
			proxy:      map[string]interface{}{"server": "https://proxy.example.com:9090", "tlsServerName": "principal.example.com"},
			wantServer: "https://proxy.example.com:9090", wantTLSName: "principal.example.com",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart, err := loader.Load("../../charts/argocd-agent-addon")
			if err != nil {
				t.Fatal(err)
			}
			controllerValues := chart.Values["controller"].(map[string]interface{})
			// --reuse-values can retain old chart defaults without the new map.
			delete(controllerValues, "resourceProxy")
			if tc.proxy != nil {
				controllerValues["resourceProxy"] = tc.proxy
			}
			values, err := chartutil.ToRenderValues(chart, map[string]interface{}{}, chartutil.ReleaseOptions{Name: "test", Namespace: "argocd"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			files, err := engine.Render(chart, values)
			if err != nil {
				t.Fatal(err)
			}
			var deployment appsv1.Deployment
			if err := yaml.Unmarshal([]byte(files["argocd-agent-addon/templates/controller/deployment.yaml"]), &deployment); err != nil {
				t.Fatal(err)
			}
			env := map[string]string{}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "manager" {
					for _, entry := range container.Env {
						env[entry.Name] = entry.Value
					}
				}
			}
			for key, want := range map[string]string{ResourceProxyServerEnvVar: tc.wantServer, ResourceProxyTLSServerNameEnvVar: tc.wantTLSName} {
				got, exists := env[key]
				if got != want || exists != (want != "") {
					t.Fatalf("%s = %q (present=%v), want %q", key, got, exists, want)
				}
			}
		})
	}
}
