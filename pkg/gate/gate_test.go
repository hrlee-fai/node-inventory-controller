package gate

import (
	"context"
	api "github.com/hrlee-fai/node-inventory-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
	"time"
)

func TestFailClosed(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	inv := &api.NodeInventory{ObjectMeta: metav1.ObjectMeta{Name: "n", Generation: 1}, Spec: api.NodeInventorySpec{NodeRef: api.NodeRef{UID: "u"}}, Status: api.NodeInventoryStatus{Observed: &api.Observation{NodeUID: "u", ObservedAt: metav1.NewTime(now)}, Conditions: []metav1.Condition{{Type: "ObservationAvailable", Status: metav1.ConditionTrue, ObservedGeneration: 1}, {Type: "BaselineMatched", Status: metav1.ConditionFalse, ObservedGeneration: 1}}}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(inv).Build()
	g := Gate{Enabled: true, Reader: c, MaxAge: time.Minute, Now: func() time.Time { return now }}
	if d := g.Check(context.Background(), "n", "u"); !d.Allowed || d.BaselineMatched != metav1.ConditionFalse {
		t.Fatal(d)
	}
	for _, x := range []struct {
		name, uid string
		delta     time.Duration
	}{{"missing", "u", 0}, {"n", "other", 0}, {"n", "u", time.Minute + time.Second}, {"n", "u", -time.Second}} {
		g.Now = func() time.Time { return now.Add(x.delta) }
		if d := g.Check(context.Background(), x.name, x.uid); d.Allowed {
			t.Fatal(x, d)
		}
	}
	g.Reader = nil
	if g.Check(context.Background(), "n", "u").Allowed {
		t.Fatal("nil reader")
	}
	g.Enabled = false
	if !g.Check(context.Background(), "n", "u").Allowed {
		t.Fatal("disabled")
	}
}
