/*
Copyright 2023.

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

package application

import (
	"context"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

var _ = Describe("Application Pull controller", func() {

	const (
		appName      = "app-5"
		workName     = "work-1"
		appNamespace = "default"
		clusterName  = "default"
	)

	appKey := types.NamespacedName{Name: appName, Namespace: appNamespace}
	workKey := types.NamespacedName{Name: workName, Namespace: clusterName}
	ctx := context.Background()

	Context("When ManifestWork is created/updated", func() {
		It("Should update Application status", func() {
			By("Creating the Application")
			app1 := &unstructured.Unstructured{}
			app1.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			app1.SetName(appName)
			app1.SetNamespace(appNamespace)
			app1.Object["spec"] = map[string]interface{}{
				"project": "default", // Required field
				"source": map[string]interface{}{
					"repoURL": "default",
				},
				"destination": map[string]interface{}{ // Required field
					"server": KubernetesInternalAPIServerAddr,
				},
			}
			Expect(k8sClient.Create(ctx, app1)).Should(Succeed())

			By("Creating the ManifestWork")
			work1 := workv1.ManifestWork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: clusterName,
					Annotations: map[string]string{
						AnnotationKeyHubApplicationNamespace: appNamespace,
						AnnotationKeyHubApplicationName:      appName,
					},
					Labels: map[string]string{
						LabelKeyPull: strconv.FormatBool(true),
					},
				},
			}
			Expect(k8sClient.Create(ctx, &work1)).Should(Succeed())
			Expect(k8sClient.Get(ctx, workKey, &work1)).Should(Succeed())

			By("Updating the ManifestWork status")
			healthy := "Healthy"
			synced := "Synced"
			work1.Status = workv1.ManifestWorkStatus{
				ResourceStatus: workv1.ManifestResourceStatus{
					Manifests: []workv1.ManifestCondition{{
						Conditions: []metav1.Condition{{
							Type:               workv1.WorkApplied,
							Status:             metav1.ConditionTrue,
							Reason:             workv1.WorkApplied,
							Message:            workv1.WorkApplied,
							LastTransitionTime: work1.CreationTimestamp,
						}},
						StatusFeedbacks: workv1.StatusFeedbackResult{
							Values: []workv1.FeedbackValue{
								{Name: "healthStatus", Value: workv1.FieldValue{String: &healthy, Type: "String"}},
								{Name: "syncStatus", Value: workv1.FieldValue{String: &synced, Type: "String"}},
							}},
					}},
				},
			}
			Expect(k8sClient.Status().Update(ctx, &work1)).Should(Succeed())

			// Verify that the Application status is updated
			Eventually(func() bool {
				app := &unstructured.Unstructured{}
				app.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "argoproj.io",
					Version: "v1alpha1",
					Kind:    "Application",
				})
				if err := k8sClient.Get(ctx, appKey, app); err != nil {
					return false
				}

				// Get the health and sync status from the Application's status
				healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
				syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")

				return healthStatus == "Healthy" && syncStatus == "Synced"
			}).Should(BeTrue())
		})
	})
})
