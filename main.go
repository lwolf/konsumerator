/*

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
	"flag"
	"fmt"
	"os"
	"runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/lwolf/konsumerator/controllers"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	konsumeratorv1 "github.com/lwolf/konsumerator/api/v1"
	// +kubebuilder:scaffold:imports
)

var (
	Version  string
	scheme   = apiruntime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	metaInfoGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "konsumerator",
		Name:      "meta_info",
	}, []string{"go_version", "binary_version"})
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(konsumeratorv1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var isDebug bool
	var namespace string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&isDebug, "verbose", false, "Set log level to debug mode.")
	flag.StringVar(&namespace, "namespace", "", "Run operator in guest mode, limit scope to only a single namespace. No CRD will be created")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	metrics.Registry.MustRegister(metaInfoGauge)
	metaInfoGauge.WithLabelValues(runtime.Version(), Version).Set(1)

	setupLog.Info(
		"Initializing konsumerator controller",
		"version", Version,
		"isDebug", isDebug,
		"namespace", namespace,
		"metricsAddr", metricsAddr,
		"leaderElection", enableLeaderElection,
	)
	guestMode := namespace != ""

	options := ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       fmt.Sprintf("konsumerator-global"),
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
	}
	if guestMode {
		options.Namespace = namespace
		options.LeaderElectionID = fmt.Sprintf("konsumerator-%s", namespace)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	var c controllers.Controller
	if guestMode {
		c = &controllers.ConfigMapReconciler{
			Client:   mgr.GetClient(),
			Scheme:   mgr.GetScheme(),
			Log:      ctrl.Log.WithName("controllers").WithName("ConsumerCM"),
			Recorder: mgr.GetEventRecorderFor("konsumerator"),
		}
	} else {
		c = &controllers.ConsumerReconciler{
			Client:   mgr.GetClient(),
			Log:      ctrl.Log.WithName("controllers").WithName("Consumer"),
			Scheme:   mgr.GetScheme(),
			Recorder: mgr.GetEventRecorderFor("konsumerator"),
		}
	}

	if err := c.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller")
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
