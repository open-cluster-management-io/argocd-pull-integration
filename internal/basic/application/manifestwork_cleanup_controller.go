/*
Copyright 2025.

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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	workv1 "open-cluster-management.io/api/work/v1"
)

// ManifestWorkCleanupReconciler periodically cleans up orphaned ManifestWorks
// whose parent Applications no longer exist
type ManifestWorkCleanupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Interval time.Duration
}

// Start begins the periodic cleanup process
func (r *ManifestWorkCleanupReconciler) Start(ctx context.Context) error {
	log := log.FromContext(ctx)
	log.Info("Starting ManifestWork cleanup controller", "interval", r.Interval.String())

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	// Run cleanup immediately on startup
	if err := r.cleanup(ctx); err != nil {
		log.Error(err, "initial cleanup failed")
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("Stopping ManifestWork cleanup controller")
			return nil
		case <-ticker.C:
			if err := r.cleanup(ctx); err != nil {
				log.Error(err, "periodic cleanup failed")
			}
		}
	}
}

// cleanup finds and deletes orphaned ManifestWorks
func (r *ManifestWorkCleanupReconciler) cleanup(ctx context.Context) error {
	log := log.FromContext(ctx)
	log.Info("Running periodic ManifestWork cleanup")

	// List all ManifestWorks that have the pull label
	var manifestWorkList workv1.ManifestWorkList
	if err := r.List(ctx, &manifestWorkList, client.MatchingLabels{
		LabelKeyPull: "true",
	}); err != nil {
		log.Error(err, "unable to list ManifestWorks")
		return err
	}

	log.Info("Found ManifestWorks with pull label", "count", len(manifestWorkList.Items))

	orphanedCount := 0
	deletedCount := 0

	// Check each ManifestWork to see if its parent Application still exists
	for _, work := range manifestWorkList.Items {
		if !containsValidManifestWorkHubApplicationAnnotations(work) {
			log.V(1).Info("ManifestWork missing required annotations, skipping", "name", work.Name, "namespace", work.Namespace)
			continue
		}

		annos := work.GetAnnotations()
		appNamespace := annos[AnnotationKeyHubApplicationNamespace]
		appName := annos[AnnotationKeyHubApplicationName]

		// Check if the Application still exists
		application := &unstructured.Unstructured{}
		application.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "argoproj.io",
			Version: "v1alpha1",
			Kind:    "Application",
		})

		err := r.Get(ctx, types.NamespacedName{Name: appName, Namespace: appNamespace}, application)
		if errors.IsNotFound(err) {
			// Application doesn't exist, this ManifestWork is orphaned
			orphanedCount++
			log.Info("Found orphaned ManifestWork", "manifestwork", work.Name, "namespace", work.Namespace, "application", appName)

			if err := r.Delete(ctx, &work); err != nil {
				if errors.IsNotFound(err) {
					// Already deleted, that's fine
					deletedCount++
					continue
				}
				log.Error(err, "unable to delete orphaned ManifestWork", "name", work.Name, "namespace", work.Namespace)
				continue
			}
			deletedCount++
			log.Info("Deleted orphaned ManifestWork", "manifestwork", work.Name, "namespace", work.Namespace)
		} else if err != nil {
			log.Error(err, "unable to check Application existence", "application", appName, "namespace", appNamespace)
			continue
		}
	}

	log.Info("Periodic ManifestWork cleanup completed", "orphaned", orphanedCount, "deleted", deletedCount)
	return nil
}
