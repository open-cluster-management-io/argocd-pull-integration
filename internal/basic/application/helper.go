/*
Copyright 2022.

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
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	// KubernetesInternalAPIServerAddr is address of the k8s API server when accessing internal to the cluster
	KubernetesInternalAPIServerAddr = "https://kubernetes.default.svc"
)

func containsValidPullLabel(labels map[string]string) bool {
	if len(labels) == 0 {
		return false
	}

	if pullLabelStr, ok := labels[LabelKeyPull]; ok {
		isPull, err := strconv.ParseBool(pullLabelStr)
		if err != nil {
			return false
		}
		return isPull
	}

	return false
}

func containsValidPullAnnotation(annos map[string]string) bool {
	if len(annos) == 0 {
		return false
	}

	managedClusterName, ok := annos[AnnotationKeyOCMManagedCluster]
	return ok && len(managedClusterName) > 0
}

func containsValidManifestWorkHubApplicationAnnotations(manifestWork workv1.ManifestWork) bool {
	annos := manifestWork.GetAnnotations()
	if len(annos) == 0 {
		return false
	}

	namespace, ok := annos[AnnotationKeyHubApplicationNamespace]
	name, ok2 := annos[AnnotationKeyHubApplicationName]

	return ok && ok2 && len(namespace) > 0 && len(name) > 0
}

// generateAppNamespace returns the intended namespace for the Application in the following priority
// 1) Annotation specified custom namespace
// 2) Application's namespace value
// 3) Fallsback to 'argocd'
func generateAppNamespace(namespace string, annos map[string]string) string {
	appNamespace := annos[AnnotationKeyOCMManagedClusterAppNamespace]
	if len(appNamespace) > 0 {
		return appNamespace
	}
	appNamespace = namespace
	if len(appNamespace) > 0 {
		return appNamespace
	}
	return "argocd"
}

// generateManifestWorkName returns the ManifestWork name for a given application.
// It uses the Application name with the suffix of the first 5 characters of the UID
func generateManifestWorkName(name string, uid types.UID) string {
	return name + "-" + string(uid)[0:5]
}

// getAppSetOwnerName returns the applicationSet resource name if the given application contains an applicationSet owner
func getAppSetOwnerName(ownerRefs []metav1.OwnerReference) string {
	if len(ownerRefs) > 0 {
		for _, ownerRef := range ownerRefs {
			if strings.EqualFold(ownerRef.APIVersion, "argoproj.io/v1alpha1") &&
				strings.EqualFold(ownerRef.Kind, "ApplicationSet") {
				return ownerRef.Name
			}
		}
	}
	return ""
}

// prepareApplicationForWorkPayload modifies the Application:
// - reset the meta
// - set the namespace value
// - ensures the Application Destination is set to in-cluster resource deployment
// - ensures the operation field is also set if present
func prepareApplicationForWorkPayload(application *unstructured.Unstructured) unstructured.Unstructured {
	newApp := &unstructured.Unstructured{}
	newApp.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	newApp.SetNamespace(generateAppNamespace(application.GetNamespace(), application.GetAnnotations()))
	newApp.SetName(application.GetName())
	newApp.SetFinalizers(application.GetFinalizers())

	// set the operation field
	if operation, ok := application.Object["operation"].(map[string]interface{}); ok {
		newApp.Object["operation"] = operation
	}

	// set the spec field
	if newSpec, ok := application.Object["spec"].(map[string]interface{}); ok {
		if destination, ok := newSpec["destination"].(map[string]interface{}); ok {
			// empty the name
			destination["name"] = ""
			// always set for in-cluster destination
			destination["server"] = KubernetesInternalAPIServerAddr
		}
		newApp.Object["spec"] = newSpec
	}

	// copy the labels except for the ocm specific labels
	labels := make(map[string]string)
	for key, value := range application.GetLabels() {
		if key != LabelKeyPull {
			labels[key] = value
		}
	}
	newApp.SetLabels(labels)

	// copy the annos except for the ocm specific annos
	annotations := make(map[string]string)
	for key, value := range application.GetAnnotations() {
		if key != AnnotationKeyOCMManagedCluster &&
			key != AnnotationKeyOCMManagedClusterAppNamespace &&
			key != AnnotationKeyAppSkipReconcile {
			annotations[key] = value
		}
	}
	newApp.SetAnnotations(annotations)

	appSetOwnerName := getAppSetOwnerName(application.GetOwnerReferences())
	if appSetOwnerName != "" {
		labels[LabelKeyAppSet] = strconv.FormatBool(true)
		annotations[AnnotationKeyAppSet] = application.GetNamespace() + "/" + appSetOwnerName
		newApp.SetLabels(labels)
		newApp.SetAnnotations(annotations)
	}

	return *newApp
}

// generateManifestWork creates the ManifestWork that wraps the Application as payload
// With the status sync feedback of Application's health status and sync status.
// The Application payload Spec Destination values are modified so that the Application is always performing in-cluster resource deployments.
// If the Application is generated from an ApplicationSet, custom label and annotation are inserted.
func generateManifestWork(name, namespace string, app *unstructured.Unstructured) *workv1.ManifestWork {
	workLabels := map[string]string{
		LabelKeyPull: strconv.FormatBool(true),
	}

	workAnnos := map[string]string{
		AnnotationKeyHubApplicationNamespace: app.GetNamespace(),
		AnnotationKeyHubApplicationName:      app.GetName(),
	}

	appSetOwnerName := getAppSetOwnerName(app.GetOwnerReferences())
	if appSetOwnerName != "" {
		workLabels[LabelKeyAppSet] = strconv.FormatBool(true)
		workAnnos[AnnotationKeyAppSet] = app.GetNamespace() + "/" + appSetOwnerName
	}

	application := prepareApplicationForWorkPayload(app)

	return &workv1.ManifestWork{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      workLabels,
			Annotations: workAnnos,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{{RawExtension: runtime.RawExtension{Object: &application}}},
			},
			ManifestConfigs: []workv1.ManifestConfigOption{
				{
					ResourceIdentifier: workv1.ResourceIdentifier{
						Group:     "argoproj.io",
						Resource:  "applications",
						Namespace: application.GetNamespace(),
						Name:      application.GetName(),
					},
					FeedbackRules: []workv1.FeedbackRule{
						{Type: workv1.JSONPathsType, JsonPaths: []workv1.JsonPath{{Name: "healthStatus", Path: ".status.health.status"}}},
						{Type: workv1.JSONPathsType, JsonPaths: []workv1.JsonPath{{Name: "syncStatus", Path: ".status.sync.status"}}},
						{Type: workv1.JSONPathsType, JsonPaths: []workv1.JsonPath{{Name: "operationStateStartedAt", Path: ".status.operationState.startedAt"}}},
						{Type: workv1.JSONPathsType, JsonPaths: []workv1.JsonPath{{Name: "operationStatePhase", Path: ".status.operationState.phase"}}},
						{Type: workv1.JSONPathsType, JsonPaths: []workv1.JsonPath{{Name: "syncRevision", Path: ".status.sync.revision"}}},
					},
					UpdateStrategy: &workv1.UpdateStrategy{
						Type: workv1.UpdateStrategyTypeServerSideApply,
						ServerSideApply: &workv1.ServerSideApplyConfig{
							Force: true,
							IgnoreFields: []workv1.IgnoreField{
								{
									Condition: workv1.IgnoreFieldsConditionOnSpokeChange,
									JSONPaths: []string{".operation"},
								},
							},
						},
					},
				},
			},
		},
	}
}
