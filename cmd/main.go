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

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"

	appsv1alpha1 "open-cluster-management.io/argocd-pull-integration/api/v1alpha1"
	"open-cluster-management.io/argocd-pull-integration/internal/addon"
	"open-cluster-management.io/argocd-pull-integration/internal/controller"
	// +kubebuilder:scaffold:imports
)

// AddonOptions for addon mode command line flag parsing
type AddonOptions struct {
	SyncInterval                int
	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration
}

var (
	addonOptions = AddonOptions{
		SyncInterval:                60,
		LeaderElectionLeaseDuration: 15 * time.Second,
		LeaderElectionRenewDeadline: 10 * time.Second,
		LeaderElectionRetryPeriod:   2 * time.Second,
	}

	// Default values for ArgoCD Agent and Operator
	ArgoCDOperatorImage      = ""
	ArgoCDAgentImage         = ""
	ArgoCDAgentServerAddress = ""
	ArgoCDAgentServerPort    = ""
	ArgoCDAgentMode          = "managed"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(addonv1alpha1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	utilruntime.Must(workv1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	utilruntime.Must(appsv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var mode string
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)

	flag.StringVar(&mode, "mode", "controller", "Mode to run: controller, addon, or cleanup")
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	// Addon mode flags
	flag.IntVar(&addonOptions.SyncInterval, "sync-interval", addonOptions.SyncInterval, "The interval of housekeeping in seconds (addon mode only)")
	flag.DurationVar(&addonOptions.LeaderElectionLeaseDuration, "leader-election-lease-duration", addonOptions.LeaderElectionLeaseDuration, "Leader election lease duration (addon mode only)")
	flag.DurationVar(&addonOptions.LeaderElectionRenewDeadline, "leader-election-renew-deadline", addonOptions.LeaderElectionRenewDeadline, "Leader election renew deadline (addon mode only)")
	flag.DurationVar(&addonOptions.LeaderElectionRetryPeriod, "leader-election-retry-period", addonOptions.LeaderElectionRetryPeriod, "Leader election retry period (addon mode only)")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Check if running in addon mode
	if mode == "addon" {
		runAddonMode()
		return
	}

	// Check if running in cleanup mode
	if mode == "cleanup" {
		runCleanupMode()
		return
	}

	// Continue with normal controller mode

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "7310a56a.open-cluster-management.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controller.GitOpsClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: mgr.GetConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GitOpsCluster")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// runAddonMode runs the application in addon mode
func runAddonMode() {
	setupLog.Info("Starting in addon mode")

	enableLeaderElection := false
	if _, err := rest.InClusterConfig(); err == nil {
		klog.Info("LeaderElection enabled as running in a cluster")
		enableLeaderElection = true
	} else {
		klog.Info("LeaderElection disabled as not running in a cluster")
	}

	// Read environment variables for addon configuration
	if newArgoCDOperatorImage, found := os.LookupEnv("ARGOCD_OPERATOR_IMAGE"); found && newArgoCDOperatorImage != "" {
		ArgoCDOperatorImage = newArgoCDOperatorImage
	}

	if newArgoCDAgentImage, found := os.LookupEnv("ARGOCD_AGENT_IMAGE"); found && newArgoCDAgentImage != "" {
		ArgoCDAgentImage = newArgoCDAgentImage
	}

	if newArgoCDAgentServerAddress, found := os.LookupEnv("ARGOCD_AGENT_SERVER_ADDRESS"); found && newArgoCDAgentServerAddress != "" {
		ArgoCDAgentServerAddress = newArgoCDAgentServerAddress
	}

	if newArgoCDAgentServerPort, found := os.LookupEnv("ARGOCD_AGENT_SERVER_PORT"); found && newArgoCDAgentServerPort != "" {
		ArgoCDAgentServerPort = newArgoCDAgentServerPort
	}

	if newArgoCDAgentMode, found := os.LookupEnv("ARGOCD_AGENT_MODE"); found && newArgoCDAgentMode != "" {
		ArgoCDAgentMode = newArgoCDAgentMode
	}

	setupLog.Info("Addon mode settings",
		"syncInterval", addonOptions.SyncInterval,
		"leaseDuration", addonOptions.LeaderElectionLeaseDuration,
		"renewDeadline", addonOptions.LeaderElectionRenewDeadline,
		"retryPeriod", addonOptions.LeaderElectionRetryPeriod,
		"ArgoCDOperatorImage", ArgoCDOperatorImage,
		"ArgoCDAgentImage", ArgoCDAgentImage,
		"ArgoCDAgentServerAddress", ArgoCDAgentServerAddress,
		"ArgoCDAgentServerPort", ArgoCDAgentServerPort,
		"ArgoCDAgentMode", ArgoCDAgentMode,
	)

	// Create a new manager for addon mode
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics in addon mode
		},
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "argocd-agent-addon-leader.open-cluster-management.io",
		LeaseDuration:    &addonOptions.LeaderElectionLeaseDuration,
		RenewDeadline:    &addonOptions.LeaderElectionRenewDeadline,
		RetryPeriod:      &addonOptions.LeaderElectionRetryPeriod,
	})
	if err != nil {
		setupLog.Error(err, "unable to start addon manager")
		os.Exit(1)
	}

	// Setup the addon with the manager
	if err = addon.SetupWithManager(mgr, addonOptions.SyncInterval,
		ArgoCDOperatorImage, ArgoCDAgentImage, ArgoCDAgentServerAddress, ArgoCDAgentServerPort, ArgoCDAgentMode); err != nil {
		setupLog.Error(err, "unable to create addon controller")
		os.Exit(1)
	}

	setupLog.Info("starting addon manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running addon manager")
		os.Exit(1)
	}
}

// runCleanupMode runs the application in cleanup mode for pre-delete hook
func runCleanupMode() {
	setupLog.Info("Starting in cleanup mode")

	// Read environment variables for cleanup configuration
	if newArgoCDOperatorImage, found := os.LookupEnv("ARGOCD_OPERATOR_IMAGE"); found && newArgoCDOperatorImage != "" {
		ArgoCDOperatorImage = newArgoCDOperatorImage
	}

	if newArgoCDAgentImage, found := os.LookupEnv("ARGOCD_AGENT_IMAGE"); found && newArgoCDAgentImage != "" {
		ArgoCDAgentImage = newArgoCDAgentImage
	}

	setupLog.Info("Cleanup mode settings",
		"ArgoCDOperatorImage", ArgoCDOperatorImage,
		"ArgoCDAgentImage", ArgoCDAgentImage,
	)

	// Create a manager for cleanup mode (no leader election for cleanup job)
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics in cleanup mode
		},
		LeaderElection: false, // Cleanup job doesn't need leader election
	})
	if err != nil {
		setupLog.Error(err, "unable to start cleanup manager")
		os.Exit(1)
	}

	// Setup the cleanup with the manager
	if err = addon.SetupCleanupWithManager(mgr, ArgoCDOperatorImage, ArgoCDAgentImage); err != nil {
		setupLog.Error(err, "unable to create cleanup controller")
		os.Exit(1)
	}

	setupLog.Info("starting cleanup manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running cleanup manager")
		os.Exit(1)
	}
}
