package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	opsv1 "kubesolv/api/v1"
	"kubesolv/internal/ai"
	"kubesolv/internal/alert"
	"kubesolv/internal/metrics"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type KubeSolvConfigReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	AI         *ai.Client
	ClientSet  *kubernetes.Clientset
	Telegram   *alert.TelegramBot
	Slack      *alert.SlackBot
	Prometheus *metrics.PrometheusClient
	AlertCache sync.Map
	StateCache sync.Map
}

func (r *KubeSolvConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if req.Namespace == "kube-system" || req.Namespace == "local-path-storage" || req.Namespace == "ingress-nginx" || req.Namespace == "monitoring" {
		return ctrl.Result{}, nil
	}

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err == nil {
		r.trackLifecycle(ctx, &pod)
		r.analyzePod(ctx, &pod)
		r.checkSecurity(ctx, &pod)
		if pod.Status.Phase == corev1.PodRunning {
			r.checkActivity(ctx, &pod)
			if r.Prometheus != nil {
				r.checkPrometheusMetrics(ctx, &pod)
				r.checkCostOptimization(ctx, &pod) // Trigger Cost Guardian
			}
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}
	return ctrl.Result{}, nil
}

func (r *KubeSolvConfigReconciler) notifyUser(title, message string) {
	hash := fmt.Sprintf("%s-%d", title, len(message))
	if lastTime, ok := r.AlertCache.Load(hash); ok {
		if time.Since(lastTime.(time.Time)) < 5*time.Minute {
			if !strings.Contains(title, "Recovered") && !strings.Contains(title, "New Pod") {
				return
			}
		}
	}
	r.AlertCache.Store(hash, time.Now())

	if r.Telegram != nil {
		r.Telegram.Broadcast("cluster", title, message)
	}
	if r.Slack != nil {
		_ = r.Slack.Broadcast(title, message)
	}
}

//nolint:unparam
func (r *KubeSolvConfigReconciler) trackLifecycle(ctx context.Context, pod *corev1.Pod) {
	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	currentStatus := string(pod.Status.Phase)
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			currentStatus = cs.State.Waiting.Reason
		} else if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			currentStatus = "Error"
		}
	}

	lastStatusInterface, known := r.StateCache.Load(podKey)
	lastStatus := ""
	if known {
		lastStatus = lastStatusInterface.(string)
	}

	if !known {
		msg := fmt.Sprintf("📦 *New Pod Detected*\nName: `%s`\nNamespace: `%s`\nStatus: `%s`", pod.Name, pod.Namespace, currentStatus)
		r.notifyUser("🆕 New Activity", msg)
		r.StateCache.Store(podKey, currentStatus)
		return
	}

	isBad := func(s string) bool {
		//nolint:goconst
		return s == "CrashLoopBackOff" || s == "ImagePullBackOff" || s == "ErrImagePull" || s == "Error" || s == "OOMKilled" || s == "CreateContainerConfigError"
	}

	if isBad(lastStatus) && currentStatus == "Running" {
		msg := fmt.Sprintf("✅ *Pod Recovered!*\nPod `%s` is now healthy and Running.", pod.Name)
		r.notifyUser("✅ Issue Resolved", msg)
		r.AlertCache.Delete(fmt.Sprintf("Issue Detected: %s-%d", lastStatus, len(msg)))
	}

	if lastStatus != currentStatus {
		r.StateCache.Store(podKey, currentStatus)
	}
}

func (r *KubeSolvConfigReconciler) checkSecurity(ctx context.Context, pod *corev1.Pod) {
	cacheKey := fmt.Sprintf("sec/%s", pod.Namespace)
	if lastTime, ok := r.AlertCache.Load(cacheKey); ok {
		if time.Since(lastTime.(time.Time)) < 1*time.Hour {
			return
		}
	}

	policies, err := r.ClientSet.NetworkingV1().NetworkPolicies(pod.Namespace).List(ctx, metav1.ListOptions{})
	if err == nil && len(policies.Items) == 0 {
		r.AlertCache.Store(cacheKey, time.Now())

		policy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubesolv-default-deny",
				Namespace: pod.Namespace,
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{
					networkingv1.PolicyTypeIngress,
					networkingv1.PolicyTypeEgress,
				},
			},
		}

		_, err := r.ClientSet.NetworkingV1().NetworkPolicies(pod.Namespace).Create(ctx, policy, metav1.CreateOptions{})
		if err == nil {
			r.notifyUser("🛡️ Security Auto-Heal", fmt.Sprintf("No NetworkPolicy found in namespace `%s`. Autonomously applied a default-deny policy to secure the perimeter.", pod.Namespace))
		}
	}
}

