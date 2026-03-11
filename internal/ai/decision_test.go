package ai

import (
	"encoding/json"
	"testing"
)

func TestDecisionJSON_Valid(t *testing.T) {
	raw := `{
		"should_auto_fix": true,
		"action": "rollback",
		"confidence": 95,
		"reason": "Pod is in CrashLoopBackOff due to segfault",
		"parameters": ""
	}`

	var d Decision
	err := json.Unmarshal([]byte(raw), &d)
	if err != nil {
		t.Fatalf("expected valid JSON parse, got: %v", err)
	}
	if !d.ShouldAutoFix {
		t.Error("expected ShouldAutoFix to be true")
	}
	if d.Action != "rollback" {
		t.Errorf("expected action 'rollback', got: %s", d.Action)
	}
	if d.Confidence != 95 {
		t.Errorf("expected confidence 95, got: %d", d.Confidence)
	}
}

func TestDecisionJSON_NoAutoFix(t *testing.T) {
	raw := `{
		"should_auto_fix": false,
		"action": "none",
		"confidence": 30,
		"reason": "Unclear root cause",
		"parameters": ""
	}`

	var d Decision
	err := json.Unmarshal([]byte(raw), &d)
	if err != nil {
		t.Fatalf("expected valid JSON parse, got: %v", err)
	}
	if d.ShouldAutoFix {
		t.Error("expected ShouldAutoFix to be false")
	}
	if d.Action != "none" {
		t.Errorf("expected action 'none', got: %s", d.Action)
	}
}

func TestDecisionJSON_InvalidJSON(t *testing.T) {
	raw := `not json at all`

	var d Decision
	err := json.Unmarshal([]byte(raw), &d)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecisionJSON_AllActions(t *testing.T) {
	actions := []string{"rollback", "patch_memory", "cordon", "restart", "none"}
	for _, action := range actions {
		raw := `{"should_auto_fix": false, "action": "` + action + `", "confidence": 50, "reason": "test", "parameters": ""}`
		var d Decision
		err := json.Unmarshal([]byte(raw), &d)
		if err != nil {
			t.Fatalf("failed to parse action '%s': %v", action, err)
		}
		if d.Action != action {
			t.Errorf("expected action '%s', got: '%s'", action, d.Action)
		}
	}
}

func TestCostDecisionJSON_Valid(t *testing.T) {
	raw := `{
		"optimize": true,
		"recommended_replicas": 2,
		"reason": "CPU usage is below 10%, safe to reduce from 5 to 2 replicas"
	}`

	var d CostDecision
	err := json.Unmarshal([]byte(raw), &d)
	if err != nil {
		t.Fatalf("expected valid JSON parse, got: %v", err)
	}
	if !d.Optimize {
		t.Error("expected Optimize to be true")
	}
	if d.RecommendedReplicas != 2 {
		t.Errorf("expected 2 recommended replicas, got: %d", d.RecommendedReplicas)
	}
}

func TestCostDecisionJSON_NoOptimize(t *testing.T) {
	raw := `{
		"optimize": false,
		"recommended_replicas": 5,
		"reason": "Current utilization is optimal"
	}`

	var d CostDecision
	err := json.Unmarshal([]byte(raw), &d)
	if err != nil {
		t.Fatalf("expected valid JSON parse, got: %v", err)
	}
	if d.Optimize {
		t.Error("expected Optimize to be false")
	}
}

func TestCostDecisionJSON_InvalidJSON(t *testing.T) {
	raw := `{broken`

	var d CostDecision
	err := json.Unmarshal([]byte(raw), &d)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecisionJSON_WithMarkdownWrapper(t *testing.T) {
	// Simulate what AI sometimes returns - JSON wrapped in markdown code blocks
	raw := "```json\n" + `{
		"should_auto_fix": true,
		"action": "rollback",
		"confidence": 92,
		"reason": "CrashLoopBackOff with segfault",
		"parameters": ""
	}` + "\n```"

	// Strip markdown (same logic used in EvaluateIncident)
	cleaned := raw
	if len(cleaned) > 7 && cleaned[:7] == "```json" {
		cleaned = cleaned[7:]
	}
	if len(cleaned) > 3 && cleaned[len(cleaned)-3:] == "```" {
		cleaned = cleaned[:len(cleaned)-3]
	}

	var d Decision
	err := json.Unmarshal([]byte(cleaned), &d)
	if err != nil {
		t.Fatalf("expected valid JSON after stripping markdown, got: %v", err)
	}
	if d.Confidence != 92 {
		t.Errorf("expected confidence 92, got: %d", d.Confidence)
	}
}
