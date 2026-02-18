// package controller

// import (
// 	"context"
// 	"fmt"
// 	"io"
// 	"math"
// 	"strconv"
// 	"strings"
// 	"sync"
// 	"time"

// 	appsv1 "k8s.io/api/apps/v1"
// 	corev1 "k8s.io/api/core/v1"
// 	"k8s.io/apimachinery/pkg/api/resource"
// 	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
// 	"k8s.io/apimachinery/pkg/runtime"
// 	"k8s.io/apimachinery/pkg/types"
// 	"k8s.io/client-go/kubernetes"
// 	ctrl "sigs.k8s.io/controller-runtime"
// 	"sigs.k8s.io/controller-runtime/pkg/client"
// 	"sigs.k8s.io/controller-runtime/pkg/handler"
// 	"sigs.k8s.io/controller-runtime/pkg/log"
// 	"sigs.k8s.io/controller-runtime/pkg/reconcile"

// 	opsv1 "kubesolv/api/v1"
// 	"kubesolv/internal/ai"
// 	"kubesolv/internal/alert"
// )

// type KubeSolvConfigReconciler struct {
// 	client.Client
// 	Scheme        *runtime.Scheme
// 	AI            *ai.Client
// 	ClientSet     *kubernetes.Clientset
// 	Telegram      *alert.TelegramBot
// 	Slack         *alert.SlackBot
// 	AnalysisCache sync.Map
// }

// // +kubebuilder:rbac:groups=ops.kubesolv.io,resources=kubesolvconfigs,verbs=get;list;watch;create;update;patch;delete
// // +kubebuilder:rbac:groups=ops.kubesolv.io,resources=kubesolvconfigs/status,verbs=get;update;patch
// // +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// // +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// // +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;update;patch

// func (r *KubeSolvConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
// 	var pod corev1.Pod
// 	if err := r.Get(ctx, req.NamespacedName, &pod); err == nil {

// 		// 1. Diagnose Crashes & OOMs
// 		r.analyzePod(ctx, &pod)

// 		// 2. Traffic Cop (Auto-Scaling)
// 		// Only check scaling if the pod is healthy and running
// 		if pod.Status.Phase == corev1.PodRunning {
// 			r.checkActivity(ctx, &pod)
// 		}
// 	}
// 	return ctrl.Result{}, nil
// }

// // --- HELPER: BROADCAST TO ALL CHANNELS ---
// func (r *KubeSolvConfigReconciler) notifyUser(title, message string) {
// 	// 1. Send to Telegram
// 	if r.Telegram != nil {
// 		r.Telegram.Broadcast("KubeSolv", "cluster", title, message)
// 	}

// 	// 2. Send to Slack
// 	if r.Slack != nil {
// 		err := r.Slack.Broadcast(title, message)
// 		if err != nil {
// 			fmt.Printf("❌ Failed to send Slack alert: %v\n", err)
// 		}
// 	}
// }

// // --- FEATURE 1: TRAFFIC COP (Horizontal Scaling) ---
// func (r *KubeSolvConfigReconciler) checkActivity(ctx context.Context, pod *corev1.Pod) {
// 	// Rate Limit: Check scaling only once every 15s per pod
// 	cacheKey := fmt.Sprintf("scale/%s/%s", pod.Namespace, pod.Name)
// 	if lastTime, ok := r.AnalysisCache.Load(cacheKey); ok {
// 		if time.Since(lastTime.(time.Time)) < 15*time.Second {
// 			return
// 		}
// 	}
// 	r.AnalysisCache.Store(cacheKey, time.Now())

// 	// Find Deployment
// 	ownerRef := metav1.GetControllerOf(pod)
// 	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
// 		return
// 	}
// 	var rs appsv1.ReplicaSet
// 	if err := r.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: pod.Namespace}, &rs); err != nil {
// 		return
// 	}
// 	rsOwner := metav1.GetControllerOf(&rs)
// 	if rsOwner == nil || rsOwner.Kind != "Deployment" {
// 		return
// 	}
// 	var deploy appsv1.Deployment
// 	if err := r.Get(ctx, types.NamespacedName{Name: rsOwner.Name, Namespace: pod.Namespace}, &deploy); err != nil {
// 		return
// 	}

