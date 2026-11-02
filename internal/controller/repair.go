package controller

import (
	"context"
	"fmt"
	api "github.com/hrlee-fai/node-inventory-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"
	"time"
)

const maxPreviewPods = 200

// RepairReconciler provides a read-only, periodically refreshed preview.
type RepairReconciler struct {
	client.Client
	Reader   client.Reader
	Interval time.Duration
	Now      func() time.Time
}

func (r *RepairReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var repair api.NodeRepair
	if err := r.Get(ctx, req.NamespacedName, &repair); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	old := repair.DeepCopy()
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	now = now.UTC().Truncate(time.Second)
	var preview *api.RepairPreview
	reason := "PreviewAvailable"
	message := "Read-only preview is available; eviction is not guaranteed"
	condition := metav1.ConditionTrue
	var readErr error
	if !repair.Spec.DryRun || repair.Spec.Operation != "CordonDrain" || repair.Spec.NodeRef.Name == "" || repair.Spec.NodeRef.UID == "" {
		condition = metav1.ConditionUnknown
		reason = "InvalidRequest"
		message = "Only CordonDrain dry-run with a node name and UID is supported"
	} else {
		preview, readErr = r.preview(ctx, &repair, now)
		if readErr != nil {
			condition = metav1.ConditionUnknown
			reason = "PreviewUnavailable"
			message = "Cannot safely produce preview: " + readErr.Error()
		}
	}
	repair.Status.Phase = "PreviewReady"
	if condition != metav1.ConditionTrue {
		repair.Status.Phase = "Blocked"
	}
	repair.Status.Preview = preview
	meta.SetStatusCondition(&repair.Status.Conditions, metav1.Condition{Type: "PreviewAvailable", Status: condition, Reason: reason, Message: message, ObservedGeneration: repair.Generation, LastTransitionTime: metav1.NewTime(now)})
	if !apiequality.Semantic.DeepEqual(old.Status, repair.Status) {
		if err := r.Status().Update(ctx, &repair); err != nil {
			return ctrl.Result{}, err
		}
	}
	if readErr != nil && !apierrors.IsNotFound(readErr) {
		return ctrl.Result{}, readErr
	}
	return ctrl.Result{RequeueAfter: r.Interval}, nil
}
func (r *RepairReconciler) preview(ctx context.Context, repair *api.NodeRepair, now time.Time) (*api.RepairPreview, error) {
	var node corev1.Node
	if err := r.Reader.Get(ctx, types.NamespacedName{Name: repair.Spec.NodeRef.Name}, &node); err != nil {
		return nil, fmt.Errorf("node read: %w", err)
	}
	if string(node.UID) != repair.Spec.NodeRef.UID {
		return nil, fmt.Errorf("node UID mismatch")
	}
	var pods corev1.PodList
	if err := r.Reader.List(ctx, &pods, client.MatchingFields{"spec.nodeName": node.Name}); err != nil {
		return nil, fmt.Errorf("pod list: %w", err)
	}
	if len(pods.Items) > maxPreviewPods {
		return nil, fmt.Errorf("node has more than %d pods; preview omitted", maxPreviewPods)
	}
	namespaces := map[string][]policyv1.PodDisruptionBudget{}
	for _, p := range pods.Items {
		if _, ok := namespaces[p.Namespace]; ok {
			continue
		}
		var list policyv1.PodDisruptionBudgetList
		if err := r.Reader.List(ctx, &list, client.InNamespace(p.Namespace)); err != nil {
			return nil, fmt.Errorf("PDB list in %s: %w", p.Namespace, err)
		}
		namespaces[p.Namespace] = list.Items
	}
	out := &api.RepairPreview{ObservedAt: metav1.NewTime(now), NodeUID: string(node.UID), NodeResourceVersion: node.ResourceVersion, Unschedulable: node.Spec.Unschedulable}
	for _, p := range pods.Items {
		x := api.RepairPod{Namespace: p.Namespace, Name: p.Name, UID: string(p.UID), Classification: "EvictionCandidate"}
		if _, ok := p.Annotations[corev1.MirrorPodAnnotationKey]; ok {
			x.Classification = "Excluded"
			x.Reason = "MirrorPod"
		} else if managedByDaemonSet(p.OwnerReferences) {
			x.Classification = "Excluded"
			x.Reason = "DaemonSet"
		} else if p.DeletionTimestamp != nil {
			x.Classification = "Terminating"
			x.Reason = "AwaitTermination"
		}
		for _, budget := range namespaces[p.Namespace] {
			if budget.Spec.Selector == nil {
				continue
			}
			sel, err := metav1.LabelSelectorAsSelector(budget.Spec.Selector)
			if err != nil {
				return nil, fmt.Errorf("invalid PDB selector %s/%s: %w", p.Namespace, budget.Name, err)
			}
			if sel.Matches(labels.Set(p.Labels)) {
				x.PDBs = append(x.PDBs, api.RepairPDB{Name: budget.Name, DisruptionsAllowed: budget.Status.DisruptionsAllowed, ObservedGeneration: budget.Status.ObservedGeneration, Generation: budget.Generation})
			}
		}
		if x.Classification == "EvictionCandidate" {
			if metav1.GetControllerOf(&p) == nil {
				x.Warnings = append(x.Warnings, "UnmanagedPod")
			}
			for _, v := range p.Spec.Volumes {
				if v.EmptyDir != nil {
					x.Warnings = append(x.Warnings, "EmptyDirData")
					break
				}
			}
			if len(x.PDBs) > 1 {
				x.Warnings = append(x.Warnings, "MultipleMatchingPDBs")
			}
			for _, b := range x.PDBs {
				if b.ObservedGeneration < b.Generation {
					x.Warnings = append(x.Warnings, "PDBStatusOutdated")
					break
				}
			}
		}
		sort.Slice(x.PDBs, func(i, j int) bool { return x.PDBs[i].Name < x.PDBs[j].Name })
		out.Pods = append(out.Pods, x)
	}
	sort.Slice(out.Pods, func(i, j int) bool {
		a, b := out.Pods[i], out.Pods[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return out, nil
}
func managedByDaemonSet(refs []metav1.OwnerReference) bool {
	for _, r := range refs {
		if r.Kind == "DaemonSet" && r.Controller != nil && *r.Controller {
			return true
		}
	}
	return false
}
func (r *RepairReconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).For(&api.NodeRepair{}, builder.WithPredicates(builderPredicate())).Complete(r)
}
