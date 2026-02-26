package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
)

type ChatIntent struct {
	Action    string `json:"action"`
	Target    string `json:"target"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
	Reply     string `json:"reply"`
}

func (c *Client) ProcessCommand(ctx context.Context, chatID string, userInput string) (*ChatIntent, error) {
	session := c.GetOrCreateSession(chatID)

	prompt := fmt.Sprintf(`You are KubeSolv AI, an expert SRE conversational agent.
User said: "%s"

Analyze the user's intent based on the current conversation history.
Return strictly JSON with keys: 
- action (scale, logs, status, metrics, events, chat)
- target (resource name if applicable, or empty)
- namespace (use default if none specified)
- value (number if scaling, or empty)
- reply (State clearly that the action has been successfully executed right now. Do not say you 'will' do it.). Do not include markdown formatting in the JSON.`, userInput)

	resp, err := session.SendMessage(ctx, genai.Text(prompt))
	if err != nil {
		return nil, fmt.Errorf("API error: %v", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from API")
	}

	part, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
	if !ok {
		return nil, fmt.Errorf("invalid response format")
	}

	rawJSON := string(part)
	rawJSON = strings.TrimSpace(rawJSON)
	rawJSON = strings.TrimPrefix(rawJSON, "```json")
	rawJSON = strings.TrimPrefix(rawJSON, "```")
	rawJSON = strings.TrimSuffix(rawJSON, "```")
	rawJSON = strings.TrimSpace(rawJSON)

	var intent ChatIntent
	err = json.Unmarshal([]byte(rawJSON), &intent)
	if err != nil {
		return nil, fmt.Errorf("JSON parse error: %v | Raw Output: %s", err, rawJSON)
	}

	if intent.Namespace == "" {
		intent.Namespace = "default"
	}

	return &intent, nil
}

type CostDecision struct {
	Optimize            bool   `json:"optimize"`
	RecommendedReplicas int32  `json:"recommended_replicas"`
	Reason              string `json:"reason"`
}

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

	session := c.GetOrCreateSession("cost-optimizer")
	resp, err := session.SendMessage(ctx, genai.Text(prompt))
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
