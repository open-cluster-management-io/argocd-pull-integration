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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

//nolint:all
//go:embed charts/argocd-agent-addon/**
var ChartFS embed.FS

// ArgoCDAgentAddonReconciler reconciles an ArgoCD agent addon
type ArgoCDAgentAddonReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	Config                   *rest.Config
	Interval                 int
	ArgoCDOperatorImage      string
	ArgoCDAgentImage         string
	ArgoCDAgentServerAddress string
	ArgoCDAgentServerPort    string
	ArgoCDAgentMode          string
	Uninstall                bool
}

// SetupWithManager sets up the addon with the Manager
func SetupWithManager(mgr manager.Manager, interval int,
	argoCDOperatorImage, argoCDAgentImage, argoCDAgentServerAddress, argoCDAgentServerPort, argoCDAgentMode string, uninstall bool) error {
	reconciler := &ArgoCDAgentAddonReconciler{
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		Config:                   mgr.GetConfig(),
		Interval:                 interval,
		ArgoCDOperatorImage:      argoCDOperatorImage,
		ArgoCDAgentImage:         argoCDAgentImage,
		ArgoCDAgentServerAddress: argoCDAgentServerAddress,
		ArgoCDAgentServerPort:    argoCDAgentServerPort,
		ArgoCDAgentMode:          argoCDAgentMode,
		Uninstall:                uninstall,
	}

	return mgr.Add(reconciler)
}

// Start implements manager.Runnable and blocks until the context is cancelled
func (r *ArgoCDAgentAddonReconciler) Start(ctx context.Context) error {
	klog.Info("Starting ArgoCD Agent Addon controller")

	// Perform initial reconciliation
	r.reconcile(ctx)

	// Run periodic reconciliation until context is cancelled
	wait.UntilWithContext(ctx, r.reconcile, time.Duration(r.Interval)*time.Second)

	klog.Info("ArgoCD Agent Addon controller stopped")
	return nil
}

// reconcile performs the addon reconciliation logic
func (r *ArgoCDAgentAddonReconciler) reconcile(ctx context.Context) {
	klog.V(2).Info("Reconciling ArgoCD Agent Addon")

	if r.Uninstall {
		// Perform uninstall
		if err := r.uninstallArgoCDAgent(ctx); err != nil {
			klog.Errorf("Failed to uninstall ArgoCD Agent Addon: %v", err)
			// Continue running - will retry on next interval
			return
		}
		klog.V(2).Info("Successfully processed ArgoCD Agent Addon uninstall")
	} else {
		// Perform install/update
		if err := r.installOrUpdateArgoCDAgent(ctx); err != nil {
			klog.Errorf("Failed to reconcile ArgoCD Agent Addon: %v", err)
			// Continue running - will retry on next interval
			return
		}
		klog.V(2).Info("Successfully reconciled ArgoCD Agent Addon")
	}
}
