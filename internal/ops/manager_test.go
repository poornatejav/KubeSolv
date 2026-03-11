package ops

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func newFakeManager(objects ...runtime.Object) *OpsManager {
	client := fake.NewSimpleClientset(objects...)
	return &OpsManager{KubeClient: client, MetricsClient: nil}
}

func int32Ptr(i int32) *int32 { return &i }

// --- GetHealthReport Tests ---

func TestGetHealthReport_HealthyCluster(t *testing.T) {
	pods := &corev1.PodList{
		Items: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "app-1", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			{ObjectMeta: metav1.ObjectMeta{Name: "app-2", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			{ObjectMeta: metav1.ObjectMeta{Name: "kube-dns", Namespace: "kube-system"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		},
	}
	m := newFakeManager(pods)
	report := m.GetHealthReport()

	if report == "" {
		t.Fatal("expected non-empty report")
	}
	if !contains(report, "Healthy") {
		t.Errorf("expected healthy report, got: %s", report)
	}
}

func TestGetHealthReport_DegradedCluster(t *testing.T) {
	pods := &corev1.PodList{
		Items: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "app-1", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			{ObjectMeta: metav1.ObjectMeta{Name: "app-2", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodFailed}},
		},
	}
	m := newFakeManager(pods)
	report := m.GetHealthReport()

	if !contains(report, "Degrading") {
		t.Errorf("expected degraded report, got: %s", report)
	}
}

func TestGetHealthReport_EmptyCluster(t *testing.T) {
	m := newFakeManager()
	report := m.GetHealthReport()

	if report == "" {
		t.Fatal("expected non-empty report")
	}
	// No pods at all should show healthy (0 failed)
	if !contains(report, "Healthy") {
		t.Errorf("expected healthy when no pods, got: %s", report)
	}
}

// --- GetResourceUsage Tests ---

func TestGetResourceUsage_NoMetricsClient(t *testing.T) {
	m := newFakeManager()
	result := m.GetResourceUsage()

	if !contains(result, "unavailable") {
		t.Errorf("expected unavailable message, got: %s", result)
	}
}

// --- GetRecentEvents Tests ---

func TestGetRecentEvents_NoEvents(t *testing.T) {
	m := newFakeManager()
	result := m.GetRecentEvents()

	if !contains(result, "No critical events") {
		t.Errorf("expected no events, got: %s", result)
	}
}

func TestGetRecentEvents_WithWarnings(t *testing.T) {
	event := &corev1.EventList{
		Items: []corev1.Event{
			{
				ObjectMeta:     metav1.ObjectMeta{Name: "ev-1", Namespace: "default"},
				InvolvedObject: corev1.ObjectReference{Name: "app-1"},
				Type:           "Warning",
				Reason:         "FailedScheduling",
				Message:        "No nodes available",
			},
		},
	}
	m := newFakeManager(event)
	result := m.GetRecentEvents()

	if !contains(result, "No nodes available") {
		t.Errorf("expected warning event in output, got: %s", result)
	}
}

// --- GetLogs Tests ---

func TestGetLogs_PodNotFound(t *testing.T) {
	m := newFakeManager()
	result := m.GetLogs("nonexistent")

	if !contains(result, "not found") {
		t.Errorf("expected not found message, got: %s", result)
	}
}

func TestGetLogs_PodFoundByFuzzyMatch(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app-abc123", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	m := newFakeManager(pod)
	// The fake client won't have actual logs, but it should at least find the pod
	result := m.GetLogs("my-app")

	if contains(result, "not found") {
		t.Errorf("expected pod to be found by fuzzy match, got: %s", result)
	}
}

// --- PatchMemoryLimit Tests ---

func TestPatchMemoryLimit_Success(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "web", Image: "nginx"},
					},
				},
			},
		},
	}
	m := newFakeManager(deploy)
	err := m.PatchMemoryLimit("default", "web", "web", "256Mi")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// --- RollbackDeployment Tests ---

func TestRollbackDeployment_Success(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "web", Image: "nginx"},
					},
				},
			},
		},
	}
	m := newFakeManager(deploy)
	err := m.RollbackDeployment("default", "web")
	if err != nil {
		t.Fatalf("expected no error from rollback, got: %v", err)
	}

	// Verify annotation was set
	updated, err := m.KubeClient.AppsV1().Deployments("default").Get(context.Background(), "web", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected to get deployment, got: %v", err)
	}
	if updated.Spec.Template.Annotations == nil {
		t.Fatal("expected annotation to be set on pod template")
	}
	if _, ok := updated.Spec.Template.Annotations["kubesolv.io/rollbackTriggeredAt"]; !ok {
		t.Error("expected rollback annotation to be present")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