func (r *KubeSolvConfigReconciler) checkActivity(ctx context.Context, pod *corev1.Pod) {
	cacheKey := fmt.Sprintf("scale/%s/%s", pod.Namespace, pod.Name)
	if lastTime, ok := r.AlertCache.Load(cacheKey); ok {
		if time.Since(lastTime.(time.Time)) < 15*time.Second {
			return
		}
	}
	r.AlertCache.Store(cacheKey, time.Now())

	ownerRef := metav1.GetControllerOf(pod)
	//nolint:goconst
	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
		return
	}
	var rs appsv1.ReplicaSet
	if err := r.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: pod.Namespace}, &rs); err != nil {
		return
	}
	rsOwner := metav1.GetControllerOf(&rs)
	//nolint:goconst
	if rsOwner == nil || rsOwner.Kind != "Deployment" {
		return
	}
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: rsOwner.Name, Namespace: pod.Namespace}, &deploy); err != nil {
		return
	}

	minReplicas := int32(1)
	maxReplicas := int32(10)
	if val, ok := deploy.Annotations["kubesolv.io/min-replicas"]; ok {
		if v, err := strconv.Atoi(val); err == nil {
			minReplicas = int32(v)
		}
	}
	if val, ok := deploy.Annotations["kubesolv.io/max-replicas"]; ok {
		if v, err := strconv.Atoi(val); err == nil {
			maxReplicas = int32(v)
		}
	}

	logs, err := r.getRecentLogs(ctx, pod.Name, pod.Namespace)
	if err != nil {
		return
	}

	lines := len(strings.Split(logs, "\n"))
	logsPerSec := float64(lines) / 5.0
	currentReplicas := *deploy.Spec.Replicas

	var desiredReplicas int32
	reason := ""

	if logsPerSec < 0.5 {
		if currentReplicas > minReplicas {
			desiredReplicas = currentReplicas - 1
			reason = fmt.Sprintf("📉 Traffic Dropped (%.1f logs/s). Scaling Down.", logsPerSec)
		} else {
			return
		}
	} else if logsPerSec > 10.0 {
		steps := min(int32(math.Ceil((logsPerSec-10.0)/10.0)), 2)
		if steps < 1 {
			steps = 1
		}

		if currentReplicas < maxReplicas {
			desiredReplicas = min(currentReplicas+steps, maxReplicas)
			reason = fmt.Sprintf("🔥 High Load (%.1f logs/s). Scaling Up +%d.", logsPerSec, steps)
		} else {
			return
		}
	} else {
		return
	}

	if desiredReplicas != currentReplicas {
		patch := fmt.Appendf(nil, `{"spec": {"replicas": %d}}`, desiredReplicas)
		if err := r.Patch(ctx, &deploy, client.RawPatch(types.MergePatchType, patch)); err == nil {
			msg := fmt.Sprintf("📦 *App:* `%s`\n📊 *Traffic:* %.1f logs/sec\n🔄 *Adjustment:* %d ➡ %d\n📝 *Reason:* %s",
				deploy.Name, logsPerSec, currentReplicas, desiredReplicas, reason)
			r.notifyUser("Smart Scaling Triggered", msg)
		}
	}
}

