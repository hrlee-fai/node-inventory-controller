package controller

import (
	"context"
	"errors"
	api "github.com/hrlee-fai/node-inventory-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
	"time"
)

type badReader struct{ client.Reader }

func (b badReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("unavailable")
}
func TestReconcile(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	baseline := api.Snapshot{NodeInfo: api.NodeInfo{OperatingSystem: "linux", Architecture: "amd64", KernelVersion: "k"}, Capacity: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1000m"), corev1.ResourceMemory: resource.MustParse("1Gi")}}
	inv := &api.NodeInventory{ObjectMeta: metav1.ObjectMeta{Name: "n", Generation: 1}, Spec: api.NodeInventorySpec{NodeRef: api.NodeRef{UID: "u"}, Baseline: api.Baseline{Snapshot: baseline}}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n", UID: "u"}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{OperatingSystem: "linux", Architecture: "amd64", KernelVersion: "k"}, Capacity: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1024Mi")}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(inv).WithObjects(inv, node).Build()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := Reconciler{Client: c, Reader: c, Interval: time.Minute, Now: func() time.Time { return now }}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "n"}}
	run := func() api.NodeInventory {
		t.Helper()
		if _, err := r.Reconcile(ctx, req); err != nil {
			t.Fatal(err)
		}
		var got api.NodeInventory
		if err := c.Get(ctx, req.NamespacedName, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	a := run()
	if meta.FindStatusCondition(a.Status.Conditions, "BaselineMatched").Status != metav1.ConditionTrue {
		t.Fatal(a.Status)
	}
	b := run()
	if a.ResourceVersion != b.ResourceVersion {
		t.Fatalf("identical observation wrote status: %#v vs %#v", a.Status, b.Status)
	}
	node.Status.NodeInfo.KernelVersion = "new"
	if err := c.Status().Update(ctx, node); err != nil {
		t.Fatal(err)
	}
	b = run()
	if meta.FindStatusCondition(b.Status.Conditions, "BaselineMatched").Status != metav1.ConditionFalse {
		t.Fatal(b.Status)
	}
	transition := meta.FindStatusCondition(b.Status.Conditions, "BaselineMatched").LastTransitionTime
	now = now.Add(time.Minute)
	b = run()
	if !meta.FindStatusCondition(b.Status.Conditions, "BaselineMatched").LastTransitionTime.Equal(&transition) {
		t.Fatal("transition time changed")
	}
	node.UID = "replacement"
	if err := c.Update(ctx, node); err != nil {
		t.Fatal(err)
	}
	b = run()
	if b.Status.Observed != nil || meta.FindStatusCondition(b.Status.Conditions, "BaselineMatched").Status != metav1.ConditionUnknown {
		t.Fatal(b.Status)
	}
	if err := c.Delete(ctx, node); err != nil {
		t.Fatal(err)
	}
	b = run()
	if meta.FindStatusCondition(b.Status.Conditions, "ObservationAvailable").Reason != "NodeNotFound" {
		t.Fatal(b.Status)
	}
	r.Reader = badReader{c}
	if _, err := r.Reconcile(ctx, req); err == nil {
		t.Fatal("expected read error")
	}
	if err := c.Get(ctx, req.NamespacedName, &b); err != nil {
		t.Fatal(err)
	}
	if b.Status.Observed != nil {
		t.Fatal(b.Status)
	}
}

type conflictWriter struct {
	client.SubResourceWriter
	fail bool
}

func (w *conflictWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if w.fail {
		w.fail = false
		return apierrors.NewConflict(schema.GroupResource{Group: "infra.fai.devops", Resource: "nodeinventories"}, obj.GetName(), errors.New("conflict"))
	}
	return w.SubResourceWriter.Update(ctx, obj, opts...)
}

type conflictClient struct {
	client.Client
	writer *conflictWriter
}

func (c conflictClient) Status() client.SubResourceWriter { return c.writer }
func TestConflictRetry(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = api.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	inv := &api.NodeInventory{ObjectMeta: metav1.ObjectMeta{Name: "n"}, Spec: api.NodeInventorySpec{NodeRef: api.NodeRef{UID: "u"}}}
	base := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(inv).WithObjects(inv).Build()
	writer := &conflictWriter{SubResourceWriter: base.Status(), fail: true}
	r := Reconciler{Client: conflictClient{Client: base, writer: writer}, Reader: base, Interval: time.Minute}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "n"}}
	if _, err := r.Reconcile(ctx, req); !apierrors.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	var got api.NodeInventory
	if err := base.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if meta.FindStatusCondition(got.Status.Conditions, "ObservationAvailable") == nil {
		t.Fatal("retry did not update status")
	}
}
