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

package addon

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	k8syaml "sigs.k8s.io/yaml"
)

// templateAndApplyChart templates a Helm chart and applies the rendered manifests
func (r *ArgoCDAgentAddonReconciler) templateAndApplyChart(ctx context.Context, chartPath, operatorNamespace, argoCDNamespace, releaseName string) error {
	klog.Infof("Templating and applying chart %s in namespace %s", releaseName, operatorNamespace)

	// Create temp directory for chart files
	tempDir, err := os.MkdirTemp("", "helm-chart-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Copy embedded chart files to temp directory
	err = r.copyEmbeddedToTemp(ChartFS, chartPath, tempDir)
	if err != nil {
		return fmt.Errorf("failed to copy files: %v", err)
	}

	// Load the chart
	chart, err := loader.Load(tempDir)
	if err != nil {
		return fmt.Errorf("failed to load chart: %v", err)
	}

	// Prepare values for templating
	values := map[string]interface{}{}

	// Parse the operator image
	operatorImageRepo, operatorImageTag, err := ParseImageReference(r.OperatorImage)
	if err != nil {
		return fmt.Errorf("failed to parse operator image: %w", err)
	}

	// Set up global values for argocd-agent-addon chart
	argocdAgent := map[string]interface{}{
		"mode": r.ArgoCDAgentMode,
	}

	if r.ArgoCDAgentServerAddress != "" {
		argocdAgent["serverAddress"] = r.ArgoCDAgentServerAddress
	}
	if r.ArgoCDAgentServerPort != "" {
		argocdAgent["serverPort"] = r.ArgoCDAgentServerPort
	}

	global := map[string]interface{}{
		"argoCDAgent":             argocdAgent,
		"argoCDOperatorNamespace": operatorNamespace,
		"argoCDNamespace":         argoCDNamespace,
	}
	values["global"] = global

	// Set operator image at top level for template access
	values["operatorImage"] = operatorImageRepo
	values["operatorImageTag"] = operatorImageTag

	// Set up release options
	options := chartutil.ReleaseOptions{
		Name:      releaseName,
		Namespace: operatorNamespace,
	}

	// Prepare values to render
	valuesToRender, err := chartutil.ToRenderValues(chart, values, options, nil)
	if err != nil {
		return fmt.Errorf("failed to prepare chart values: %v", err)
	}

	// Render the chart templates
	files, err := engine.Engine{}.Render(chart, valuesToRender)
	if err != nil {
		return fmt.Errorf("failed to render chart templates: %v", err)
	}

	// Apply each rendered manifest
	for name, content := range files {
		if len(strings.TrimSpace(content)) == 0 || strings.HasSuffix(name, "NOTES.txt") {
			continue
		}

		yamlDocs := strings.Split(content, "\n---\n")
		for _, doc := range yamlDocs {
			doc = strings.TrimSpace(doc)
			if len(doc) == 0 {
				continue
			}

			var obj unstructured.Unstructured
			if err := k8syaml.Unmarshal([]byte(doc), &obj); err != nil {
				klog.Warningf("Failed to parse YAML document in %s: %v", name, err)
				continue
			}

			if obj.GetKind() == "" || obj.GetName() == "" {
				continue
			}

			if obj.GetNamespace() == "" && obj.GetKind() != "Namespace" && obj.GetKind() != "ClusterRole" && obj.GetKind() != "ClusterRoleBinding" {
				if obj.GetKind() == "ArgoCD" {
					obj.SetNamespace(argoCDNamespace)
				} else {
					obj.SetNamespace(operatorNamespace)
				}
			}

			if err := r.applyManifest(ctx, &obj); err != nil {
				klog.Errorf("Failed to apply manifest %s/%s %s: %v",
					obj.GetKind(), obj.GetName(), obj.GetNamespace(), err)
			}
		}
	}

	klog.Infof("Successfully templated and applied chart %s in operator namespace %s and argocd namespace %s", releaseName, operatorNamespace, argoCDNamespace)
	return nil
}

// copyEmbeddedToTemp copies embedded chart files to a temporary directory
func (r *ArgoCDAgentAddonReconciler) copyEmbeddedToTemp(fs embed.FS, srcPath, destPath string) error {
	entries, err := fs.ReadDir(srcPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcEntryPath := filepath.Join(srcPath, entry.Name())
		destEntryPath := filepath.Join(destPath, entry.Name())

		if entry.IsDir() {
			if err := os.MkdirAll(destEntryPath, 0750); err != nil {
				return err
			}
			if err := r.copyEmbeddedToTemp(fs, srcEntryPath, destEntryPath); err != nil {
				return err
			}
		} else {
			data, err := fs.ReadFile(srcEntryPath)
			if err != nil {
				return err
			}

			err = os.WriteFile(destEntryPath, data, 0600)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// applyManifest applies a Kubernetes manifest
func (r *ArgoCDAgentAddonReconciler) applyManifest(ctx context.Context, obj *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(obj.GetAPIVersion())
	existing.SetKind(obj.GetKind())

	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}

	err := r.Get(ctx, key, existing)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err == nil {
		annotations := existing.GetAnnotations()
		if annotations != nil && annotations["argocd-addon.open-cluster-management.io/skip"] == "true" {
			klog.V(1).Infof("Skipping %s/%s %s due to skip annotation", obj.GetKind(), obj.GetName(), obj.GetNamespace())
			return nil
		}
	}

	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app.kubernetes.io/managed-by"] = "argocd-agent-addon"
	obj.SetLabels(labels)

	if err != nil && errors.IsNotFound(err) {
		klog.V(1).Infof("Creating %s/%s %s", obj.GetKind(), obj.GetName(), obj.GetNamespace())
		return r.Create(ctx, obj)
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	klog.V(1).Infof("Updating %s/%s %s", obj.GetKind(), obj.GetName(), obj.GetNamespace())
	return r.Update(ctx, obj)
}

// applyCRDIfNotExists applies a CRD only if it doesn't already exist
func (r *ArgoCDAgentAddonReconciler) applyCRDIfNotExists(ctx context.Context, resource, apiVersion, yamlFilePath string) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.Config)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %v", err)
	}

	apiResourceList, err := discoveryClient.ServerResourcesForGroupVersion(apiVersion)
	if err == nil {
		for _, apiResource := range apiResourceList.APIResources {
			if apiResource.Name == resource {
				klog.Infof("CRD %s already exists, skipping installation", yamlFilePath)
				return nil
			}
		}
	}

	klog.Infof("Installing CRD %s", yamlFilePath)

	crdData, err := ChartFS.ReadFile(yamlFilePath)
	if err != nil {
		return fmt.Errorf("failed to read CRD file %s: %v", yamlFilePath, err)
	}

	yamlDocs := strings.Split(string(crdData), "\n---\n")
	for _, doc := range yamlDocs {
		doc = strings.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		var crd apiextensionsv1.CustomResourceDefinition
		if err := k8syaml.Unmarshal([]byte(doc), &crd); err != nil {
			klog.Warningf("Failed to unmarshal CRD document: %v", err)
			continue
		}

		if crd.Kind != "CustomResourceDefinition" {
			continue
		}

		annotations := crd.GetAnnotations()
		if annotations != nil && annotations["argocd-addon.open-cluster-management.io/skip"] == "true" {
			klog.Infof("Skipping CRD %s due to skip annotation", crd.Name)
			continue
		}

		crdLabels := crd.GetLabels()
		if crdLabels == nil {
			crdLabels = make(map[string]string)
		}
		crdLabels["app.kubernetes.io/managed-by"] = "argocd-agent-addon"
		crd.SetLabels(crdLabels)

		err = r.Create(ctx, &crd)
		if err != nil && !errors.IsAlreadyExists(err) {
			klog.Errorf("Failed to create CRD %s: %v", crd.Name, err)
			return err
		}

		klog.V(1).Infof("Successfully installed CRD %s", crd.Name)
	}

	return nil
}
