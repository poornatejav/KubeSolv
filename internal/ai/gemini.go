// package ai

// import (
// 	"context"
// 	"fmt"
// 	"github.com/google/generative-ai-go/genai"
// 	"google.golang.org/api/option"
// )

// type Client struct {
// 	genModel *genai.GenerativeModel
// }

// func NewClient(apiKey string) (*Client, error) {
// 	ctx := context.Background()
// 	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
// 	if err != nil {
// 		return nil, err
// 	}

// 	// Use Flash for speed and low cost
// 	model := client.GenerativeModel("gemini-2.5-flash")
// 	return &Client{genModel: model}, nil
// }

// func (c *Client) AnalyzeError(ctx context.Context, podName, namespace, errorReason string, logs string) (string, error) {
// 	prompt := fmt.Sprintf(`
// You are a Senior Kubernetes SRE. A pod named '%s' in namespace '%s' has crashed.
// Reason: %s
// Recent Logs:
// %s

// Analyze this. Return a SHORT, one-sentence root cause and a suggested fix.
// Format: "ROOT CAUSE: <cause>. FIX: <fix>."
// `, podName, namespace, errorReason, logs)

// 	resp, err := c.genModel.GenerateContent(ctx, genai.Text(prompt))
// 	if err != nil {
// 		return "", err
// 	}

// 	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
// 		return fmt.Sprintf("%s", resp.Candidates[0].Content.Parts[0]), nil
// 	}
// 	return "Could not generate analysis.", nil
// }

// -----------------------------

// internal/ai/gemini.go
package ai

import (
	"context"
	"sync"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type Client struct {
	APIKey   string
	genModel *genai.GenerativeModel
	aiClient *genai.Client
}

var (
	chatSessions = make(map[string]*genai.ChatSession)
	sessionMutex sync.Mutex
)

func NewClient(apiKey string) (*Client, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}

	model := client.GenerativeModel("gemini-2.5-flash")
	return &Client{
		APIKey:   apiKey,
		genModel: model,
		aiClient: client,
	}, nil
}

// GetOrCreateSession manages conversation history per chat ID
func (c *Client) GetOrCreateSession(chatID string) *genai.ChatSession {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()

	if session, exists := chatSessions[chatID]; exists {
		return session
	}

	session := c.genModel.StartChat()
	chatSessions[chatID] = session
	return session
}
