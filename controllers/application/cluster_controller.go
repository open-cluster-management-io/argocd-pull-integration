/*
Copyright 2024.

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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// ClusterReconciler reconciles a ManagedCluster object
type ClusterReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ArgoCDNamespace string
}

//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;delete

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.ManagedCluster{}).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool { return false },
		}).
		Complete(r)
}

// Reconcile create/delete Secrets to the corresponding ManagedCluster resources
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling ManagedCluster...")

	managedCluster := &clusterv1.ManagedCluster{}
	err := r.Get(ctx, req.NamespacedName, managedCluster)

	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch ManagedCluster")
			return ctrl.Result{}, err
		}
		// If the ManagedCluster resource is not found, it has been deleted.
		// Proceed with cleaning up the associated Secret.
		log.Info("ManagedCluster resource not found, cleaning up Secret")
		if cleanupErr := r.deleteSecret(ctx, req.Name); cleanupErr != nil {
			log.Error(cleanupErr, "unable to delete Secret")
			return ctrl.Result{}, cleanupErr
		}
		return ctrl.Result{}, nil
	}

	// If the ManagedCluster is being deleted, clean up the Secret
	if !managedCluster.GetDeletionTimestamp().IsZero() {
		log.Info("ManagedCluster is being deleted, cleaning up Secret")
		if cleanupErr := r.deleteSecret(ctx, req.Name); cleanupErr != nil {
			log.Error(cleanupErr, "unable to delete Secret")
			return ctrl.Result{}, cleanupErr
		}
		return ctrl.Result{}, nil
	}

	// ManagedCluster is created, ensure the Secret exists
	log.Info("ManagedCluster is created, ensuring Secret exists")
	if createErr := r.createSecret(ctx, managedCluster); createErr != nil {
		log.Error(createErr, "unable to create Secret")
		return ctrl.Result{}, createErr
	}

	log.Info("done reconciling ManagedCluster")
	return ctrl.Result{}, nil
}

// createSecret creates a Secret for the given ManagedCluster
func (r *ClusterReconciler) createSecret(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	secret := &corev1.Secret{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      fmt.Sprintf("%s-secret", cluster.Name),
			Namespace: r.ArgoCDNamespace,
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type": "cluster",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"name":   cluster.Name,
			"server": fmt.Sprintf("https://%s-control-plane:6443", cluster.Name),
		},
	}

	existingSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, existingSecret)
	if err == nil {
		return nil
	}
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	if err := r.Create(ctx, secret); err != nil {
		return err
	}

	return nil
}

// deleteSecret deletes the Secret associated with a ManagedCluster
func (r *ClusterReconciler) deleteSecret(ctx context.Context, clusterName string) error {
	secretName := fmt.Sprintf("%s-secret", clusterName)
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: r.ArgoCDNamespace}, secret)
	if err != nil {
		return client.IgnoreNotFound(err)
	}

	if err := r.Delete(ctx, secret); err != nil {
		return err
	}

	return nil
}
