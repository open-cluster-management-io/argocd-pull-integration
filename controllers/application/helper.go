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
	"k8s.io/apimachinery/pkg/runtime"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	appsetv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/applicationset/v1alpha1"
	workv1 "open-cluster-management.io/api/work/v1"
)

func containsValidPullLabel(application argov1alpha1.Application) bool {
	labels := application.GetLabels()
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

func containsValidPullAnnotation(application argov1alpha1.Application) bool {
	annos := application.GetAnnotations()
	if len(annos) == 0 {
		return false
	}

	managedClusterName, ok := annos[AnnotationKeyOCMManagedCluster]
	return ok && len(managedClusterName) > 0
}

// generateAppNamespace returns the intended namespace for the Application in the following priority
// 1) Annotation specified custom namespace
// 2) Application's namespace value
// 3) Fallsback to 'argocd'
func generateAppNamespace(application argov1alpha1.Application) string {
	annos := application.GetAnnotations()
	appNamespace := annos[AnnotationKeyOCMManagedClusterAppNamespace]
	if len(appNamespace) > 0 {
		return appNamespace
	}

	appNamespace = application.GetNamespace()
	if len(appNamespace) > 0 {
		return appNamespace
	}

	return "argocd" // TODO find the constant value from the argo API for this field
}

// generateManifestWorkName returns the ManifestWork name for a given application.
// It uses the Application name with the suffix of the first 5 characters of the UID
func generateManifestWorkName(application argov1alpha1.Application) string {
	return application.Name + "-" + string(application.UID)[0:5]
}

// getAppSetOwnerName returns the applicationSet resource name if the given application contains an applicationSet owner
func getAppSetOwnerName(application argov1alpha1.Application) string {
	if len(application.OwnerReferences) > 0 {
		for _, ownerRef := range application.OwnerReferences {
			if strings.EqualFold(ownerRef.APIVersion, appsetv1alpha1.GroupVersion.String()) &&
				strings.EqualFold(ownerRef.Kind, "ApplicationSet") { // TODO find the constant value from the argo appset API for this field
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
func prepareApplicationForWorkPayload(application argov1alpha1.Application) argov1alpha1.Application {
	newApp := &argov1alpha1.Application{
		TypeMeta: metav1.TypeMeta{
			APIVersion: argov1alpha1.SchemeGroupVersion.String(),
			Kind:       argov1alpha1.ApplicationSchemaGroupVersionKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  generateAppNamespace(application),
			Name:       application.Name,
			Finalizers: application.Finalizers,
		},
		Spec: application.Spec,
	}

	// empty the name
	newApp.Spec.Destination.Name = ""
	// always set for in-cluster destination
	newApp.Spec.Destination.Server = argov1alpha1.KubernetesInternalAPIServerAddr
	// copy the labels except for the ocm specific labels
	newApp.Labels = make(map[string]string)
	if len(application.Labels) > 0 {
		for key, value := range application.Labels {
			if key != LabelKeyPull {
				newApp.Labels[key] = value
			}
		}
	}
	// copy the annos except for the ocm specific annos
	newApp.Annotations = make(map[string]string)
	if len(application.Annotations) > 0 {
		for key, value := range application.Annotations {
			if key != AnnotationKeyOCMManagedCluster &&
				key != AnnotationKeyOCMManagedClusterAppNamespace {
				newApp.Annotations[key] = value
			}
		}
	}

	appSetOwnerName := getAppSetOwnerName(application)
	if appSetOwnerName != "" {
		newApp.Labels[LabelKeyAppSet] = strconv.FormatBool(true)
		newApp.Annotations[AnnotationKeyAppSet] = application.Namespace + "/" + appSetOwnerName
	}

	return *newApp
}

// generateManifestWork creates the ManifestWork that wraps the Application as payload
// With the status sync feedback of Application's health status and sync status.
// The Application payload Spec Destination values are modified so that the Application is always performing in-cluster resource deployments.
// If the Application is generated from an ApplicationSet, custom label and annotation are inserted.
func generateManifestWork(name, namespace string, application argov1alpha1.Application) *workv1.ManifestWork {
	var workLabels map[string]string
	var workAnnos map[string]string

	appSetOwnerName := getAppSetOwnerName(application)
	if appSetOwnerName != "" {
		workLabels = map[string]string{LabelKeyAppSet: strconv.FormatBool(true)}
		workAnnos = map[string]string{AnnotationKeyAppSet: application.Namespace + "/" + appSetOwnerName}
	}

	application = prepareApplicationForWorkPayload(application)

	return &workv1.ManifestWork{ // TODO use OCM API helper to generate manifest work.
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
						Group:     argov1alpha1.SchemeGroupVersion.Group,
						Resource:  "applications", // TODO find the constant value from the argo API for this field
						Namespace: application.Namespace,
						Name:      application.Name,
					},
					FeedbackRules: []workv1.FeedbackRule{
						{Type: workv1.JSONPathsType, JsonPaths: []workv1.JsonPath{{Name: "healthStatus", Path: ".status.health.status"}}},
						{Type: workv1.JSONPathsType, JsonPaths: []workv1.JsonPath{{Name: "syncStatus", Path: ".status.sync.status"}}},
					},
				},
			},
		},
	}
}
