package ops

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// RestartDeployment triggers a rollout restart by patching the restartedAt annotation.
// This is NOT a rollback — it re-creates pods with the current spec.
func (m *OpsManager) RestartDeployment(ctx context.Context, namespace, deployName string) error {
	patch := fmt.Appendf(nil, `{"spec": {"template": {"metadata": {"annotations": {"kubesolv.io/restartedAt": "%s"}}}}}`, time.Now().Format(time.RFC3339))
	_, err := m.KubeClient.AppsV1().Deployments(namespace).Patch(ctx, deployName, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
}