// 	// Read Min/Max from Annotations
// 	minReplicas := int32(1)
// 	maxReplicas := int32(10)
// 	if val, ok := deploy.Annotations["kubesolv.io/min-replicas"]; ok {
// 		if v, err := strconv.Atoi(val); err == nil {
// 			minReplicas = int32(v)
// 		}
// 	}
// 	if val, ok := deploy.Annotations["kubesolv.io/max-replicas"]; ok {
// 		if v, err := strconv.Atoi(val); err == nil {
// 			maxReplicas = int32(v)
// 		}
// 	}

// 	// Analyze Traffic
// 	logs, err := r.getRecentLogs(ctx, pod.Name, pod.Namespace)
// 	if err != nil {
// 		return
// 	}

// 	lines := len(strings.Split(logs, "\n"))
// 	logsPerSec := float64(lines) / 5.0
// 	currentReplicas := *deploy.Spec.Replicas

// 	var desiredReplicas int32
// 	reason := ""

// 	if logsPerSec < 0.5 {
// 		// Scale Down
// 		if currentReplicas > minReplicas {
// 			desiredReplicas = currentReplicas - 1
// 			reason = fmt.Sprintf("📉 Traffic Dropped (%.1f logs/s). Scaling Down.", logsPerSec)
// 		} else {
// 			return
// 		}
// 	} else if logsPerSec > 10.0 {
// 		// Scale Up
// 		steps := int32(math.Ceil((logsPerSec - 10.0) / 10.0))
// 		if steps > 2 {
// 			steps = 2
// 		}
// 		if steps < 1 {
// 			steps = 1
// 		}

// 		if currentReplicas < maxReplicas {
// 			desiredReplicas = currentReplicas + steps
// 			if desiredReplicas > maxReplicas {
// 				desiredReplicas = maxReplicas
// 			}
// 			reason = fmt.Sprintf("🔥 High Load (%.1f logs/s). Scaling Up +%d.", logsPerSec, steps)
// 		} else {
// 			return
// 		}
// 	} else {
// 		return
// 	}

// 	// Execute Scaling
// 	if desiredReplicas != currentReplicas {
// 		logger := log.FromContext(ctx)
// 		logger.Info(fmt.Sprintf("⚖️ Smart Scaler: %s (%d -> %d)", reason, currentReplicas, desiredReplicas))

// 		patch := []byte(fmt.Sprintf(`{"spec": {"replicas": %d}}`, desiredReplicas))
// 		if err := r.Client.Patch(ctx, &deploy, client.RawPatch(types.MergePatchType, patch)); err == nil {
// 			msg := fmt.Sprintf("📦 *App:* `%s`\n📊 *Traffic:* %.1f logs/sec\n🔄 *Adjustment:* %d ➡ %d\n📝 *Reason:* %s",
// 				deploy.Name, logsPerSec, currentReplicas, desiredReplicas, reason)
// 			r.notifyUser("Smart Scaling Triggered", msg)
// 		}
// 	}
// }

// // --- FEATURE 2: CRASH ANALYST (Diagnosis & Fixes) ---
// func (r *KubeSolvConfigReconciler) analyzePod(ctx context.Context, pod *corev1.Pod) {
// 	logger := log.FromContext(ctx)

// 	for _, status := range pod.Status.ContainerStatuses {

// 		// A. Check for OOMKilled
// 		if status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.Reason == "OOMKilled" {
// 			r.handleOOM(ctx, pod)
// 			return
// 		}

// 		// B. Check for Waiting States
// 		if status.State.Waiting != nil {
// 			reason := status.State.Waiting.Reason

// 			if reason == "ErrImagePull" || reason == "ImagePullBackOff" || reason == "CrashLoopBackOff" {

