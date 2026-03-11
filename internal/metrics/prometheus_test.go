package metrics

import (
	"testing"
)

// TestNewPrometheusClient_InvalidURL verifies that an invalid URL doesn't panic
// and returns a valid client (the Prometheus API client validates lazily).
func TestNewPrometheusClient_ValidURL(t *testing.T) {
	client, err := NewPrometheusClient("http://localhost:9090")
	if err != nil {
		t.Fatalf("expected no error for valid URL, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewPrometheusClient_EmptyURL(t *testing.T) {
	// Empty URL is valid for the Prometheus client API (it validates lazily on query)
	client, err := NewPrometheusClient("")
	if err != nil {
		t.Fatalf("expected no error for empty URL, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewPrometheusClient_CustomURL(t *testing.T) {
	client, err := NewPrometheusClient("http://prometheus.monitoring.svc:9090")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
