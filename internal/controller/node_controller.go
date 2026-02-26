package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kubesolv/internal/alert"
)

type NodeReconciler struct {
	client.Client
	Telegram   *alert.TelegramBot
	Slack      *alert.SlackBot
	AlertCache sync.Map
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if node.Labels["kubesolv.io/test-cordon"] == "true" {
		r.alertNode(node.Name, "DiskPressure", "SimulatedPressure", "Testing KubeSolv infrastructure alerts.")
		patch := []byte(`{"metadata":{"labels":{"kubesolv.io/test-cordon":null}}}`)
		_ = r.Client.Patch(ctx, &node, client.RawPatch(types.MergePatchType, patch))
		return ctrl.Result{}, nil
	}

	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			r.alertNode(node.Name, "Node Not Ready", condition.Reason, condition.Message)
		}
		if (condition.Type == corev1.NodeMemoryPressure || condition.Type == corev1.NodeDiskPressure || condition.Type == corev1.NodePIDPressure) && condition.Status == corev1.ConditionTrue {
			r.alertNode(node.Name, string(condition.Type), condition.Reason, condition.Message)
		}
	}

	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

func (r *NodeReconciler) alertNode(nodeName, issueType, reason, message string) {
	cacheKey := fmt.Sprintf("node/%s/%s", nodeName, issueType)
	if lastTime, ok := r.AlertCache.Load(cacheKey); ok {
		if time.Since(lastTime.(time.Time)) < 30*time.Minute {
			return
		}
	}
	r.AlertCache.Store(cacheKey, time.Now())

	title := fmt.Sprintf("Infrastructure Alert: %s", issueType)
	body := fmt.Sprintf("Node: %s\nReason: %s\nDetails: %s\n\nAction Required: Prevent new pods from scheduling here.", nodeName, reason, message)
	actionID := fmt.Sprintf("cordon_node|%s", nodeName)

	if r.Slack != nil {
		_ = r.Slack.BroadcastWithAction(title, body, actionID, "Approve Cordon")
	}
	if r.Telegram != nil {
		r.Telegram.BroadcastWithAction("cluster", title, body, actionID, "Approve Cordon")
	}
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}
