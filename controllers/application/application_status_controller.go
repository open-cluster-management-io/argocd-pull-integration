/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicationlicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package application

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	workv1 "open-cluster-management.io/api/work/v1"
)

// ApplicationStatusReconciler reconciles a Application object
type ApplicationStatusReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=argoproj.io,resources=applications,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch

// ManifestWorkPredicateFunctions defines which ManifestWork this controller should watch
var ManifestWorkPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newManifestWork := e.ObjectNew.(*workv1.ManifestWork)
		return containsValidPullLabel(newManifestWork.Labels) && containsValidManifestWorkHubApplicationAnnotations(*newManifestWork)

	},
	CreateFunc: func(e event.CreateEvent) bool {
		manifestWork := e.Object.(*workv1.ManifestWork)
		return containsValidPullLabel(manifestWork.Labels) && containsValidManifestWorkHubApplicationAnnotations(*manifestWork)
	},

	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
}

// SetupWithManager sets up the controller with the Manager.
func (re *ApplicationStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&workv1.ManifestWork{}).
		WithEventFilter(ManifestWorkPredicateFunctions).
		Complete(re)
}

// Reconcile populates the Application status based on the associated ManifestWork's status feedback
// Reconcile populates the Application status based on the associated ManifestWork's status feedback
func (r *ApplicationStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling Application for status update..")
	defer log.Info("done reconciling Application for status update")

	var manifestWork workv1.ManifestWork
	if err := r.Get(ctx, req.NamespacedName, &manifestWork); err != nil {
		log.Error(err, "unable to fetch ManifestWork")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the ManifestWork is being deleted
	if manifestWork.ObjectMeta.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	// Extract status feedbacks for healthStatus and syncStatus
	resourceManifests := manifestWork.Status.ResourceStatus.Manifests
	healthStatus := ""
	syncStatus := ""
	if len(resourceManifests) > 0 {
		statusFeedbacks := resourceManifests[0].StatusFeedbacks.Values
		for _, statusFeedback := range statusFeedbacks {
			if statusFeedback.Name == "healthStatus" {
				healthStatus = *statusFeedback.Value.String
			}
			if statusFeedback.Name == "syncStatus" {
				syncStatus = *statusFeedback.Value.String
			}
		}
	}

	// If status values are missing, log and return
	if len(healthStatus) == 0 || len(syncStatus) == 0 {
		log.Info("healthStatus and syncStatus are both not in ManifestWork status feedback yet")
		return ctrl.Result{}, nil
	}

	// Retrieve Application reference from annotations
	applicationNamespace := manifestWork.Annotations[AnnotationKeyHubApplicationNamespace]
	applicationName := manifestWork.Annotations[AnnotationKeyHubApplicationName]

	// Fetch the Application as an unstructured object
	application := &unstructured.Unstructured{}
	application.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})

	if err := r.Get(ctx, types.NamespacedName{Namespace: applicationNamespace, Name: applicationName}, application); err != nil {
		log.Error(err, "unable to fetch Application")
		return ctrl.Result{}, err
	}

	// Update the Application's status fields dynamically
	if err := unstructured.SetNestedField(application.Object, healthStatus, "status", "health", "status"); err != nil {
		log.Error(err, "unable to set healthStatus in Application status")
		return ctrl.Result{}, err
	}
	if err := unstructured.SetNestedField(application.Object, syncStatus, "status", "sync", "status"); err != nil {
		log.Error(err, "unable to set syncStatus in Application status")
		return ctrl.Result{}, err
	}

	log.Info("updating Application status with ManifestWork status feedbacks")
	if err := r.Client.Update(ctx, application); err != nil {
		log.Error(err, "unable to update Application")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
