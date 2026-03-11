package alert

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"kubesolv/internal/ai"
	"kubesolv/internal/ops"

	"github.com/google/generative-ai-go/genai"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const approvalMsg = "Approval required. An interactive approval prompt has been sent."

type SlackBot struct {
	Client    *slack.Client
	Socket    *socketmode.Client
	Ops       *ops.OpsManager
	AI        *ai.Client
	ChannelID string
}

func NewSlackBot(appToken, botToken, channelID string, opsManager *ops.OpsManager, aiClient *ai.Client) *SlackBot {
	if appToken == "" || botToken == "" {
		return nil
	}

	api := slack.New(botToken, slack.OptionAppLevelToken(appToken), slack.OptionLog(log.New(os.Stdout, "slack: ", log.Lshortfile|log.LstdFlags)))
	socket := socketmode.New(api)

	return &SlackBot{
		Client:    api,
		Socket:    socket,
		Ops:       opsManager,
		AI:        aiClient,
		ChannelID: channelID,
	}
}

func (s *SlackBot) Start() {
	go func() {
		for evt := range s.Socket.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				s.Socket.Ack(*evt.Request)

				if eventsAPIEvent.Type == slackevents.CallbackEvent {
					innerEvent := eventsAPIEvent.InnerEvent
					switch ev := innerEvent.Data.(type) {
					case *slackevents.MessageEvent:
						if ev.BotID != "" || ev.ChannelType != "im" {
							continue
						}
						s.routeMessage(ev.Channel, ev.Text)
					case *slackevents.AppMentionEvent:
						s.routeMessage(ev.Channel, ev.Text)
					}
				}
			case socketmode.EventTypeInteractive:
				s.Socket.Ack(*evt.Request)
				interaction, ok := evt.Data.(slack.InteractionCallback)
				if !ok {
					continue
				}
				if len(interaction.ActionCallback.BlockActions) > 0 {
					action := interaction.ActionCallback.BlockActions[0]
					s.handleInteraction(action.ActionID, interaction.Channel.ID, interaction.Message.Timestamp)
				}
			}
		}
	}()
	_ = s.Socket.Run()
}

func cleanSlackMessage(msg string) string {
	text := regexp.MustCompile(`<@[^>]+>`).ReplaceAllString(msg, "")
	return strings.TrimSpace(text)
}

func (s *SlackBot) routeMessage(channelID, msg string) {
	cleanMsg := cleanSlackMessage(msg)

	ctx := context.Background()
	resp, err := s.AI.SendChatMessage(ctx, channelID, cleanMsg)

	for {
		if err != nil {
			s.send(channelID, fmt.Sprintf("⚠️ AI Error: %v", err))
			return
		}

		if len(resp.Candidates) == 0 {
			s.send(channelID, "⚠️ AI returned an empty response.")
			return
		}

		cand := resp.Candidates[0]
		var replyText string
		var toolCalls []genai.FunctionCall

		for _, part := range cand.Content.Parts {
			switch p := part.(type) {
			case genai.Text:
				replyText += string(p)
			case genai.FunctionCall:
				toolCalls = append(toolCalls, p)
			}
		}

		if replyText != "" {
			s.send(channelID, replyText)
		}

		if len(toolCalls) == 0 {
			break
		}

		var toolResponses []*genai.FunctionResponse
		for _, call := range toolCalls {
			result := s.executeTool(ctx, channelID, call)
			toolResponses = append(toolResponses, &genai.FunctionResponse{
				Name: call.Name,
				Response: map[string]any{
					"result": result,
				},
			})
		}

		resp, err = s.AI.SendFunctionResponse(ctx, channelID, toolResponses)
	}
}

