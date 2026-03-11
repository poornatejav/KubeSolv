package alert

import (
	"context"
	"fmt"
	"strings"

	"kubesolv/internal/ai"
	"kubesolv/internal/ops"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/generative-ai-go/genai"
)

type TelegramBot struct {
	Bot    *tgbotapi.BotAPI
	ChatID int64
	Ops    *ops.OpsManager
	AI     *ai.Client
}

func NewTelegramBot(token string, chatID int64, opsManager *ops.OpsManager, aiClient *ai.Client) *TelegramBot {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil
	}
	return &TelegramBot{
		Bot:    bot,
		ChatID: chatID,
		Ops:    opsManager,
		AI:     aiClient,
	}
}

func (t *TelegramBot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := t.Bot.GetUpdatesChan(u)

	for update := range updates {
		if update.CallbackQuery != nil {
			t.handleInteraction(update.CallbackQuery)
			continue
		}
		if update.Message != nil && update.Message.Text != "" {
			t.routeMessage(update.Message.Chat.ID, update.Message.Text)
		}
	}
}

func (t *TelegramBot) routeMessage(chatID int64, msg string) {
	if t.AI == nil {
		t.send(chatID, "⚠️ AI Client not configured.")
		return
	}

	ctx := context.Background()
	resp, err := t.AI.SendChatMessage(ctx, fmt.Sprintf("%d", chatID), msg)

	for {
		if err != nil {
			t.send(chatID, fmt.Sprintf("⚠️ AI Error: %v", err))
			return
		}

		if len(resp.Candidates) == 0 {
			t.send(chatID, "⚠️ AI returned an empty response.")
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
			t.send(chatID, replyText)
		}

		if len(toolCalls) == 0 {
			break
		}

		var toolResponses []*genai.FunctionResponse
		for _, call := range toolCalls {
			result := t.executeTool(ctx, chatID, call)
			toolResponses = append(toolResponses, &genai.FunctionResponse{
				Name: call.Name,
				Response: map[string]any{
					"result": result,
				},
			})
		}

		resp, err = t.AI.SendFunctionResponse(ctx, fmt.Sprintf("%d", chatID), toolResponses)
	}
}

func (t *TelegramBot) executeTool(ctx context.Context, chatID int64, call genai.FunctionCall) any {
	const approvalMsg = approvalMsg
	switch call.Name {
	case "get_health_report":
		return t.Ops.GetHealthReport()
	case "get_resource_usage":
		return t.Ops.GetResourceUsage()
	case "get_recent_events":
		return t.Ops.GetRecentEvents()
	case "get_pod_logs":
		podName, _ := call.Args["pod_name"].(string)
		return t.Ops.GetLogs(podName)
	case "scale_deployment":
		namespace, _ := call.Args["namespace"].(string)
		deployName, _ := call.Args["deployment_name"].(string)
		replicas, _ := call.Args["replicas"].(string)
		actionID := fmt.Sprintf("scale|%s|%s|%s", namespace, deployName, replicas)
		t.sendWithAction(chatID, "Action Required: Scale Deployment", fmt.Sprintf("AI proposes scaling `%s` in `%s` to `%s` replicas.", deployName, namespace, replicas), actionID, "Approve Scale")
		return approvalMsg
	case "list_pods":
		namespace, _ := call.Args["namespace"].(string)
		return t.Ops.ListPods(ctx, namespace)
	case "get_pod_details":
		namespace, _ := call.Args["namespace"].(string)
		podName, _ := call.Args["pod_name"].(string)
		return t.Ops.GetPodDetails(ctx, namespace, podName)
	case "delete_pod":
		namespace, _ := call.Args["namespace"].(string)
		podName, _ := call.Args["pod_name"].(string)
		actionID := fmt.Sprintf("del_pod|%s|%s", namespace, podName)
		t.sendWithAction(chatID, "Action Required: Delete Pod", fmt.Sprintf("AI proposes to force-delete pod `%s` in `%s`.", podName, namespace), actionID, "Approve Delete")
		return approvalMsg
	case "restart_deployment":
		namespace, _ := call.Args["namespace"].(string)
		deployName, _ := call.Args["deployment_name"].(string)
		actionID := fmt.Sprintf("restart_dep|%s|%s", namespace, deployName)
		t.sendWithAction(chatID, "Action Required: Restart Deployment", fmt.Sprintf("AI proposes rollout restart for `%s` in `%s`.", deployName, namespace), actionID, "Approve Restart")
		return approvalMsg
	//nolint:goconst
	case "cordon_node":
		nodeName, _ := call.Args["node_name"].(string)
		actionID := fmt.Sprintf("cordon|%s", nodeName)
		t.sendWithAction(chatID, "Action Required: Cordon Node", fmt.Sprintf("AI proposes to cordon node `%s`.", nodeName), actionID, "Approve Cordon")
		return approvalMsg
	case "uncordon_node":
		nodeName, _ := call.Args["node_name"].(string)
		actionID := fmt.Sprintf("uncordon|%s", nodeName)
		t.sendWithAction(chatID, "Action Required: Uncordon Node", fmt.Sprintf("AI proposes to uncordon node `%s`.", nodeName), actionID, "Approve Uncordon")
		return approvalMsg
	default:
		return "Unknown tool called"
	}
}

