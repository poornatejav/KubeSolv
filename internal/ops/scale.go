// internal/ops/scale.go
package ops

import (
	"context"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

func (m *OpsManager) ScaleDeployment(ctx context.Context, namespace, name string, replicas string) error {
	rep64, err := strconv.ParseInt(replicas, 10, 32)
	if err != nil {
		return err
	}
	rep32 := int32(rep64)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := m.KubeClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Spec.Replicas = &rep32
		_, err = m.KubeClient.AppsV1().Deployments(namespace).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
}
