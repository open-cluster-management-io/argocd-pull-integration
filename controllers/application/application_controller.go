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
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	// Application annotation that dictates which managed cluster this Application should be pulled to
	AnnotationKeyOCMManagedCluster = "argocd.argoproj.io/ocm-managed-cluster"
	// Application annotation that dictates which managed cluster namespace this Application should be pulled to
	AnnotationKeyOCMManagedClusterAppNamespace = "argocd.argoproj.io/ocm-managed-cluster-app-namespace"
	// Application and ManifestWork annotation that shows which ApplicationSet is the grand parent of this work
	AnnotationKeyAppSet = "apps.open-cluster-management.io/hosting-applicationset"
	// Application and ManifestWork label that shows that ApplicationSet is the grand parent of this work
	LabelKeyAppSet = "apps.open-cluster-management.io/application-set"
	// Application label that enables the pull controller to wrap the Application in ManifestWork payload
	LabelKeyPull = "argocd.argoproj.io/pull-to-ocm-managed-cluster"
)

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=argoproj.io,resources=applications,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete

// ApplicationPredicateFunctions defines which Application this controller should wrap inside ManifestWork's payload
var ApplicationPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newApp := e.ObjectNew.(*argov1alpha1.Application)
		return containsValidPullLabel(*newApp) && containsValidPullAnnotation(*newApp)

	},
	CreateFunc: func(e event.CreateEvent) bool {
		app := e.Object.(*argov1alpha1.Application)
		return containsValidPullLabel(*app) && containsValidPullAnnotation(*app)
	},

	DeleteFunc: func(e event.DeleteEvent) bool {
		app := e.Object.(*argov1alpha1.Application)
		return containsValidPullLabel(*app) && containsValidPullAnnotation(*app)
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&argov1alpha1.Application{}).
		WithEventFilter(ApplicationPredicateFunctions).
		Complete(r)
}

// Reconcile create/update/delete ManifestWork with the Application as its payload
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling Application...")

	var application argov1alpha1.Application
	if err := r.Get(ctx, req.NamespacedName, &application); err != nil {
		log.Error(err, "unable to fetch Application")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	managedClusterName := application.GetAnnotations()[AnnotationKeyOCMManagedCluster]
	mwName := generateManifestWorkName(application)

	// the Application is being deleted, find the ManifestWork and delete that as well
	if application.ObjectMeta.DeletionTimestamp != nil {
		// remove finalizer from Application but do not 'commit' yet
		if len(application.Finalizers) != 0 {
			f := application.GetFinalizers()
			for i := 0; i < len(f); i++ {
				if f[i] == argov1alpha1.ResourcesFinalizerName {
					f = append(f[:i], f[i+1:]...)
					i--
				}
			}
			application.SetFinalizers(f)
		}

		// delete the ManifestWork associated with this Application
		var work workv1.ManifestWork
		err := r.Get(ctx, types.NamespacedName{Name: mwName, Namespace: managedClusterName}, &work)
		if errors.IsNotFound(err) {
			// already deleted ManifestWork, commit the Application finalizer removal
			if err = r.Update(ctx, &application); err != nil {
				log.Error(err, "unable to update Application")
				return ctrl.Result{}, err
			}
		} else if err != nil {
			log.Error(err, "unable to fetch ManifestWork")
			return ctrl.Result{}, err
		}

		if err := r.Delete(ctx, &work); err != nil {
			log.Error(err, "unable to delete ManifestWork")
			return ctrl.Result{}, err
		}

		// deleted ManifestWork, commit the Application finalizer removal
		if err := r.Update(ctx, &application); err != nil {
			log.Error(err, "unable to update Application")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// verify the ManagedCluster actually exists
	var managedCluster clusterv1.ManagedCluster
	if err := r.Get(ctx, types.NamespacedName{Name: managedClusterName}, &managedCluster); err != nil {
		log.Error(err, "unable to fetch ManagedCluster")
		return ctrl.Result{}, err
	}

	log.Info("generating ManifestWork for Application")
	w := generateManifestWork(mwName, managedClusterName, application)

	// create or update the ManifestWork depends if it already exists or not
	var mw workv1.ManifestWork
	err := r.Get(ctx, types.NamespacedName{Name: mwName, Namespace: managedClusterName}, &mw)
	if errors.IsNotFound(err) {
		err = r.Client.Create(ctx, w)
		if err != nil {
			log.Error(err, "unable to create ManifestWork")
			return ctrl.Result{}, err
		}
	} else if err == nil {
		app := prepareApplicationForWorkPayload(application)
		mw.Spec.Workload.Manifests = []workv1.Manifest{{RawExtension: runtime.RawExtension{Object: &app}}}
		err = r.Client.Update(ctx, &mw)
		if err != nil {
			log.Error(err, "unable to update ManifestWork")
			return ctrl.Result{}, err
		}
	} else {
		log.Error(err, "unable to fetch ManifestWork")
		return ctrl.Result{}, err
	}

	log.Info("done reconciling Application")

	return ctrl.Result{}, nil
}