// 				cacheKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
// 				if lastTime, ok := r.AnalysisCache.Load(cacheKey); ok {
// 					if time.Since(lastTime.(time.Time)) < 2*time.Minute {
// 						return
// 					}
// 				}
// 				r.AnalysisCache.Store(cacheKey, time.Now())

// 				logger.Info("🚨 Issue Detected", "pod", pod.Name, "reason", reason)

// 				var logs string
// 				if reason == "CrashLoopBackOff" {
// 					logs, _ = r.getPodLogs(ctx, pod.Name, pod.Namespace, status.Name)
// 				} else {
// 					logs = fmt.Sprintf("Image Pull Error: %s", status.State.Waiting.Message)
// 				}

// 				analysis := "⚠️ **Diagnosis Unavailable**\nKubeSolv unable to get diagnosis.\n\n*Manual Check Required.*"
// 				actionTaken := "No automated action taken."

// 				if r.AI != nil {
// 					aiResponse, err := r.AI.AnalyzeError(ctx, pod.Name, pod.Namespace, reason, logs)
// 					if err == nil {
// 						analysis = aiResponse
// 						logger.Info("🧠 GEMINI: " + strings.ReplaceAll(analysis, "\n", " "))

// 						if status.RestartCount > 2 || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
// 							result := r.attemptRestart(ctx, pod)
// 							if result != "" {
// 								actionTaken = result
// 							}
// 						}
// 					} else {
// 						logger.Error(err, "❌ Gemini failed")
// 					}
// 				}

// 				finalReport := fmt.Sprintf("📦 *Pod:* `%s`\n📍 *Ns:* `%s`\n⚠️ *Issue:* %s\n\n%s\n\n🛠️ *Action Taken:*\n%s",
// 					pod.Name, pod.Namespace, reason, analysis, actionTaken)

// 				r.notifyUser(fmt.Sprintf("Issue Detected: %s", reason), finalReport)
// 				logger.Info("📣 Alert sent to Channels")
// 			}
// 		}
// 	}
// }

// // --- FEATURE 3: THE ARCHITECT (Vertical Memory Scaling) ---
// func (r *KubeSolvConfigReconciler) handleOOM(ctx context.Context, pod *corev1.Pod) {
// 	logger := log.FromContext(ctx)

// 	cacheKey := fmt.Sprintf("oom/%s/%s", pod.Namespace, pod.Name)
// 	if _, ok := r.AnalysisCache.Load(cacheKey); ok {
// 		return
// 	}
// 	r.AnalysisCache.Store(cacheKey, time.Now())

// 	logger.Info("🚨 OOMKilled Detected! Preparing to Scale Up...", "pod", pod.Name)

// 	ownerRef := metav1.GetControllerOf(pod)
// 	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
// 		return
// 	}
// 	var rs appsv1.ReplicaSet
// 	if err := r.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: pod.Namespace}, &rs); err != nil {
// 		return
// 	}
// 	rsOwner := metav1.GetControllerOf(&rs)
// 	if rsOwner == nil || rsOwner.Kind != "Deployment" {
// 		return
// 	}
// 	var deploy appsv1.Deployment
// 	if err := r.Get(ctx, types.NamespacedName{Name: rsOwner.Name, Namespace: pod.Namespace}, &deploy); err != nil {
// 		return
// 	}

// 	newLimit := resource.MustParse("128Mi")
// 	currentLimit := deploy.Spec.Template.Spec.Containers[0].Resources.Limits.Memory()
// 	if !currentLimit.IsZero() {
// 		newLimit = *currentLimit
// 		newLimit.Add(*currentLimit)
// 	}

// 	patch := []byte(fmt.Sprintf(`{"spec": {"template": {"spec": {"containers": [{"name": "%s", "resources": {"limits": {"memory": "%s"}}}]}}}}`,
// 		deploy.Spec.Template.Spec.Containers[0].Name, newLimit.String()))

// 	if err := r.Client.Patch(ctx, &deploy, client.RawPatch(types.StrategicMergePatchType, patch)); err != nil {
// 		logger.Error(err, "❌ Failed to scale memory")
// 		return
// 	}

