// Package gate provides an opt-in, fail-closed check before creating a sentinel.
package gate

import (
	"context"
	api "github.com/hrlee-fai/node-inventory-controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type Gate struct {
	Enabled bool
	Reader  client.Reader
	MaxAge  time.Duration
	Now     func() time.Time
}
type Decision struct {
	Allowed         bool
	Reason          string
	BaselineMatched metav1.ConditionStatus
}

func (g Gate) Check(ctx context.Context, nodeName, nodeUID string) Decision {
	if !g.Enabled {
		return Decision{Allowed: true, Reason: "disabled", BaselineMatched: metav1.ConditionUnknown}
	}
	deny := func(reason string) Decision {
		return Decision{Reason: reason, BaselineMatched: metav1.ConditionUnknown}
	}
	if g.Reader == nil || g.MaxAge <= 0 || nodeName == "" || nodeUID == "" {
		return deny("invalid gate configuration or node identity")
	}
	var inv api.NodeInventory
	if err := g.Reader.Get(ctx, types.NamespacedName{Name: nodeName}, &inv); err != nil {
		return deny("inventory unavailable: " + err.Error())
	}
	if inv.Spec.NodeRef.UID != nodeUID || inv.Status.Observed == nil || inv.Status.Observed.NodeUID != nodeUID {
		return deny("inventory UID mismatch or observation missing")
	}
	available := meta.FindStatusCondition(inv.Status.Conditions, "ObservationAvailable")
	if available == nil || available.Status != metav1.ConditionTrue || available.ObservedGeneration != inv.Generation {
		return deny("observation unavailable or outdated generation")
	}
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	t := inv.Status.Observed.ObservedAt.Time
	if t.IsZero() || t.After(now) || now.Sub(t) > g.MaxAge {
		return deny("observation missing, in future, or stale")
	}
	result := Decision{Allowed: true, Reason: "fresh inventory", BaselineMatched: metav1.ConditionUnknown}
	if c := meta.FindStatusCondition(inv.Status.Conditions, "BaselineMatched"); c != nil && c.ObservedGeneration == inv.Generation {
		result.BaselineMatched = c.Status
	}
	return result
}