//nolint:gocyclo
func (r *KubeSolvConfigReconciler) analyzePod(ctx context.Context, pod *corev1.Pod) {
	for _, status := range pod.Status.ContainerStatuses {
		if status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.Reason == "OOMKilled" {
			r.handleOOM(ctx, pod)
			return
		}

		if status.State.Waiting != nil {
			reason := status.State.Waiting.Reason

			if reason == "ErrImagePull" || reason == "ImagePullBackOff" || reason == "CrashLoopBackOff" || reason == "CreateContainerConfigError" {
				cacheKey := fmt.Sprintf("analyze/%s/%s", pod.Namespace, pod.Name)
				if lastTime, ok := r.AlertCache.Load(cacheKey); ok {
					if time.Since(lastTime.(time.Time)) < 2*time.Minute {
						return
					}
				}
				r.AlertCache.Store(cacheKey, time.Now())

				var logs string
				if reason == "CrashLoopBackOff" {
					logs, _ = r.getPodLogs(ctx, pod.Name, pod.Namespace, status.Name)
				}

				if r.AI != nil && reason != "ImagePullBackOff" && reason != "ErrImagePull" {
					decision, err := r.AI.EvaluateIncident(ctx, reason, pod.Name, pod.Namespace, status.State.Waiting.Message, logs)
					if err == nil {

						if decision.ShouldAutoFix {
							actionMsg := ""

							if decision.Action == "rollback" {
								ownerRef := metav1.GetControllerOf(pod)
								if ownerRef != nil && ownerRef.Kind == "ReplicaSet" {
									rs, _ := r.ClientSet.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ownerRef.Name, metav1.GetOptions{})
									rsOwner := metav1.GetControllerOf(rs)
									if rsOwner != nil && rsOwner.Kind == "Deployment" {
										if err := r.rollbackDeployment(ctx, pod.Namespace, rsOwner.Name); err == nil {
											actionMsg = fmt.Sprintf("🤖 *Auto-Healed:* Successfully rolled back `%s` to previous stable version (Confidence: %d%%).", rsOwner.Name, decision.Confidence)
										} else {
											actionMsg = fmt.Sprintf("🤖 *Auto-Heal Failed:* Tried to rollback `%s` but encountered error: %v", rsOwner.Name, err)
										}
									}
								}
							}

							if actionMsg != "" {
								r.notifyUser("✅ Autonomous Action", actionMsg)
								return
							}
						}

						msg := fmt.Sprintf("⚠️ *Issue:* %s\n🤖 *AI Analysis:* %s\nConfidence: %d%%", reason, decision.Reason, decision.Confidence)

						if reason == "CrashLoopBackOff" && status.RestartCount >= 2 {
							ownerRef := metav1.GetControllerOf(pod)
							if ownerRef != nil && ownerRef.Kind == "ReplicaSet" {
								rs, _ := r.ClientSet.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ownerRef.Name, metav1.GetOptions{})
								rsOwner := metav1.GetControllerOf(rs)
								if rsOwner != nil && rsOwner.Kind == "Deployment" {
									actionID := fmt.Sprintf("rollback|%s|%s", pod.Namespace, rsOwner.Name)
									if r.Slack != nil {
										_ = r.Slack.BroadcastWithAction("Action Required", msg, actionID, "Approve Rollback")
									}
									if r.Telegram != nil {
										r.Telegram.BroadcastWithAction("cluster", "Action Required", msg, actionID, "Approve Rollback")
									}
									return
								}
							}
						}

						r.notifyUser(fmt.Sprintf("Issue Detected: %s", reason), msg)
					}
				}
			}
		}
	}
}

func (r *KubeSolvConfigReconciler) handleOOM(ctx context.Context, pod *corev1.Pod) {
	cacheKey := fmt.Sprintf("oom/%s/%s", pod.Namespace, pod.Name)
	if _, ok := r.AlertCache.Load(cacheKey); ok {
		return
	}
	r.AlertCache.Store(cacheKey, time.Now())

	ownerRef := metav1.GetControllerOf(pod)
	//nolint:goconst
	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
		return
	}
	rs, err := r.ClientSet.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ownerRef.Name, metav1.GetOptions{})
	if err != nil {
		return
	}
	rsOwner := metav1.GetControllerOf(rs)
	//nolint:goconst
	if rsOwner == nil || rsOwner.Kind != "Deployment" {
		return
	}
	deploy, err := r.ClientSet.AppsV1().Deployments(pod.Namespace).Get(ctx, rsOwner.Name, metav1.GetOptions{})
	if err != nil {
		return
	}

	newLimit := resource.MustParse("128Mi")
	containerName := deploy.Spec.Template.Spec.Containers[0].Name
	currentLimit := deploy.Spec.Template.Spec.Containers[0].Resources.Limits.Memory()
	if !currentLimit.IsZero() {
		newLimit = *currentLimit
		newLimit.Add(*currentLimit)
	}

	maxLimit := resource.MustParse("1Gi")
	if newLimit.Cmp(maxLimit) > 0 {
		r.notifyUser("Guardrail Blocked Scaling", fmt.Sprintf("Pod %s hit the max memory guardrail (1Gi).", pod.Name))
		return
	}

	msg := fmt.Sprintf("Pod %s ran out of memory. Current limit is insufficient.", pod.Name)
	actionID := fmt.Sprintf("patch_mem|%s|%s|%s|%s", pod.Namespace, deploy.Name, containerName, newLimit.String())
	buttonText := fmt.Sprintf("Approve %s Bump", newLimit.String())

	if r.Slack != nil {
		_ = _ = r.Slack.BroadcastWithAction("Vertical Scaling Required", msg, actionID, buttonText)
	}
	if r.Telegram != nil {
		r.Telegram.BroadcastWithAction("cluster", "Vertical Scaling Required", msg, actionID, buttonText)
	}
	if r.Slack == nil && r.Telegram == nil {
		r.notifyUser("Vertical Scaling Required", msg+fmt.Sprintf("\n\nAction Required: Increase memory to %s manually.", newLimit.String()))
	}
}