func (t *TelegramBot) send(chatID int64, text string) {
	if len(text) > 4000 {
		text = text[:4000] + "\n...[truncated]"
	}
	msg := tgbotapi.NewMessage(chatID, text)
	_, _ = t.Bot.Send(msg)
}

func (t *TelegramBot) Broadcast(category, title, message string) {
	if t.ChatID != 0 {
		t.send(t.ChatID, fmt.Sprintf("🚨 *%s*\n%s", title, message))
	}
}

func (t *TelegramBot) BroadcastWithAction(category, title, message, actionID, buttonText string) {
	if t.ChatID == 0 {
		return
	}
	msg := tgbotapi.NewMessage(t.ChatID, fmt.Sprintf("🚨 *%s*\n%s", title, message))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText, actionID),
		),
	)
	_, _ = t.Bot.Send(msg)
}

func (t *TelegramBot) sendWithAction(chatID int64, title, message, actionID, buttonText string) {
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🚨 *%s*\n%s", title, message))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText, actionID),
		),
	)
	_, _ = t.Bot.Send(msg)
}

func (t *TelegramBot) handleInteraction(query *tgbotapi.CallbackQuery) {
	parts := strings.Split(query.Data, "|")
	chatID := query.Message.Chat.ID

	if len(parts) == 5 && parts[0] == "patch_mem" {
		namespace, deployName, containerName, newLimit := parts[1], parts[2], parts[3], parts[4]
		if err := t.Ops.PatchMemoryLimit(namespace, deployName, containerName, newLimit); err == nil {
			msg := fmt.Sprintf("✨ [Approved] Increased memory limit to `%s` for deployment `%s`.", newLimit, deployName)
			t.send(chatID, msg)
			CrossPlatformSync <- "Telegram User Approved: Patch Memory for " + deployName
		}
	} else if len(parts) == 3 && parts[0] == "rollback" {
		namespace, deployName := parts[1], parts[2]
		if err := t.Ops.RollbackDeployment(namespace, deployName); err == nil {
			t.send(chatID, fmt.Sprintf("🔄 [Approved] Initiated rollback sequence for deployment `%s`.", deployName))
			CrossPlatformSync <- "Telegram User Approved: Rollback for " + deployName
		}
	} else if len(parts) == 2 && parts[0] == "cordon" {
		nodeName := parts[1]
		if err := t.Ops.CordonNode(context.Background(), nodeName); err == nil {
			t.send(chatID, fmt.Sprintf("🔒 [Approved] Node `%s` successfully cordoned (marked unschedulable).", nodeName))
			CrossPlatformSync <- "Telegram User Approved: Cordon Node " + nodeName
		} else {
			t.send(chatID, fmt.Sprintf("⚠️ [Error] Could not cordon node `%s`: %v", nodeName, err))
		}
	} else if len(parts) == 2 && parts[0] == "uncordon" {
		nodeName := parts[1]
		if err := t.Ops.UncordonNode(context.Background(), nodeName); err == nil {
			t.send(chatID, fmt.Sprintf("🔓 [Approved] Node `%s` uncordoned and actively scheduling.", nodeName))
			CrossPlatformSync <- "Telegram User Approved: Uncordon Node " + nodeName
		} else {
			t.send(chatID, fmt.Sprintf("⚠️ [Error] Could not uncordon node `%s`: %v", nodeName, err))
		}
	} else if len(parts) == 4 && parts[0] == "scale" {
		namespace, deployName, replicas := parts[1], parts[2], parts[3]
		if err := t.Ops.ScaleDeployment(context.Background(), namespace, deployName, replicas); err == nil {
			t.send(chatID, fmt.Sprintf("🚀 [Executed] Scaled deployment `%s` to `%s` replica(s).", deployName, replicas))
			CrossPlatformSync <- "Telegram User Approved: Scale " + deployName + " to " + replicas
		} else {
			t.send(chatID, fmt.Sprintf("⚠️ [Error] Could not scale deployment `%s`: %v", deployName, err))
		}
	} else if len(parts) == 3 && parts[0] == "del_pod" {
		namespace, podName := parts[1], parts[2]
		if err := t.Ops.DeletePod(context.Background(), namespace, podName); err == nil {
			t.send(chatID, fmt.Sprintf("🗑️ [Executed] Forcefully deleted pod `%s`.", podName))
			CrossPlatformSync <- "Telegram User Approved: Delete Pod " + podName
		} else {
			t.send(chatID, fmt.Sprintf("⚠️ [Error] Could not delete pod `%s`: %v", podName, err))
		}
	} else if len(parts) == 3 && parts[0] == "restart_dep" {
		namespace, deployName := parts[1], parts[2]
		if err := t.Ops.RestartDeployment(context.Background(), namespace, deployName); err == nil {
			t.send(chatID, fmt.Sprintf("🔄 [Executed] Triggered rollout restart for deployment `%s`.", deployName))
			CrossPlatformSync <- "Telegram User Approved: Restart " + deployName
		} else {
			t.send(chatID, fmt.Sprintf("⚠️ [Error] Could not trigger restart for deployment `%s`: %v", deployName, err))
		}
	}

	callback := tgbotapi.NewCallback(query.ID, "")
	_, _ = t.Bot.Request(callback)
}