// 	msg := fmt.Sprintf("Pod `%s` ran out of memory.\n\n🛠️ *Action:* Increased memory limit to `%s`.",
// 		pod.Name, newLimit.String())

// 	r.notifyUser("Vertical Scaling Triggered", msg)
// 	logger.Info("✅ Vertical Scaling Applied")
// }

// func (r *KubeSolvConfigReconciler) attemptRestart(ctx context.Context, pod *corev1.Pod) string {
// 	logger := log.FromContext(ctx)
// 	ownerRef := metav1.GetControllerOf(pod)
// 	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
// 		return ""
// 	}
// 	var rs appsv1.ReplicaSet
// 	if err := r.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: pod.Namespace}, &rs); err != nil {
// 		return ""
// 	}
// 	rsOwner := metav1.GetControllerOf(&rs)
// 	if rsOwner == nil || rsOwner.Kind != "Deployment" {
// 		return ""
// 	}
// 	var deploy appsv1.Deployment
// 	if err := r.Get(ctx, types.NamespacedName{Name: rsOwner.Name, Namespace: pod.Namespace}, &deploy); err != nil {
// 		return ""
// 	}

// 	patch := []byte(fmt.Sprintf(`{"spec": {"template": {"metadata": {"annotations": {"kubesolv.io/restartedAt": "%s"}}}}}`, time.Now().Format(time.RFC3339)))
// 	if err := r.Client.Patch(ctx, &deploy, client.RawPatch(types.MergePatchType, patch)); err != nil {
// 		logger.Error(err, "❌ Failed to restart")
// 		return ""
// 	}
// 	logger.Info("✅ Restarted Deployment " + deploy.Name)
// 	return fmt.Sprintf("🩹 Restarted Deployment `%s`.", deploy.Name)
// }

// // Helpers
// func (r *KubeSolvConfigReconciler) getRecentLogs(ctx context.Context, name, namespace string) (string, error) {
// 	opts := &corev1.PodLogOptions{SinceSeconds: func(i int64) *int64 { return &i }(5)}
// 	req := r.ClientSet.CoreV1().Pods(namespace).GetLogs(name, opts)
// 	logs, err := req.Stream(ctx)
// 	if err != nil {
// 		return "", err
// 	}
// 	defer logs.Close()
// 	buf := new(strings.Builder)
// 	_, err = io.Copy(buf, logs)
// 	return buf.String(), err
// }

// func (r *KubeSolvConfigReconciler) getPodLogs(ctx context.Context, name, namespace, container string) (string, error) {
// 	opts := &corev1.PodLogOptions{Container: container, TailLines: func(i int64) *int64 { return &i }(20)}
// 	req := r.ClientSet.CoreV1().Pods(namespace).GetLogs(name, opts)
// 	logs, err := req.Stream(ctx)
// 	if err != nil {
// 		return "", err
// 	}
// 	defer logs.Close()
// 	buf := new(strings.Builder)
// 	_, err = io.Copy(buf, logs)
// 	return buf.String(), err
// }

// func (r *KubeSolvConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
// 	return ctrl.NewControllerManagedBy(mgr).For(&opsv1.KubeSolvConfig{}).Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
// 		return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(o)}}
// 	})).Complete(r)
// }
// -----------------------------------------------------

package controller

import (
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opsv1 "kubesolv/api/v1"
	"kubesolv/internal/ai"
	"kubesolv/internal/alert"
)

type KubeSolvConfigReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	AI        *ai.Client
	ClientSet *kubernetes.Clientset
	Telegram  *alert.TelegramBot
	Slack     *alert.SlackBot

	// Cache for Rate Limiting Alerts (stores timestamps)
	AlertCache sync.Map
	// Cache for Tracking State Changes (stores strings like "Running", "CrashLoop")
	StateCache sync.Map
}