func (s *SlackBot) executeTool(ctx context.Context, channelID string, call genai.FunctionCall) any {
	const approvalMsg = approvalMsg
	switch call.Name {
	case "get_health_report":
		return s.Ops.GetHealthReport()
	case "get_resource_usage":
		return s.Ops.GetResourceUsage()
	case "get_recent_events":
		return s.Ops.GetRecentEvents()
	case "get_pod_logs":
		podName, _ := call.Args["pod_name"].(string)
		return s.Ops.GetLogs(podName)
	case "scale_deployment":
		namespace, _ := call.Args["namespace"].(string)
		deployName, _ := call.Args["deployment_name"].(string)
		replicas, _ := call.Args["replicas"].(string)
		actionID := fmt.Sprintf("scale|%s|%s|%s", namespace, deployName, replicas)
		_ = s.sendWithAction(channelID, "Action Required: Scale Deployment", fmt.Sprintf("AI proposes scaling `%s` in `%s` to `%s` replicas.", deployName, namespace, replicas), actionID, "Approve Scale")
		return approvalMsg
	case "list_pods":
		namespace, _ := call.Args["namespace"].(string)
		return s.Ops.ListPods(ctx, namespace)
	case "get_pod_details":
		namespace, _ := call.Args["namespace"].(string)
		podName, _ := call.Args["pod_name"].(string)
		return s.Ops.GetPodDetails(ctx, namespace, podName)
	case "delete_pod":
		namespace, _ := call.Args["namespace"].(string)
		podName, _ := call.Args["pod_name"].(string)
		actionID := fmt.Sprintf("del_pod|%s|%s", namespace, podName)
		_ = s.sendWithAction(channelID, "Action Required: Delete Pod", fmt.Sprintf("AI proposes to force-delete pod `%s` in `%s`.", podName, namespace), actionID, "Approve Delete")
		return approvalMsg
	case "restart_deployment":
		namespace, _ := call.Args["namespace"].(string)
		deployName, _ := call.Args["deployment_name"].(string)
		actionID := fmt.Sprintf("restart_dep|%s|%s", namespace, deployName)
		_ = s.sendWithAction(channelID, "Action Required: Restart Deployment", fmt.Sprintf("AI proposes rollout restart for `%s` in `%s`.", deployName, namespace), actionID, "Approve Restart")
		return approvalMsg
	case "cordon_node":
		nodeName, _ := call.Args["node_name"].(string)
		actionID := fmt.Sprintf("cordon|%s", nodeName)
		_ = s.sendWithAction(channelID, "Action Required: Cordon Node", fmt.Sprintf("AI proposes to cordon node `%s`.", nodeName), actionID, "Approve Cordon")
		return approvalMsg
	case "uncordon_node":
		nodeName, _ := call.Args["node_name"].(string)
		actionID := fmt.Sprintf("uncordon|%s", nodeName)
		_ = s.sendWithAction(channelID, "Action Required: Uncordon Node", fmt.Sprintf("AI proposes to uncordon node `%s`.", nodeName), actionID, "Approve Uncordon")
		return approvalMsg
	default:
		return "Unknown tool called"
	}
}

func (s *SlackBot) Broadcast(title, message string) error {
	if s.ChannelID == "" {
		return nil
	}
	_, _, err := s.Client.PostMessage(s.ChannelID, slack.MsgOptionText(fmt.Sprintf("🚨 *%s*\n%s", title, message), false))
	return err
}

func (s *SlackBot) send(channelID string, text string) {
	if len(text) > 2900 {
		text = text[:2900] + "\n...[truncated]"
		if strings.Contains(text, "```") && !strings.HasSuffix(text, "```") {
			text += "\n```"
		}
	}
	_, _, _ = s.Client.PostMessage(channelID, slack.MsgOptionText(text, false))
}

func (s *SlackBot) BroadcastWithAction(title, message, actionID, buttonText string) error {
	if s.ChannelID == "" {
		return nil
	}
	headerText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("🚨 *%s*\n%s", title, message), false, false)
	headerSection := slack.NewSectionBlock(headerText, nil, nil)

	btnText := slack.NewTextBlockObject("plain_text", buttonText, false, false)
	btn := slack.NewButtonBlockElement(actionID, "click", btnText)
	actionBlock := slack.NewActionBlock("action_block", btn)

	_, _, err := s.Client.PostMessage(s.ChannelID, slack.MsgOptionBlocks(headerSection, actionBlock))
	return err
}

func (s *SlackBot) sendWithAction(channelID, title, message, actionID, buttonText string) error {
	headerText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("🚨 *%s*\n%s", title, message), false, false)
	headerSection := slack.NewSectionBlock(headerText, nil, nil)

	btnText := slack.NewTextBlockObject("plain_text", buttonText, false, false)
	btn := slack.NewButtonBlockElement(actionID, "click", btnText)
	actionBlock := slack.NewActionBlock("action_block", btn)

	_, _, err := s.Client.PostMessage(channelID, slack.MsgOptionBlocks(headerSection, actionBlock))
	return err
}

