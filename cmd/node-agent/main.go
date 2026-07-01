package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	praestov1alpha1 "github.com/federicolepera/praesto/api/v1alpha1"
	"github.com/federicolepera/praesto/internal/downloader"
	"github.com/federicolepera/praesto/internal/modeldownload"
	"github.com/federicolepera/praesto/internal/nodeagent"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(praestov1alpha1.AddToScheme(scheme))
}

func main() {
	var nodeName string
	var cacheRoot string
	var dirMode string
	var metricsBindAddress string
	var healthProbeBindAddress string

	flag.StringVar(&nodeName, "node-name", "", "Name of the Kubernetes node this agent is running on.")
	flag.StringVar(&cacheRoot, "cache-root", downloader.DefaultLocalCacheBasePath,
		"Host-local Praesto cache root prepared by the cluster administrator.")
	flag.StringVar(&dirMode, "dir-mode", "0775", "Octal permissions for prepared model cache directories.")
	flag.StringVar(&metricsBindAddress, "metrics-bind-address", "0",
		"The address the metric endpoint binds to. Use 0 to disable metrics.")
	flag.StringVar(&healthProbeBindAddress, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if strings.TrimSpace(nodeName) == "" {
		fmt.Fprintln(os.Stderr, "--node-name is required")
		os.Exit(1)
	}
	parsedMode, err := parseDirMode(dirMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --dir-mode: %v\n", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsBindAddress},
		HealthProbeBindAddress: healthProbeBindAddress,
		LeaderElection:         false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to start manager: %v\n", err)
		os.Exit(1)
	}
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&corev1.Pod{},
		"spec.nodeName",
		func(obj client.Object) []string {
			pod := obj.(*corev1.Pod)
			if pod.Spec.NodeName == "" {
				return nil
			}
			return []string{pod.Spec.NodeName}
		},
	); err != nil {
		fmt.Fprintf(os.Stderr, "unable to index pods by node name: %v\n", err)
		os.Exit(1)
	}

	if err := (&nodeagent.Reconciler{
		Client:    mgr.GetClient(),
		NodeName:  nodeName,
		CacheRoot: cacheRoot,
		DirMode:   parsedMode,
		Downloader: &modeldownload.Router{
			HuggingFace: &modeldownload.HuggingFaceDownloader{},
		},
	}).SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "unable to create node-agent controller: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		fmt.Fprintf(os.Stderr, "unable to set up health check: %v\n", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		fmt.Fprintf(os.Stderr, "unable to set up ready check: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		fmt.Fprintf(os.Stderr, "problem running manager: %v\n", err)
		os.Exit(1)
	}
}

func parseDirMode(value string) (fs.FileMode, error) {
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, "0o"), 8, 32)
	if err != nil {
		return 0, err
	}
	return fs.FileMode(parsed), nil
}
