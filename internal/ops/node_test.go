package ops

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCordonNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: false},
	}

	m := newFakeManager(node)
	err := m.CordonNode(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := m.KubeClient.CoreV1().Nodes().Get(context.Background(), "worker-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected to get node, got: %v", err)
	}
	if !updated.Spec.Unschedulable {
		t.Error("expected node to be unschedulable after cordoning")
	}
}

func TestCordonNode_NotFound(t *testing.T) {
	m := newFakeManager()
	err := m.CordonNode(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent node")
	}
}

func TestUncordonNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	}

	m := newFakeManager(node)
	err := m.UncordonNode(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := m.KubeClient.CoreV1().Nodes().Get(context.Background(), "worker-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected to get node, got: %v", err)
	}
	if updated.Spec.Unschedulable {
		t.Error("expected node to be schedulable after uncordoning")
	}
}

func TestUncordonNode_NotFound(t *testing.T) {
	m := newFakeManager()
	err := m.UncordonNode(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent node")
	}
}
