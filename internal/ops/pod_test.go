package ops

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeletePod_Success(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app-1", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}

	m := newFakeManager(pod)
	err := m.DeletePod(context.Background(), "default", "app-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify pod is gone
	_, err = m.KubeClient.CoreV1().Pods("default").Get(context.Background(), "app-1", metav1.GetOptions{})
	if err == nil {
		t.Fatal("expected pod to be deleted")
	}
}

func TestDeletePod_NotFound(t *testing.T) {
	m := newFakeManager()
	err := m.DeletePod(context.Background(), "default", "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent pod")
	}
}

func TestGetPodDetails_Running(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app-1", Namespace: "default"},
		Spec:       corev1.PodSpec{NodeName: "worker-1"},
		Status: corev1.PodStatus{
			Phase:  corev1.PodRunning,
			PodIP:  "10.0.0.5",
			HostIP: "192.168.1.10",
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", RestartCount: 2},
			},
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue, Message: "Pod is ready"},
			},
		},
	}

	m := newFakeManager(pod)
	result := m.GetPodDetails(context.Background(), "default", "app-1")

	if !contains(result, "app-1") {
		t.Errorf("expected pod name in details, got: %s", result)
	}
	if !contains(result, "10.0.0.5") {
		t.Errorf("expected pod IP in details, got: %s", result)
	}
	if !contains(result, "worker-1") {
		t.Errorf("expected node name in details, got: %s", result)
	}
	if !contains(result, "2") {
		t.Errorf("expected restart count in details, got: %s", result)
	}
}

func TestGetPodDetails_NotFound(t *testing.T) {
	m := newFakeManager()
	result := m.GetPodDetails(context.Background(), "default", "nonexistent")

	if !contains(result, "Error") {
		t.Errorf("expected error message, got: %s", result)
	}
}

func TestListPods_WithPods(t *testing.T) {
	pods := &corev1.PodList{
		Items: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "app-1", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			{ObjectMeta: metav1.ObjectMeta{Name: "app-2", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodPending}},
		},
	}

	m := newFakeManager(pods)
	result := m.ListPods(context.Background(), "default")

	if !contains(result, "app-1") {
		t.Errorf("expected app-1 in list, got: %s", result)
	}
	if !contains(result, "app-2") {
		t.Errorf("expected app-2 in list, got: %s", result)
	}
}

func TestListPods_EmptyNamespace(t *testing.T) {
	m := newFakeManager()
	result := m.ListPods(context.Background(), "empty-ns")

	if !contains(result, "No pods found") {
		t.Errorf("expected no pods message, got: %s", result)
	}
}
