package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type PrometheusClient struct {
	api v1.API
}

func NewPrometheusClient(url string) (*PrometheusClient, error) {
	client, err := api.NewClient(api.Config{
		Address: url,
	})
	if err != nil {
		return nil, err
	}
	return &PrometheusClient{api: v1.NewAPI(client)}, nil
}

func (p *PrometheusClient) GetCPUUsage(ctx context.Context, namespace, pod string) (float64, error) {
	query := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod="%s", container!="", container!="POD"}[1m]))`, namespace, pod)
	result, _, err := p.api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}
	vec := result.(model.Vector)
	if len(vec) == 0 {
		return 0, nil
	}
	return float64(vec[0].Value), nil
}

func (p *PrometheusClient) GetMemoryUsage(ctx context.Context, namespace, pod string) (float64, error) {
	query := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s", pod="%s", container!="", container!="POD"}) / 1024 / 1024`, namespace, pod)
	result, _, err := p.api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}
	vec := result.(model.Vector)
	if len(vec) == 0 {
		return 0, nil
	}
	return float64(vec[0].Value), nil
}