//nolint:unused
func (r *KubeSolvConfigReconciler) attemptRestart(ctx context.Context, pod *corev1.Pod) string {
	ownerRef := metav1.GetControllerOf(pod)
	//nolint:goconst
	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
		return ""
	}
	var rs appsv1.ReplicaSet
	if err := r.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: pod.Namespace}, &rs); err != nil {
		return ""
	}
	rsOwner := metav1.GetControllerOf(&rs)
	//nolint:goconst
	if rsOwner == nil || rsOwner.Kind != "Deployment" {
		return ""
	}
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: rsOwner.Name, Namespace: pod.Namespace}, &deploy); err != nil {
		return ""
	}

	patch := fmt.Appendf(nil, `{"spec": {"template": {"metadata": {"annotations": {"kubesolv.io/restartedAt": "%s"}}}}}`, time.Now().Format(time.RFC3339))
	if err := r.Patch(ctx, &deploy, client.RawPatch(types.MergePatchType, patch)); err == nil {
		return fmt.Sprintf("🩹 Restarted Deployment `%s`.", deploy.Name)
	}
	return ""
}

func (r *KubeSolvConfigReconciler) getRecentLogs(ctx context.Context, name, namespace string) (string, error) {
	opts := &corev1.PodLogOptions{SinceSeconds: func(i int64) *int64 { return &i }(5)}
	req := r.ClientSet.CoreV1().Pods(namespace).GetLogs(name, opts)
	logs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = logs.Close() }()
	buf := new(strings.Builder)
	_, err = io.Copy(buf, logs)
	return buf.String(), err
}

func (r *KubeSolvConfigReconciler) getPodLogs(ctx context.Context, name, namespace, container string) (string, error) {
	opts := &corev1.PodLogOptions{Container: container, TailLines: func(i int64) *int64 { return &i }(20)}
	req := r.ClientSet.CoreV1().Pods(namespace).GetLogs(name, opts)
	logs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = logs.Close() }()
	buf := new(strings.Builder)
	_, err = io.Copy(buf, logs)
	return buf.String(), err
}

func (r *KubeSolvConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&opsv1.KubeSolvConfig{}).Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(o)}}
	})).Complete(r)
}

func (r *KubeSolvConfigReconciler) checkPrometheusMetrics(ctx context.Context, pod *corev1.Pod) {
	cacheKey := fmt.Sprintf("metrics/%s/%s", pod.Namespace, pod.Name)
	if lastTime, ok := r.AlertCache.Load(cacheKey); ok {
		if time.Since(lastTime.(time.Time)) < 5*time.Minute {
			return
		}
	}

	cpu, err := r.Prometheus.GetCPUUsage(ctx, pod.Namespace, pod.Name)
	if err == nil && cpu > 0.90 {
		r.AlertCache.Store(cacheKey, time.Now())
		r.notifyUser("High CPU Usage Detected", fmt.Sprintf("Pod `%s` is using %.2f CPU cores. Consider horizontal scaling.", pod.Name, cpu))
	}

	mem, err := r.Prometheus.GetMemoryUsage(ctx, pod.Namespace, pod.Name)
	if err == nil && mem > 800 {
		r.AlertCache.Store(cacheKey, time.Now())
		r.notifyUser("High Memory Usage Detected", fmt.Sprintf("Pod `%s` is using %.2f MB of memory. Nearing OOM limits.", pod.Name, mem))
	}
}

