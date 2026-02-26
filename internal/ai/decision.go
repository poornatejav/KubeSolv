package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
)

type Decision struct {
	ShouldAutoFix bool   `json:"should_auto_fix"`
	Action        string `json:"action"`     // e.g., "rollback", "patch_memory", "cordon"
	Confidence    int    `json:"confidence"` // 0-100
	Reason        string `json:"reason"`
	Parameters    string `json:"parameters"` // e.g., "128Mi" or "node-01"
}

func (c *Client) EvaluateIncident(ctx context.Context, incidentType, resourceName, namespace, details, logs string) (*Decision, error) {
	model := c.genModel
	model.ResponseMIMEType = "application/json"

	prompt := fmt.Sprintf(`
You are a Senior Site Reliability Engineer (SRE).
A critical incident has occurred in the cluster.
Incident: %s
Resource: %s (Namespace: %s)
Details: %s
Logs:
%s

Analyze the situation.
Decide if you should AUTOMATICALLY fix this without human intervention.
Auto-fix ONLY if:
1. The error is obvious (e.g., OOMKilled = increase memory).
2. The fix is safe (e.g., Rollback on repeated crash).
3. You are >90%% confident.

CRITICAL RULE FOR ROLLBACKS:
If a pod is in CrashLoopBackOff due to a code-level error (like a Segmentation fault, NullPointerException, or panic), the SAFEST immediate action is to rollback the deployment to the last working state to restore service. In these cases, set action to "rollback", should_auto_fix to true, and confidence to >90.

Return JSON:
{
  "should_auto_fix": true/false,
  "action": "rollback" OR "patch_memory" OR "cordon" OR "restart" OR "none",
  "confidence": <0-100>,
  "reason": "Short explanation for the human log",
  "parameters": "value needed for the action (e.g. '256Mi' for memory, or empty)"
}
`, incidentType, resourceName, namespace, details, logs)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return nil, err
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	part, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
	if !ok {
		return nil, fmt.Errorf("invalid format")
	}

	rawJSON := string(part)
	// Cleanup markdown code blocks if present
	rawJSON = strings.TrimPrefix(strings.TrimSpace(rawJSON), "```json")
	rawJSON = strings.TrimSuffix(strings.TrimSpace(rawJSON), "```")

	var decision Decision
	if err := json.Unmarshal([]byte(rawJSON), &decision); err != nil {
		return nil, err
	}

	return &decision, nil
}