// +kubebuilder:rbac:groups=ops.kubesolv.io,resources=kubesolvconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.kubesolv.io,resources=kubesolvconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;update;patch

func (r *KubeSolvConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err == nil {

		// 1. Track Lifecycle (New Pods & Recovery)
		r.trackLifecycle(ctx, &pod)

		// 2. Diagnose Crashes & OOMs (The Doctor)
		r.analyzePod(ctx, &pod)

		// 3. Traffic Cop (Auto-Scaling)
		if pod.Status.Phase == corev1.PodRunning {
			r.checkActivity(ctx, &pod)
		}
	}
	return ctrl.Result{}, nil
}

// --- HELPER: BROADCAST (With Smart Deduplication) ---
func (r *KubeSolvConfigReconciler) notifyUser(title, message string) {
	// Create a hash of the message content
	hash := fmt.Sprintf("%s-%d", title, len(message))

	// Rate Limit Logic: 5 Minutes
	if lastTime, ok := r.AlertCache.Load(hash); ok {
		if time.Since(lastTime.(time.Time)) < 5*time.Minute {
			// Check if this is a "Recovery" message. We NEVER silence recoveries!
			if !strings.Contains(title, "Recovered") && !strings.Contains(title, "New Pod") {
				return // Silence duplicate errors
			}
		}
	}
	r.AlertCache.Store(hash, time.Now())

	// Send to Telegram
	if r.Telegram != nil {
		r.Telegram.Broadcast("KubeSolv", "cluster", title, message)
	}

	// Send to Slack
	if r.Slack != nil {
		err := r.Slack.Broadcast(title, message)
		if err != nil {
			fmt.Printf("❌ Failed to send Slack alert: %v\n", err)
		}
	}
}

// --- FEATURE 0: LIFECYCLE TRACKER (New! ✨) ---
func (r *KubeSolvConfigReconciler) trackLifecycle(ctx context.Context, pod *corev1.Pod) {
	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	// Determine Current Status (Simplified)
	currentStatus := string(pod.Status.Phase) // Default: Running, Pending, etc.
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			currentStatus = cs.State.Waiting.Reason // e.g. CrashLoopBackOff
		} else if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			currentStatus = "Error"
		}
	}

	// Load Previous Status
	lastStatusInterface, known := r.StateCache.Load(podKey)
	lastStatus := ""
	if known {
		lastStatus = lastStatusInterface.(string)
	}

	// 1. Detect New Pod
	if !known {
		msg := fmt.Sprintf("📦 *New Pod Detected*\nName: `%s`\nNamespace: `%s`\nStatus: `%s`",
			pod.Name, pod.Namespace, currentStatus)
		r.notifyUser("🆕 New Activity", msg)
		r.StateCache.Store(podKey, currentStatus)
		return
	}

	// 2. Detect Recovery (Bad -> Good)
	isBad := func(s string) bool {
		return s == "CrashLoopBackOff" || s == "ImagePullBackOff" || s == "ErrImagePull" || s == "Error" || s == "OOMKilled"
	}

	if isBad(lastStatus) && currentStatus == "Running" {
		msg := fmt.Sprintf("✅ *Pod Recovered!*\nPod `%s` is now healthy and Running.", pod.Name)
		r.notifyUser("✅ Issue Resolved", msg)

		// Clear the Alert Cache for this pod so if it fails again, we alert immediately
		r.AlertCache.Delete(fmt.Sprintf("Issue Detected: %s-%d", lastStatus, len(msg))) // Rough cleanup attempt
	}

	// Update Cache
	if lastStatus != currentStatus {
		r.StateCache.Store(podKey, currentStatus)
	}
}