func (r *KubeSolvConfigReconciler) rollbackDeployment(ctx context.Context, namespace, deployName string) error {
	deploy, err := r.ClientSet.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	rsList, err := r.ClientSet.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(deploy.Spec.Selector),
	})
	if err != nil || len(rsList.Items) < 2 {
		return fmt.Errorf("not enough history to rollback")
	}

	var prevRS *appsv1.ReplicaSet
	for i := range rsList.Items {
		rs := &rsList.Items[i]
		if rs.Annotations["deployment.kubernetes.io/revision"] != deploy.Annotations["deployment.kubernetes.io/revision"] {
			if prevRS == nil || rs.CreationTimestamp.After(prevRS.CreationTimestamp.Time) {
				prevRS = rs
			}
		}
	}

	if prevRS == nil {
		return fmt.Errorf("previous replica set not found")
	}

	// Create a deep copy of the template so we can modify it safely
	cleanTemplate := prevRS.Spec.Template.DeepCopy()
	// Strip the system-generated hash so K8s handles the rollout scale-down cleanly
	delete(cleanTemplate.Labels, "pod-template-hash")

	patchBytes, _ := json.Marshal(map[string]any{
		"spec": map[string]any{
			"template": cleanTemplate,
		},
	})

	_, err = r.ClientSet.AppsV1().Deployments(namespace).Patch(ctx, deployName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	return err
}

func (r *KubeSolvConfigReconciler) checkCostOptimization(ctx context.Context, pod *corev1.Pod) {
	if r.Prometheus == nil || r.AI == nil {
		return
	}

	ownerRef := metav1.GetControllerOf(pod)
	//nolint:goconst
	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
		return
	}

	rs, err := r.ClientSet.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ownerRef.Name, metav1.GetOptions{})
	if err != nil {
		return
	}

	rsOwner := metav1.GetControllerOf(rs)
	//nolint:goconst
	if rsOwner == nil || rsOwner.Kind != "Deployment" {
		return
	}

	cacheKey := fmt.Sprintf("cost/%s/%s", pod.Namespace, rsOwner.Name)
	if lastTime, ok := r.AlertCache.Load(cacheKey); ok {
		if time.Since(lastTime.(time.Time)) < 24*time.Hour {
			return
		}
	}

	cpuUsage, err := r.Prometheus.GetCPUUsage(ctx, pod.Namespace, pod.Name)
	if err != nil {
		return
	}

	memUsage, err := r.Prometheus.GetMemoryUsage(ctx, pod.Namespace, pod.Name)
	if err != nil {
		return
	}

	deploy, err := r.ClientSet.AppsV1().Deployments(pod.Namespace).Get(ctx, rsOwner.Name, metav1.GetOptions{})
	if err != nil {
		return
	}

	// Prevent premature evaluation: Wait 15 minutes after deployment creation
	if time.Since(deploy.CreationTimestamp.Time) < 2*time.Minute {
		return
	}

	replicas := *deploy.Spec.Replicas

	// Only trigger AI analysis if running multiple replicas
	if replicas > 1 {
		decision, err := r.AI.AnalyzeCostOptimization(ctx, pod.Namespace, deploy.Name, replicas, cpuUsage, memUsage)
		if err != nil || !decision.Optimize || decision.RecommendedReplicas >= replicas {
			return
		}

		r.AlertCache.Store(cacheKey, time.Now())

		msg := fmt.Sprintf("💰 *AI Cost Optimizer*\n%s\n\n📊 *Current:* %d replicas\n📉 *Suggested:* %d replicas", decision.Reason, replicas, decision.RecommendedReplicas)
		actionID := fmt.Sprintf("scale|%s|%s|%d", pod.Namespace, deploy.Name, decision.RecommendedReplicas)
		buttonText := fmt.Sprintf("Scale Down to %d", decision.RecommendedReplicas)

		if r.Slack != nil {
			_ = r.Slack.BroadcastWithAction("Cost Optimization Opportunity", msg, actionID, buttonText)
		}
		if r.Telegram != nil {
			r.Telegram.BroadcastWithAction("cluster", "Cost Optimization Opportunity", msg, actionID, buttonText)
		}
	}
}
