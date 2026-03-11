package ops

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRestartDeployment_Success(t *testing.T) {
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
	err := m.RestartDeployment(context.Background(), "default", "web")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	updated, err := m.KubeClient.AppsV1().Deployments("default").Get(context.Background(), "web", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected to get deployment, got: %v", err)
	}

	if updated.Spec.Template.Annotations == nil {
		t.Fatal("expected restart annotation to be set")
	}
	if _, ok := updated.Spec.Template.Annotations["kubesolv.io/restartedAt"]; !ok {
		t.Error("expected kubesolv.io/restartedAt annotation to be present")
	}
}

func TestRestartDeployment_NotFound(t *testing.T) {
	m := newFakeManager()
	err := m.RestartDeployment(context.Background(), "default", "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent deployment")
	}
}
