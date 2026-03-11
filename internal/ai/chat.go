package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
)

func (c *Client) SendChatMessage(ctx context.Context, chatID string, userInput string) (*genai.GenerateContentResponse, error) {
	session := c.GetOrCreateSession(chatID)
	return session.SendMessage(ctx, genai.Text(userInput))
}

func (c *Client) SendFunctionResponse(ctx context.Context, chatID string, responses []*genai.FunctionResponse) (*genai.GenerateContentResponse, error) {
	session := c.GetOrCreateSession(chatID)
	parts := make([]genai.Part, 0, len(responses))
	for _, r := range responses {
		parts = append(parts, r)
	}
	return session.SendMessage(ctx, parts...)
}

type CostDecision struct {
	Optimize            bool   `json:"optimize"`
	RecommendedReplicas int32  `json:"recommended_replicas"`
	Reason              string `json:"reason"`
}

// AnalyzeCostOptimization uses a stateless (non-session) call to prevent context bleed
// between different deployment analyses.
func (c *Client) AnalyzeCostOptimization(ctx context.Context, namespace, deployment string, currentReplicas int32, cpu float64, mem float64) (*CostDecision, error) {
	prompt := fmt.Sprintf(`You are KubeSolv AI, a Kubernetes SRE specializing in cost optimization.
Analyze this deployment:
Deployment: %s
Namespace: %s
Current Replicas: %d
CPU Usage: %.2f cores
Memory Usage: %.2f MB

Determine if this deployment is significantly over-provisioned. If so, recommend a lower replica count.
Return strictly JSON with keys:
- optimize (boolean: true if downscaling is recommended, false otherwise)
- recommended_replicas (integer: >= 1, the ideal scale count based on usage)
- reason (string: a short, professional explanation of why this specific number is recommended). Do not include markdown formatting.`, deployment, namespace, currentReplicas, cpu, mem)

	// Use stateless generation — no shared session, no context bleed between deployments
	resp, err := c.GenerateStateless(ctx, prompt)
	if err != nil {
		return nil, err
	}

	part, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
	if !ok {
		return nil, fmt.Errorf("invalid response")
	}

	rawJSON := string(part)
	rawJSON = strings.TrimSpace(rawJSON)
	rawJSON = strings.TrimPrefix(rawJSON, "```json")
	rawJSON = strings.TrimPrefix(rawJSON, "```")
	rawJSON = strings.TrimSuffix(rawJSON, "```")
	rawJSON = strings.TrimSpace(rawJSON)

	var decision CostDecision
	err = json.Unmarshal([]byte(rawJSON), &decision)
	if err != nil {
		return nil, err
	}

	return &decision, nil
}
