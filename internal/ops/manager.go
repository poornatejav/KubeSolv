package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// OpsManager handles all cluster operations and reporting
type OpsManager struct {
	KubeClient    *kubernetes.Clientset
	MetricsClient *metricsv.Clientset
}

func NewManager(k *kubernetes.Clientset, m *metricsv.Clientset) *OpsManager {
	return &OpsManager{KubeClient: k, MetricsClient: m}
}

// GetHealthReport generates a formatted status of the cluster
func (m *OpsManager) GetHealthReport() string {
	pods, err := m.KubeClient.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "❌ Error reading cluster status."
	}

	uRun, uFail, uPend := 0, 0, 0
	sRun, sFail, sPend := 0, 0, 0

	for _, p := range pods.Items {
		isSystem := (p.Namespace == "kube-system" || p.Namespace == "local-path-storage" || p.Namespace == "ingress-nginx")
		switch p.Status.Phase {
		case "Running":
			if isSystem {
				sRun++
			} else {
				uRun++
			}
		case "Failed", "Unknown":
			if isSystem {
				sFail++
			} else {
				uFail++
			}
		case "Pending":
			if isSystem {
				sPend++
			} else {
				uPend++
			}
		}
	}

	emoji := "🟢"
	summary := "✨ *Cluster is Healthy*"
	if uFail > 0 || uPend > 0 {
		emoji = "⚠️"
		summary = "🔥 *User Apps Degrading*"
	} else if sFail > 0 {
		emoji = "🔧"
		summary = "⚠️ *System Infrastructure is unstable.*"
	}

	return fmt.Sprintf("%s *Health Report*\n\n👤 *User Apps:*\n   ✅ %d | ❌ %d | ⏳ %d\n\n⚙️ *System Infra:*\n   ✅ %d | ❌ %d | ⏳ %d\n\n%s",
		emoji, uRun, uFail, uPend, sRun, sFail, sPend, summary)
}

// GetResourceUsage fetches CPU/Memory stats from the Metrics Server
func (m *OpsManager) GetResourceUsage() string {
	if m.MetricsClient == nil {
		return "⚠️ Metrics API unavailable. (Is Metrics Server installed?)"
	}

	metrics, err := m.MetricsClient.MetricsV1beta1().PodMetricses("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "❌ Failed to fetch metrics. Is the Metrics Server running?"
	}

	var sb strings.Builder
	sb.WriteString("📊 *Top Resource Consumers*\n\n")

	hasData := false
	for _, pod := range metrics.Items {
		if pod.Namespace == "kube-system" {
			continue
		}

		var cpu int64 = 0
		var mem int64 = 0
		for _, c := range pod.Containers {
			cpu += c.Usage.Cpu().MilliValue()
			mem += c.Usage.Memory().Value() / (1024 * 1024)
		}

		// Threshold: >10m CPU or >50Mi Memory to reduce noise
		if cpu > 10 || mem > 50 {
			hasData = true
			sb.WriteString(fmt.Sprintf("🔹 *%s* (`%s`)\n    CPU: `%dm` | RAM: `%dMi`\n", pod.Name, pod.Namespace, cpu, mem))
		}
	}

	if !hasData {
		return "💤 Cluster is idle (Low resource usage)."
	}
	return sb.String()
}

// GetRecentEvents fetches warnings from the last hour
func (m *OpsManager) GetRecentEvents() string {
	events, err := m.KubeClient.CoreV1().Events("").List(context.TODO(), metav1.ListOptions{Limit: 15})
	if err != nil {
		return "❌ Error reading events."
	}

	var sb strings.Builder
	sb.WriteString("📢 *Recent Cluster Events*\n\n")

	count := 0
	for _, e := range events.Items {
		if e.Type == "Warning" || (e.Reason != "Scheduled" && e.Reason != "Pulling" && e.Reason != "Pulled") {
			icon := "ℹ️"
			if e.Type == "Warning" {
				icon = "⚠️"
			}

			sb.WriteString(fmt.Sprintf("%s *%s*: %s (%s)\n", icon, e.InvolvedObject.Name, e.Message, e.Reason))
			count++
		}
	}

	if count == 0 {
		return "✅ No critical events found."
	}
	return sb.String()
}

// GetLogs fetches the last 15 lines of a pod matching the name
func (m *OpsManager) GetLogs(podName string) string {
	// Fuzzy Search in Default NS
	pods, _ := m.KubeClient.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{})
	target := ""
	for _, p := range pods.Items {
		if strings.Contains(p.Name, podName) {
			target = p.Name
			break
		}
	}

	if target == "" {
		return fmt.Sprintf("❌ Pod containing `%s` not found in default namespace.", podName)
	}

	req := m.KubeClient.CoreV1().Pods("default").GetLogs(target, &corev1.PodLogOptions{
		TailLines: func(i int64) *int64 { return &i }(15),
	})

	logs, err := req.DoRaw(context.TODO())
	if err != nil {
		return fmt.Sprintf("❌ Failed to read logs: %v", err)
	}

	return fmt.Sprintf("📜 *Logs for %s:*\n```%s```", target, string(logs))
}

func (m *OpsManager) PatchMemoryLimit(namespace, deployName, containerName, newLimit string) error {
	patch := []byte(fmt.Sprintf(`{"spec": {"template": {"spec": {"containers": [{"name": "%s", "resources": {"limits": {"memory": "%s"}}}]}}}}`, containerName, newLimit))
	_, err := m.KubeClient.AppsV1().Deployments(namespace).Patch(context.TODO(), deployName, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (m *OpsManager) RollbackDeployment(namespace, deployName string) error {
	// A simple rollback simulation: Annotate to force a re-evaluation/restart
	// In a full production scenario, this queries ReplicaSets and restores the previous PodTemplateSpec.
	patch := []byte(fmt.Sprintf(`{"spec": {"template": {"metadata": {"annotations": {"kubesolv.io/rollbackTriggeredAt": "%s"}}}}}`, time.Now().Format(time.RFC3339)))
	_, err := m.KubeClient.AppsV1().Deployments(namespace).Patch(context.TODO(), deployName, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
}
