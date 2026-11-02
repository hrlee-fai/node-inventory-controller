package main

import (
	"flag"
	api "github.com/hrlee-fai/node-inventory-controller/api/v1alpha1"
	"github.com/hrlee-fai/node-inventory-controller/internal/controller"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"time"
)

func main() {
	var leader bool
	var interval time.Duration
	flag.BoolVar(&leader, "leader-elect", true, "enable leader election")
	flag.DurationVar(&interval, "interval", time.Minute, "observation refresh interval")
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))
	if interval <= 0 {
		os.Exit(1)
	}
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = api.AddToScheme(s)
	m, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: s, LeaderElection: leader, LeaderElectionID: "node-inventory.infra.fai.devops", Metrics: server.Options{BindAddress: "0"}})
	if err != nil {
		ctrl.Log.Error(err, "manager")
		os.Exit(1)
	}
	if err = (&controller.Reconciler{Client: m.GetClient(), Reader: m.GetAPIReader(), Interval: interval}).SetupWithManager(m); err != nil {
		ctrl.Log.Error(err, "setup")
		os.Exit(1)
	}
	if err = m.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "manager stopped")
		os.Exit(1)
	}
}
