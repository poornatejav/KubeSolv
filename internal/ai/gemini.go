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

	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text("You are KubeSolv AI, an expert Kubernetes SRE conversational agent. You have comprehensive DevOps capabilities to manage the cluster. You can get health reports, resource usage, events, read logs, scale and restart deployments, get pod details, delete pods, and cordon/uncordon nodes. Use these tools when requested by the user, or if you need to gather context to answer their question. Be direct, concise, and helpful. Format your responses elegantly with markdown."),
		},
	}

	model.Tools = []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "get_health_report",
					Description: "Gets the health report of the cluster showing the number of running, failed, and pending pods.",
				},
				{
					Name:        "get_resource_usage",
					Description: "Gets the CPU and Memory resource usage for the top pods in the cluster.",
				},
				{
					Name:        "get_recent_events",
					Description: "Gets the recent events/warnings from the cluster.",
				},
				{
					Name:        "get_pod_logs",
					Description: "Gets the recent logs for a specific pod.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"pod_name": {
								Type:        genai.TypeString,
								Description: "The name or partial name of the pod to fetch logs for.",
							},
						},
						Required: []string{"pod_name"},
					},
				},
				{
					Name:        "scale_deployment",
					Description: "Scales a deployment to the specified number of replicas.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"namespace": {
								Type:        genai.TypeString,
								Description: "The namespace of the deployment.",
							},
							"deployment_name": {
								Type:        genai.TypeString,
								Description: "The name of the deployment.",
							},
							"replicas": {
								Type:        genai.TypeString,
								Description: "The number of replicas to scale to (as a string).",
							},
						},
						Required: []string{"namespace", "deployment_name", "replicas"},
					},
				},
				{
					Name:        "get_pod_details",
					Description: "Gets detailed information about a pod including its status, IP, node, and condition. Use this before trying to delete a pod or read logs to understand its context.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"namespace": {
								Type:        genai.TypeString,
								Description: "The namespace of the pod.",
							},
							"pod_name": {
								Type:        genai.TypeString,
								Description: "The exact name of the pod.",
							},
						},
						Required: []string{"namespace", "pod_name"},
					},
				},
				{
					Name:        "delete_pod",
					Description: "Force deletes a pod to cause it to reschedule or restart. Useful if a pod is stuck.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"namespace": {
								Type:        genai.TypeString,
								Description: "The namespace of the pod.",
							},
							"pod_name": {
								Type:        genai.TypeString,
								Description: "The exact name of the pod to delete.",
							},
						},
						Required: []string{"namespace", "pod_name"},
					},
				},
				{
					Name:        "restart_deployment",
					Description: "Triggers a rollout restart of a deployment.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"namespace": {
								Type:        genai.TypeString,
								Description: "The namespace of the deployment.",
							},
							"deployment_name": {
								Type:        genai.TypeString,
								Description: "The exact name of the deployment to restart.",
							},
						},
						Required: []string{"namespace", "deployment_name"},
					},
				},
				{
					Name:        "list_pods",
					Description: "Lists all pods in a specified namespace. Use this when you need to find a pod name.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"namespace": {
								Type:        genai.TypeString,
								Description: "The namespace to list pods for.",
							},
						},
						Required: []string{"namespace"},
					},
				},
				{
					Name:        "cordon_node",
					Description: "Marks a node as unschedulable (cordons it).",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"node_name": {
								Type:        genai.TypeString,
								Description: "The exact name of the node to cordon.",
							},
						},
						Required: []string{"node_name"},
					},
				},
				{
					Name:        "uncordon_node",
					Description: "Marks a node as schedulable (uncordons it).",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"node_name": {
								Type:        genai.TypeString,
								Description: "The exact name of the node to uncordon.",
							},
						},
						Required: []string{"node_name"},
					},
				},
			},
		},
	}

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
