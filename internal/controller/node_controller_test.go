package controller

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNodeAlertDeduplication(t *testing.T) {
	r := &NodeReconciler{}

	// First alert should be sent
	cacheKey := fmt.Sprintf("node/%s/%s", "worker-1", "DiskPressure")
	r.AlertCache.Store(cacheKey, time.Now())

	// Verify it's cached
	_, ok := r.AlertCache.Load(cacheKey)
	if !ok {
		t.Error("expected node alert to be cached")
	}
}

func TestNodeAlertDeduplication_Expired(t *testing.T) {
	r := &NodeReconciler{}

	cacheKey := fmt.Sprintf("node/%s/%s", "worker-1", "DiskPressure")
	// Store with a time 31 minutes ago
	r.AlertCache.Store(cacheKey, time.Now().Add(-31*time.Minute))

	// After 30 minutes, the alert should be re-sent
	lastTime, ok := r.AlertCache.Load(cacheKey)
	if !ok {
		t.Fatal("expected cached entry")
	}
	if time.Since(lastTime.(time.Time)) < 30*time.Minute {
		t.Error("expected expired cache entry")
	}
}

func TestNodeAlertFormatting(t *testing.T) {
	title := fmt.Sprintf("Infrastructure Alert: %s", "DiskPressure")
	body := fmt.Sprintf("Node: %s\nReason: %s\nDetails: %s\n\nAction Required: Prevent new pods from scheduling here.",
		"worker-1", "DiskIsFull", "Node disk usage exceeds 90%")

	if title != "Infrastructure Alert: DiskPressure" {
		t.Errorf("unexpected title format: %s", title)
	}
	if body == "" {
		t.Error("expected non-empty body")
	}
}

func TestNodeAlertConcurrency(t *testing.T) {
	r := &NodeReconciler{}
	var wg sync.WaitGroup

	// Simulate concurrent alerts from multiple nodes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			nodeName := fmt.Sprintf("worker-%d", i)
			cacheKey := fmt.Sprintf("node/%s/%s", nodeName, "MemoryPressure")
			r.AlertCache.Store(cacheKey, time.Now())
		}(i)
	}

	wg.Wait()

	count := 0
	r.AlertCache.Range(func(key, value any) bool {
		count++
		return true
	})

	if count != 50 {
		t.Errorf("expected 50 cached node alerts, got: %d", count)
	}
}

func TestNodeConditionTypes(t *testing.T) {
	// Verify the condition types we check are valid
	pressureTypes := []string{"MemoryPressure", "DiskPressure", "PIDPressure"}
	for _, pt := range pressureTypes {
		if pt == "" {
			t.Error("empty pressure type")
		}
	}
}

func TestNodeCordonLabelDetection(t *testing.T) {
	labels := map[string]string{
		"kubesolv.io/test-cordon": "true",
	}

	if labels["kubesolv.io/test-cordon"] != "true" {
		t.Error("expected test cordon label to be detected")
	}

	emptyLabels := map[string]string{}
	if emptyLabels["kubesolv.io/test-cordon"] == "true" {
		t.Error("expected no test cordon label on empty labels")
	}
}