//nolint:unparam
func (s *SlackBot) handleInteraction(actionID string, channelID string, triggerID string) {
	parts := strings.Split(actionID, "|")
	if len(parts) == 5 && parts[0] == "patch_mem" {
		namespace, deployName, containerName, newLimit := parts[1], parts[2], parts[3], parts[4]
		if err := s.Ops.PatchMemoryLimit(namespace, deployName, containerName, newLimit); err == nil {
			msg := fmt.Sprintf("✨ [Approved] Increased memory limit to `%s` for deployment `%s`.", newLimit, deployName)
			_ = s.Broadcast("Action Approved", msg)
			CrossPlatformSync <- "Slack User Approved: Patch Memory for " + deployName
		}
	} else if len(parts) == 3 && parts[0] == "rollback" {
		namespace, deployName := parts[1], parts[2]
		if err := s.Ops.RollbackDeployment(namespace, deployName); err == nil {
			s.send(channelID, fmt.Sprintf("🔄 [Approved] Initiated rollback sequence for deployment `%s`.", deployName))
			CrossPlatformSync <- "Slack User Approved: Rollback for " + deployName
		}
	} else if len(parts) == 2 && parts[0] == "cordon" {
		nodeName := parts[1]
		if err := s.Ops.CordonNode(context.Background(), nodeName); err == nil {
			s.send(channelID, fmt.Sprintf("🔒 [Approved] Node `%s` successfully cordoned (marked unschedulable).", nodeName))
			CrossPlatformSync <- "Slack User Approved: Cordon Node " + nodeName
		} else {
			s.send(channelID, fmt.Sprintf("⚠️ [Error] Could not cordon node `%s`: %v", nodeName, err))
		}
	} else if len(parts) == 2 && parts[0] == "uncordon" {
		nodeName := parts[1]
		if err := s.Ops.UncordonNode(context.Background(), nodeName); err == nil {
			s.send(channelID, fmt.Sprintf("🔓 [Approved] Node `%s` uncordoned and actively scheduling.", nodeName))
			CrossPlatformSync <- "Slack User Approved: Uncordon Node " + nodeName
		} else {
			s.send(channelID, fmt.Sprintf("⚠️ [Error] Could not uncordon node `%s`: %v", nodeName, err))
		}
	} else if len(parts) == 4 && parts[0] == "scale" {
		namespace, deployName, replicas := parts[1], parts[2], parts[3]
		if err := s.Ops.ScaleDeployment(context.Background(), namespace, deployName, replicas); err == nil {
			s.send(channelID, fmt.Sprintf("🚀 [Executed] Scaled deployment `%s` to `%s` replica(s).", deployName, replicas))
			CrossPlatformSync <- "Slack User Approved: Scale " + deployName + " to " + replicas
		} else {
			s.send(channelID, fmt.Sprintf("⚠️ [Error] Could not scale deployment `%s`: %v", deployName, err))
		}
	} else if len(parts) == 3 && parts[0] == "del_pod" {
		namespace, podName := parts[1], parts[2]
		if err := s.Ops.DeletePod(context.Background(), namespace, podName); err == nil {
			s.send(channelID, fmt.Sprintf("🗑️ [Executed] Forcefully deleted pod `%s`.", podName))
			CrossPlatformSync <- "Slack User Approved: Delete Pod " + podName
		} else {
			s.send(channelID, fmt.Sprintf("⚠️ [Error] Could not delete pod `%s`: %v", podName, err))
		}
	} else if len(parts) == 3 && parts[0] == "restart_dep" {
		namespace, deployName := parts[1], parts[2]
		if err := s.Ops.RestartDeployment(context.Background(), namespace, deployName); err == nil {
			s.send(channelID, fmt.Sprintf("🔄 [Executed] Triggered rollout restart for deployment `%s`.", deployName))
			CrossPlatformSync <- "Slack User Approved: Restart " + deployName
		} else {
			s.send(channelID, fmt.Sprintf("⚠️ [Error] Could not trigger restart for deployment `%s`: %v", deployName, err))
		}
	}
}
