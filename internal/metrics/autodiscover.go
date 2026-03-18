package metrics

import (
	"context"
	"os"

	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var autoLog = ctrl.Log.WithName("prometheus-autodiscover")

// AutoDiscoverPrometheus finds the Prometheus URL from env or by scanning known service locations.
func AutoDiscoverPrometheus(ctx context.Context, clientset kubernetes.Interface) string {
	if url := os.Getenv("PROMETHEUS_URL"); url != "" {
		autoLog.Info("Using PROMETHEUS_URL from environment", "url", url)
		return url
	}

	candidates := []struct {
		namespace string
		name      string
	}{
		{"monitoring", "prometheus-server"},
		{"monitoring", "kube-prometheus-stack-prometheus"},
		{"monitoring", "prometheus-operated"},
		{"prometheus", "prometheus-server"},
		{"observability", "prometheus"},
		{"default", "prometheus"},
	}

	for _, c := range candidates {
		_, err := clientset.CoreV1().Services(c.namespace).Get(ctx, c.name, metav1.GetOptions{})
		if err == nil {
			url := "http://" + c.name + "." + c.namespace + ".svc.cluster.local:9090"
			autoLog.Info("Auto-discovered Prometheus", "url", url)
			return url
		}
	}

	autoLog.Info("Prometheus not found. Cost optimization and CPU-based scaling disabled.")
	return ""
}