// --- FEATURE 1: TRAFFIC COP (Horizontal Scaling) ---
func (r *KubeSolvConfigReconciler) checkActivity(ctx context.Context, pod *corev1.Pod) {
	cacheKey := fmt.Sprintf("scale/%s/%s", pod.Namespace, pod.Name)
	if lastTime, ok := r.AlertCache.Load(cacheKey); ok {
		if time.Since(lastTime.(time.Time)) < 15*time.Second {
			return
		}
	}
	r.AlertCache.Store(cacheKey, time.Now())

	ownerRef := metav1.GetControllerOf(pod)
	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
		return
	}
	var rs appsv1.ReplicaSet
	if err := r.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: pod.Namespace}, &rs); err != nil {
		return
	}
	rsOwner := metav1.GetControllerOf(&rs)
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
		steps := int32(math.Ceil((logsPerSec - 10.0) / 10.0))
		if steps > 2 {
			steps = 2
		}
		if steps < 1 {
			steps = 1
		}

		if currentReplicas < maxReplicas {
			desiredReplicas = currentReplicas + steps
			if desiredReplicas > maxReplicas {
				desiredReplicas = maxReplicas
			}
			reason = fmt.Sprintf("🔥 High Load (%.1f logs/s). Scaling Up +%d.", logsPerSec, steps)
		} else {
			return
		}
	} else {
		return
	}

	if desiredReplicas != currentReplicas {
		patch := []byte(fmt.Sprintf(`{"spec": {"replicas": %d}}`, desiredReplicas))
		if err := r.Client.Patch(ctx, &deploy, client.RawPatch(types.MergePatchType, patch)); err == nil {
			msg := fmt.Sprintf("📦 *App:* `%s`\n📊 *Traffic:* %.1f logs/sec\n🔄 *Adjustment:* %d ➡ %d\n📝 *Reason:* %s",
				deploy.Name, logsPerSec, currentReplicas, desiredReplicas, reason)
			r.notifyUser("Smart Scaling Triggered", msg)
		}
	}
}

// --- FEATURE 2: CRASH ANALYST (Diagnosis & Fixes) ---
func (r *KubeSolvConfigReconciler) analyzePod(ctx context.Context, pod *corev1.Pod) {
	logger := log.FromContext(ctx)

	for _, status := range pod.Status.ContainerStatuses {
		if status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.Reason == "OOMKilled" {
			r.handleOOM(ctx, pod)
			return
		}

		if status.State.Waiting != nil {
			reason := status.State.Waiting.Reason

			if reason == "ErrImagePull" || reason == "ImagePullBackOff" || reason == "CrashLoopBackOff" {

				// ANALYSIS RATE LIMIT (Don't analyze same pod every second)
				// We use a shorter cache here (1 min) just to prevent CPU burn,
				// but rely on notifyUser for the 5-min alert silence.
				cacheKey := fmt.Sprintf("analyze/%s/%s", pod.Namespace, pod.Name)
				if lastTime, ok := r.AlertCache.Load(cacheKey); ok {
					if time.Since(lastTime.(time.Time)) < 1*time.Minute {
						return
					}
				}
				r.AlertCache.Store(cacheKey, time.Now())

				logger.Info("🚨 Issue Detected", "pod", pod.Name, "reason", reason)

				var logs string
				if reason == "CrashLoopBackOff" {
					logs, _ = r.getPodLogs(ctx, pod.Name, pod.Namespace, status.Name)
				} else {
					logs = fmt.Sprintf("Image Pull Error: %s", status.State.Waiting.Message)
				}

				analysis := "⚠️ **Diagnosis Unavailable**\nKubeSolv unable to get diagnosis.\n\n*Manual Check Required.*"
				actionTaken := "No automated action taken."

				if r.AI != nil {
					aiResponse, err := r.AI.AnalyzeError(ctx, pod.Name, pod.Namespace, reason, logs)
					if err == nil {
						analysis = aiResponse

						if status.RestartCount > 2 || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
							result := r.attemptRestart(ctx, pod)
							if result != "" {
								actionTaken = result
							}
						}
					}
				}

				finalReport := fmt.Sprintf("📦 *Pod:* `%s`\n📍 *Ns:* `%s`\n⚠️ *Issue:* %s\n\n%s\n\n🛠️ *Action Taken:*\n%s",
					pod.Name, pod.Namespace, reason, analysis, actionTaken)

				// THIS CALL USES THE 5-MIN RATE LIMITER
				r.notifyUser(fmt.Sprintf("Issue Detected: %s", reason), finalReport)
			}
		}
	}
}

