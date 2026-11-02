package controller

import (
	"context"
	"fmt"
	api "github.com/hrlee-fai/node-inventory-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

type Reconciler struct {
	client.Client
	Reader   client.Reader
	Interval time.Duration
	Now      func() time.Time
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var inv api.NodeInventory
	if err := r.Get(ctx, req.NamespacedName, &inv); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	old := inv.DeepCopy()
	var node corev1.Node
	err := r.Reader.Get(ctx, types.NamespacedName{Name: inv.Name}, &node)
	reason := "Observed"
	msg := "Node observed"
	available := metav1.ConditionTrue
	if err != nil {
		available = metav1.ConditionUnknown
		reason = "NodeReadFailed"
		msg = "Node could not be read"
		if apierrors.IsNotFound(err) {
			reason = "NodeNotFound"
			msg = "Node does not exist"
		}
	} else if string(node.UID) != inv.Spec.NodeRef.UID {
		available = metav1.ConditionUnknown
		reason = "UIDMismatch"
		msg = "Node UID differs from approved UID"
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	now = now.UTC().Truncate(time.Second)
	set := func(t string, s metav1.ConditionStatus, reason, msg string) {
		meta.SetStatusCondition(&inv.Status.Conditions, metav1.Condition{Type: t, Status: s, Reason: reason, Message: msg, ObservedGeneration: inv.Generation, LastTransitionTime: metav1.NewTime(now)})
	}
	set("ObservationAvailable", available, reason, msg)
	if available != metav1.ConditionTrue {
		inv.Status.Observed = nil
		set("BaselineMatched", metav1.ConditionUnknown, reason, msg)
	} else {
		cap := corev1.ResourceList{}
		for name := range inv.Spec.Baseline.Capacity {
			if v, ok := node.Status.Capacity[name]; ok {
				cap[name] = v.DeepCopy()
			}
		}
		snap := api.Snapshot{NodeInfo: api.NodeInfo{OperatingSystem: node.Status.NodeInfo.OperatingSystem, Architecture: node.Status.NodeInfo.Architecture, KernelVersion: node.Status.NodeInfo.KernelVersion}, Capacity: cap}
		inv.Status.Observed = &api.Observation{NodeUID: string(node.UID), ResourceVersion: node.ResourceVersion, ObservedAt: metav1.NewTime(now), Snapshot: snap}
		matched := equalSnapshot(inv.Spec.Baseline.Snapshot, snap)
		state := metav1.ConditionFalse
		why := "BaselineDiffers"
		message := "Observed values differ from approved baseline"
		if matched {
			state = metav1.ConditionTrue
			why = "BaselineMatches"
			message = "Observed values match approved baseline"
		}
		set("BaselineMatched", state, why, message)
	}
	if !apiequality.Semantic.DeepEqual(old.Status, inv.Status) {
		if updateErr := r.Status().Update(ctx, &inv); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("read Node: %w", err)
	}
	return ctrl.Result{RequeueAfter: r.Interval}, nil
}
func equalSnapshot(a, b api.Snapshot) bool {
	if a.NodeInfo != b.NodeInfo || len(a.Capacity) != len(b.Capacity) {
		return false
	}
	for k, v := range a.Capacity {
		w, ok := b.Capacity[k]
		if !ok || v.Cmp(w) != 0 {
			return false
		}
	}
	return true
}
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).For(&api.NodeInventory{}, builder.WithPredicates(builderPredicate())).Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: o.GetName()}}}
	})).Complete(r)
}
func builderPredicate() predicate.Predicate {
	return predicate.Funcs{CreateFunc: func(event.CreateEvent) bool { return true }, DeleteFunc: func(event.DeleteEvent) bool { return true }, GenericFunc: func(event.GenericEvent) bool { return true }, UpdateFunc: func(e event.UpdateEvent) bool { return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() }}
}
