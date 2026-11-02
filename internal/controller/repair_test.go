package controller

import (
	"context"
	api "github.com/hrlee-fai/node-inventory-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
	"time"
)

func TestRepairPreview(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = policyv1.AddToScheme(s)
	_ = api.AddToScheme(s)
	repair := &api.NodeRepair{ObjectMeta: metav1.ObjectMeta{Name: "request", Generation: 1}, Spec: api.NodeRepairSpec{NodeRef: api.RepairNodeRef{Name: "n", UID: "u"}, Operation: "CordonDrain", DryRun: true, Reason: "test"}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n", UID: "u"}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns", UID: "puid", Labels: map[string]string{"app": "a"}}, Spec: corev1.PodSpec{NodeName: "n"}}
	pdb := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "budget", Namespace: "ns", Generation: 1}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}}, Status: policyv1.PodDisruptionBudgetStatus{ObservedGeneration: 1}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(repair).WithObjects(repair, node, pod, pdb).WithIndex(&corev1.Pod{}, "spec.nodeName", func(o client.Object) []string { return []string{o.(*corev1.Pod).Spec.NodeName} }).Build()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := RepairReconciler{Client: c, Reader: c, Interval: time.Minute, Now: func() time.Time { return now }}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "request"}}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	var got api.NodeRepair
	if err := c.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != "PreviewReady" || len(got.Status.Preview.Pods) != 1 || len(got.Status.Preview.Pods[0].PDBs) != 1 || got.Status.Preview.Pods[0].PDBs[0].DisruptionsAllowed != 0 {
		t.Fatalf("unexpected preview: %+v", got.Status)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: "n"}, node); err != nil {
		t.Fatal(err)
	}
	if node.Spec.Unschedulable {
		t.Fatal("dry-run changed node")
	}
	firstVersion := got.ResourceVersion
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if firstVersion != got.ResourceVersion {
		t.Fatal("identical preview updated status")
	}
	got.Spec.NodeRef.UID = "different"
	if err := c.Update(ctx, &got); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err == nil {
		t.Fatal("expected UID mismatch")
	}
	if err := c.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != "Blocked" || got.Status.Preview != nil {
		t.Fatalf("stale preview retained: %+v", got.Status)
	}
}

func TestRepairPreviewRejectsReadFailureAndUnsafeRequests(t *testing.T) {
	ctx := context.Background()
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = policyv1.AddToScheme(s)
	_ = api.AddToScheme(s)
	inv := &api.NodeRepair{ObjectMeta: metav1.ObjectMeta{Name: "r"}, Spec: api.NodeRepairSpec{NodeRef: api.RepairNodeRef{Name: "n", UID: "u"}, Operation: "CordonDrain", DryRun: true, Reason: "test"}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(inv).WithObjects(inv).Build()
	r := RepairReconciler{Client: c, Reader: badReader{c}, Interval: time.Minute}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "r"}}
	if _, err := r.Reconcile(ctx, req); err == nil {
		t.Fatal("expected API failure")
	}
	var got api.NodeRepair
	if err := c.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != "Blocked" || got.Status.Preview != nil {
		t.Fatal(got.Status)
	}
	got.Spec.DryRun = false
	if err := c.Update(ctx, &got); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Conditions[0].Reason != "InvalidRequest" {
		t.Fatal(got.Status)
	}
}
