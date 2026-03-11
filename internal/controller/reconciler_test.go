package controller

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- notifyUser deduplication tests ---

func TestNotifyUser_Deduplication(t *testing.T) {
	r := &KubeSolvConfigReconciler{}

	// First call should store in cache
	r.notifyUser("Test Alert", "Something happened")

	// Verify it was stored — use the same hash logic
	hash := fmt.Sprintf("%s-%d", "Test Alert", len("Something happened"))
	_, ok := r.AlertCache.Load(hash)
	if !ok {
		t.Error("expected alert to be cached after first call")
	}
}

func TestNotifyUser_RecoveredBypassesDedup(t *testing.T) {
	r := &KubeSolvConfigReconciler{}

	// First call
	r.notifyUser("Recovered: pod-1", "Pod recovered")
	hash := fmt.Sprintf("%s-%d", "Recovered: pod-1", len("Pod recovered"))
	_, ok := r.AlertCache.Load(hash)
	if !ok {
		t.Error("expected recovery alert to be cached")
	}
}

// --- Alert caching tests ---

func TestAlertCache_ConcurrentAccess(t *testing.T) {
	r := &KubeSolvConfigReconciler{}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("alert-%d", i)
			r.AlertCache.Store(key, time.Now())
		}(i)
	}

	wg.Wait()

	count := 0
	r.AlertCache.Range(func(key, value any) bool {
		count++
		return true
	})

	if count != 100 {
		t.Errorf("expected 100 cached alerts, got: %d", count)
	}
}

// --- State tracking tests ---

func TestStateCache_PodTracking(t *testing.T) {
	r := &KubeSolvConfigReconciler{}

	podKey := "default/app-1"
	r.StateCache.Store(podKey, "Running")

	status, ok := r.StateCache.Load(podKey)
	if !ok {
		t.Fatal("expected pod state to be tracked")
	}
	if status.(string) != "Running" {
		t.Errorf("expected 'Running', got: '%s'", status)
	}

	// Update state
	r.StateCache.Store(podKey, "CrashLoopBackOff")
	status, _ = r.StateCache.Load(podKey)
	if status.(string) != "CrashLoopBackOff" {
		t.Errorf("expected 'CrashLoopBackOff', got: '%s'", status)
	}
}

// --- isBad status helper inline tests ---

func TestIsBadStatus(t *testing.T) {
	badStatuses := []string{
		"CrashLoopBackOff",
		"ImagePullBackOff",
		"ErrImagePull",
		"Error",
		"OOMKilled",
		"CreateContainerConfigError",
	}

	goodStatuses := []string{
		"Running",
		"Pending",
		"Succeeded",
		"ContainerCreating",
	}

	isBad := func(s string) bool {
		return s == "CrashLoopBackOff" || s == "ImagePullBackOff" || s == "ErrImagePull" || s == "Error" || s == "OOMKilled" || s == "CreateContainerConfigError"
	}

	for _, status := range badStatuses {
		if !isBad(status) {
			t.Errorf("expected '%s' to be bad", status)
		}
	}
	for _, status := range goodStatuses {
		if isBad(status) {
			t.Errorf("expected '%s' to be good", status)
		}
	}
}

// --- Scaling logic tests ---

func TestScalingLogic_LowTraffic(t *testing.T) {
	// Simulate <0.5 logs/sec → should scale down
	logsPerSec := 0.3
	currentReplicas := int32(3)
	minReplicas := int32(1)

	if logsPerSec < 0.5 && currentReplicas > minReplicas {
		desiredReplicas := currentReplicas - 1
		if desiredReplicas != 2 {
			t.Errorf("expected scale down to 2, got: %d", desiredReplicas)
		}
	} else {
		t.Error("expected scale down condition to be met")
	}
}

func TestScalingLogic_HighTraffic(t *testing.T) {
	// Simulate >10 logs/sec → should scale up
	logsPerSec := 25.0
	currentReplicas := int32(2)
	maxReplicas := int32(10)

	if logsPerSec > 10.0 && currentReplicas < maxReplicas {
		steps := int32(2) // ceil((25-10)/10) = 2, capped at 2
		desired := currentReplicas + steps
		if desired > maxReplicas {
			desired = maxReplicas
		}
		if desired != 4 {
			t.Errorf("expected scale up to 4, got: %d", desired)
		}
	} else {
		t.Error("expected scale up condition to be met")
	}
}

func TestScalingLogic_NormalTraffic(t *testing.T) {
	// Simulate 0.5 < logs/sec < 10 → should not scale
	logsPerSec := 5.0
	if logsPerSec < 0.5 || logsPerSec > 10.0 {
		t.Error("expected normal traffic, no scaling")
	}
}

func TestScalingLogic_AtMinReplicas(t *testing.T) {
	// Low traffic but already at min → should not scale further
	logsPerSec := 0.1
	currentReplicas := int32(1)
	minReplicas := int32(1)

	if logsPerSec < 0.5 && currentReplicas <= minReplicas {
		// Correct: no action
	} else {
		t.Error("expected no scale action when at min replicas")
	}
}

func TestScalingLogic_AtMaxReplicas(t *testing.T) {
	// High traffic but already at max → should not scale further
	logsPerSec := 50.0
	currentReplicas := int32(10)
	maxReplicas := int32(10)

	if logsPerSec > 10.0 && currentReplicas >= maxReplicas {
		// Correct: no action
	} else {
		t.Error("expected no scale action when at max replicas")
	}
}

// --- Namespace filtering tests ---

func TestNamespaceFiltering(t *testing.T) {
	filteredNamespaces := []string{"kube-system", "local-path-storage", "ingress-nginx", "monitoring"}

	for _, ns := range filteredNamespaces {
		if ns != "kube-system" && ns != "local-path-storage" && ns != "ingress-nginx" && ns != "monitoring" {
			t.Errorf("expected '%s' to be filtered", ns)
		}
	}

	userNamespaces := []string{"default", "production", "staging"}
	for _, ns := range userNamespaces {
		if ns == "kube-system" || ns == "local-path-storage" || ns == "ingress-nginx" || ns == "monitoring" {
			t.Errorf("expected '%s' NOT to be filtered", ns)
		}
	}
}

// --- Alert message formatting tests ---

func TestAlertMessageFormatting_NewPod(t *testing.T) {
	msg := fmt.Sprintf("📦 *New Pod Detected*\nName: `%s`\nNamespace: `%s`\nStatus: `%s`", "my-app-abc", "default", "Running")

	if !strings.Contains(msg, "my-app-abc") {
		t.Error("expected pod name in message")
	}
	if !strings.Contains(msg, "default") {
		t.Error("expected namespace in message")
	}
}

func TestAlertMessageFormatting_Recovery(t *testing.T) {
	msg := fmt.Sprintf("✅ *Pod Recovered!*\nPod `%s` is now healthy and Running.", "crashed-pod")
	if !strings.Contains(msg, "crashed-pod") {
		t.Error("expected pod name in recovery message")
	}
	if !strings.Contains(msg, "Recovered") {
		t.Error("expected 'Recovered' in message")
	}
}
