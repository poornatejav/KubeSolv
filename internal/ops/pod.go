package ops

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeletePod safely deletes a pod from the specified namespace
func (m *OpsManager) DeletePod(ctx context.Context, namespace, podName string) error {
	return m.KubeClient.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
}

// GetPodDetails fetches comprehensive details about a pod's current state
func (m *OpsManager) GetPodDetails(ctx context.Context, namespace, podName string) string {
	pod, err := m.KubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("❌ Error fetching details for pod '%s' in namespace '%s': %v", podName, namespace, err)
	}

	var details strings.Builder
	details.WriteString(fmt.Sprintf("📦 *Pod Details: %s* (Namespace: %s)\n", pod.Name, pod.Namespace))
	details.WriteString(fmt.Sprintf("• Status: `%s`\n", pod.Status.Phase))
	details.WriteString(fmt.Sprintf("• Pod IP: `%s`\n", pod.Status.PodIP))
	details.WriteString(fmt.Sprintf("• Host IP: `%s`\n", pod.Status.HostIP))
	details.WriteString(fmt.Sprintf("• Node: `%s`\n", pod.Spec.NodeName))
	details.WriteString("• Restarts: ")

	totalRestarts := int32(0)
	for _, cs := range pod.Status.ContainerStatuses {
		totalRestarts += cs.RestartCount
	}
	details.WriteString(fmt.Sprintf("`%d`\n", totalRestarts))

	if len(pod.Status.Conditions) > 0 {
		details.WriteString("\n📋 *Conditions:*\n")
		for _, cond := range pod.Status.Conditions {
			icon := "✅"
			if cond.Status != "True" {
				icon = "❌"
			}
			details.WriteString(fmt.Sprintf("%s %s: %s\n", icon, cond.Type, cond.Message))
		}
	}

	return details.String()
}

// ListPods returns a summary of pods in a specific namespace
func (m *OpsManager) ListPods(ctx context.Context, namespace string) string {
	pods, err := m.KubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("❌ Error listing pods in namespace '%s': %v", namespace, err)
	}

	if len(pods.Items) == 0 {
		return fmt.Sprintf("No pods found in namespace '%s'.", namespace)
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("📋 *Pods in Namespace: %s*\n", namespace))
	for _, p := range pods.Items {
		summary.WriteString(fmt.Sprintf("• `%s` (Status: %s)\n", p.Name, p.Status.Phase))
	}

	return summary.String()
}