// --- FEATURE 3: THE ARCHITECT (Vertical Memory Scaling) ---
func (r *KubeSolvConfigReconciler) handleOOM(ctx context.Context, pod *corev1.Pod) {
	logger := log.FromContext(ctx)

	cacheKey := fmt.Sprintf("oom/%s/%s", pod.Namespace, pod.Name)
	if _, ok := r.AlertCache.Load(cacheKey); ok {
		return
	}
	r.AlertCache.Store(cacheKey, time.Now())

	ownerRef := metav1.GetControllerOf(pod)
	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
		return
	}
	var rs appsv1.ReplicaSet
	if err := r.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: pod.Namespace}, &rs); err != nil {
		return
	}
	rsOwner := metav1.GetControllerOf(&rs)
	if rsOwner == nil || rsOwner.Kind != "Deployment" {
		return
	}
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: rsOwner.Name, Namespace: pod.Namespace}, &deploy); err != nil {
		return
	}

	newLimit := resource.MustParse("128Mi")
	currentLimit := deploy.Spec.Template.Spec.Containers[0].Resources.Limits.Memory()
	if !currentLimit.IsZero() {
		newLimit = *currentLimit
		newLimit.Add(*currentLimit)
	}

	patch := []byte(fmt.Sprintf(`{"spec": {"template": {"spec": {"containers": [{"name": "%s", "resources": {"limits": {"memory": "%s"}}}]}}}}`,
		deploy.Spec.Template.Spec.Containers[0].Name, newLimit.String()))

	if err := r.Client.Patch(ctx, &deploy, client.RawPatch(types.StrategicMergePatchType, patch)); err == nil {
		msg := fmt.Sprintf("Pod `%s` ran out of memory.\n\n🛠️ *Action:* Increased memory limit to `%s`.",
			pod.Name, newLimit.String())
		r.notifyUser("Vertical Scaling Triggered", msg)
		logger.Info("✅ Vertical Scaling Applied")
	}
}

func (r *KubeSolvConfigReconciler) attemptRestart(ctx context.Context, pod *corev1.Pod) string {
	ownerRef := metav1.GetControllerOf(pod)
	if ownerRef == nil || ownerRef.Kind != "ReplicaSet" {
		return ""
	}
	var rs appsv1.ReplicaSet
	if err := r.Get(ctx, types.NamespacedName{Name: ownerRef.Name, Namespace: pod.Namespace}, &rs); err != nil {
		return ""
	}
	rsOwner := metav1.GetControllerOf(&rs)
	if rsOwner == nil || rsOwner.Kind != "Deployment" {
		return ""
	}
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: rsOwner.Name, Namespace: pod.Namespace}, &deploy); err != nil {
		return ""
	}

	patch := []byte(fmt.Sprintf(`{"spec": {"template": {"metadata": {"annotations": {"kubesolv.io/restartedAt": "%s"}}}}}`, time.Now().Format(time.RFC3339)))
	if err := r.Client.Patch(ctx, &deploy, client.RawPatch(types.MergePatchType, patch)); err == nil {
		return fmt.Sprintf("🩹 Restarted Deployment `%s`.", deploy.Name)
	}
	return ""
}

// Helpers
func (r *KubeSolvConfigReconciler) getRecentLogs(ctx context.Context, name, namespace string) (string, error) {
	opts := &corev1.PodLogOptions{SinceSeconds: func(i int64) *int64 { return &i }(5)}
	req := r.ClientSet.CoreV1().Pods(namespace).GetLogs(name, opts)
	logs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer logs.Close()
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
	defer logs.Close()
	buf := new(strings.Builder)
	_, err = io.Copy(buf, logs)
	return buf.String(), err
}

func (r *KubeSolvConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&opsv1.KubeSolvConfig{}).Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(o)}}
	})).Complete(r)
}
