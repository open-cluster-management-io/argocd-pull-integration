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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSetupInformersShutdownIsFast(t *testing.T) {
	r := &GitOpsClusterReconciler{}
	const namespace = "test-ns"
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "probe",
			Namespace: namespace,
		},
	})

	const runs = 2
	const maxPerRun = 5 * time.Second

	for i := 0; i < runs; i++ {
		start := time.Now()
		_, secretLister, stop, err := r.setupInformers(clientset, namespace)
		if err != nil {
			t.Fatalf("run %d: setupInformers() error: %v", i+1, err)
		}

		got, err := secretLister.Secrets(namespace).Get("probe")
		if err != nil {
			stop()
			t.Fatalf("run %d: secret lister Get(probe) error: %v", i+1, err)
		}
		if got.Name != "probe" {
			stop()
			t.Fatalf("run %d: secret lister Get(probe) name = %q, want probe", i+1, got.Name)
		}

		stop()
		elapsed := time.Since(start)
		t.Logf("run %d: setupInformers+lister+stop took %v", i+1, elapsed)
		if elapsed > maxPerRun {
			t.Fatalf("run %d: setupInformers+stop took %v (want <= %v)", i+1, elapsed, maxPerRun)
		}
	}
}
