// package alert

// import (
// 	"context"
// 	"fmt"
// 	"strings"
// 	"sync"

// 	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
// 	"kubesolv/internal/ops"
// )

// type TelegramBot struct {
// 	Bot           *tgbotapi.BotAPI
// 	ChatID        int64
// 	Ops           *ops.OpsManager
// 	mu            sync.RWMutex
// 	Subscriptions map[int64]map[string]bool
// }

// func NewTelegramBot(token string, chatID int64, opsManager *ops.OpsManager) *TelegramBot {
// 	bot, err := tgbotapi.NewBotAPI(token)
// 	if err != nil {
// 		return nil
// 	}

// 	subs := make(map[int64]map[string]bool)
// 	if chatID != 0 {
// 		subs[chatID] = map[string]bool{"cluster": true}
// 	}

// 	return &TelegramBot{
// 		Bot:           bot,
// 		ChatID:        chatID,
// 		Ops:           opsManager,
// 		Subscriptions: subs,
// 	}
// }

// func (t *TelegramBot) Start() {
// 	u := tgbotapi.NewUpdate(0)
// 	u.Timeout = 60
// 	updates := t.Bot.GetUpdatesChan(u)

// 	go func() {
// 		for update := range updates {
// 			if update.Message != nil {
// 				if t.ChatID == 0 {
// 					t.ChatID = update.Message.Chat.ID
// 					t.mu.Lock()
// 					t.Subscriptions[t.ChatID] = map[string]bool{"cluster": true}
// 					t.mu.Unlock()
// 					t.send(t.ChatID, "👋 *KubeSolv Connected!* I'm listening. Try asking 'status' or 'events'.")
// 				}

// 				t.routeMessage(update.Message.Chat.ID, update.Message.Text)
// 			} else if update.CallbackQuery != nil {
// 				t.handleInteraction(update.CallbackQuery)
// 			}
// 		}
// 	}()
// }

// func (t *TelegramBot) BroadcastWithAction(topic, title, message, actionID, buttonText string) {
// 	t.mu.RLock()
// 	defer t.mu.RUnlock()

// 	for chatID, subs := range t.Subscriptions {
// 		if subs["cluster"] || subs[topic] {
// 			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🚨 *%s*\n%s", title, message))
// 			msg.ParseMode = "Markdown"

// 			btn := tgbotapi.NewInlineKeyboardButtonData(buttonText, actionID)
// 			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btn))

// 			t.Bot.Send(msg)
// 		}
// 	}
// }

// func (t *TelegramBot) handleInteraction(query *tgbotapi.CallbackQuery) {
// 	parts := strings.Split(query.Data, "|")
// 	chatID := query.Message.Chat.ID

// 	if len(parts) == 5 && parts[0] == "patch_mem" {
// 		namespace, deployName, containerName, newLimit := parts[1], parts[2], parts[3], parts[4]
// 		if err := t.Ops.PatchMemoryLimit(namespace, deployName, containerName, newLimit); err == nil {
// 			msg := fmt.Sprintf("✅ Approved: Increased memory to %s for %s.", newLimit, deployName)
// 			t.send(chatID, msg)
// 			CrossPlatformSync <- "Telegram User Approved: Patch Memory for " + deployName
// 		}
// 	} else if len(parts) == 3 && parts[0] == "rollback" {
// 		namespace, deployName := parts[1], parts[2]
// 		if err := t.Ops.RollbackDeployment(namespace, deployName); err == nil {
// 			msg := fmt.Sprintf("⏪ Approved: Initiated Rollback for %s.", deployName)
// 			t.send(chatID, msg)
// 			CrossPlatformSync <- "Telegram User Approved: Rollback for " + deployName
// 		}
// 	} else if len(parts) == 2 && parts[0] == "cordon_node" {
// 		nodeName := parts[1]
// 		if err := t.Ops.CordonNode(context.Background(), nodeName); err == nil {
// 			msg := fmt.Sprintf("🚧 Approved: Node `%s` cordoned (marked unschedulable).", nodeName)
// 			t.send(chatID, msg)
// 			CrossPlatformSync <- "Telegram User Approved: Cordon Node " + nodeName
// 		}
// 	}

// 	callback := tgbotapi.NewCallback(query.ID, "")
// 	_ = t.Bot.Request(callback)
// }

// func (t *TelegramBot) routeMessage(chatID int64, msg string) {
// 	msg = strings.ToLower(msg)

// 	if strings.Contains(msg, "hi") || strings.Contains(msg, "hello") || strings.Contains(msg, "help") || strings.Contains(msg, "start") {
// 		t.send(chatID, "🤖 *I am KubeSolv.*\n\nYou can ask me things like:\n• \"status\"\n• \"metrics\"\n• \"events\"\n• \"logs <pod>\"")
// 	} else if strings.Contains(msg, "status") {
// 		t.send(chatID, t.Ops.GetHealthReport())
// 	} else if strings.Contains(msg, "metrics") || strings.Contains(msg, "usage") {
// 		t.send(chatID, t.Ops.GetResourceUsage())
// 	} else if strings.Contains(msg, "events") {
// 		t.send(chatID, t.Ops.GetRecentEvents())
// 	} else if strings.HasPrefix(msg, "logs") {
// 		parts := strings.Fields(msg)
// 		if len(parts) > 1 {
// 			t.send(chatID, t.Ops.GetLogs(parts[1]))
// 		} else {
// 			t.send(chatID, "⚠️ Usage: `logs <pod-name>`")
// 		}
// 	} else {
// 		t.send(chatID, "🤔 I didn't catch that. Try asking for *status*, *metrics*, *events*, or *logs <pod>*.")
// 	}
// }

// func (t *TelegramBot) Broadcast(source, namespace, reason, message string) {
// 	if t.ChatID == 0 {
// 		return
// 	}
// 	t.send(t.ChatID, fmt.Sprintf("🚨 *Alert: %s*\n\n%s", reason, message))
// }

// func (t *TelegramBot) send(chatID int64, text string) {
// 	// Truncate to fix "Events not working" if the string is too long for Telegram
// 	if len(text) > 4000 {
// 		text = text[:4000] + "\n...[truncated]"
// 	}
// 	msg := tgbotapi.NewMessage(chatID, text)
// 	_, _ = t.Bot.Send(msg)
// }

// ------------------------------

package alert

import (
	"context"
	"fmt"
	"strings"

	"kubesolv/internal/ai"
	"kubesolv/internal/ops"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

	intent, err := t.AI.ProcessCommand(context.Background(), fmt.Sprintf("%d", chatID), msg)
	if err != nil {
		t.send(chatID, fmt.Sprintf("⚠️ Error analyzing request: %v", err))
		return
	}

	switch intent.Action {
	case "status":
		t.send(chatID, t.Ops.GetHealthReport())
	case "metrics":
		t.send(chatID, t.Ops.GetResourceUsage())
	case "events":
		t.send(chatID, t.Ops.GetRecentEvents())
	case "logs":
		logs := t.Ops.GetLogs(intent.Target)
		if logs == "" || strings.Contains(logs, "Error") {
			t.send(chatID, fmt.Sprintf("❌ Could not fetch logs for `%s`.", intent.Target))
			return
		}
		t.send(chatID, fmt.Sprintf("📜 *Logs for %s:*\n```\n%s\n```", intent.Target, logs))
	//nolint:goconst
		case "scale":
		err := t.Ops.ScaleDeployment(context.Background(), intent.Namespace, intent.Target, intent.Value)
		if err != nil {
			t.send(chatID, fmt.Sprintf("❌ Failed to scale: %v", err))
			return
		}
		t.send(chatID, fmt.Sprintf("✅ Executed: Scaled `%s` to %s replicas.\n🤖 %s", intent.Target, intent.Value, intent.Reply))
	default:
		t.send(chatID, intent.Reply)
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

func (t *TelegramBot) handleInteraction(query *tgbotapi.CallbackQuery) {
	parts := strings.Split(query.Data, "|")
	chatID := query.Message.Chat.ID

	if len(parts) == 5 && parts[0] == "patch_mem" {
		namespace, deployName, containerName, newLimit := parts[1], parts[2], parts[3], parts[4]
		if err := t.Ops.PatchMemoryLimit(namespace, deployName, containerName, newLimit); err == nil {
			msg := fmt.Sprintf("✅ Approved: Increased memory to %s for %s.", newLimit, deployName)
			t.send(chatID, msg)
			CrossPlatformSync <- "Telegram User Approved: Patch Memory for " + deployName
		}
	} else if len(parts) == 3 && parts[0] == "rollback" {
		namespace, deployName := parts[1], parts[2]
		if err := t.Ops.RollbackDeployment(namespace, deployName); err == nil {
			msg := fmt.Sprintf("⏪ Approved: Initiated Rollback for %s.", deployName)
			t.send(chatID, msg)
			CrossPlatformSync <- "Telegram User Approved: Rollback for " + deployName
		}
	} else if len(parts) == 2 && parts[0] == "cordon_node" {
		nodeName := parts[1]
		if err := t.Ops.CordonNode(context.Background(), nodeName); err == nil {
			msg := fmt.Sprintf("🚧 Approved: Node `%s` cordoned (marked unschedulable).", nodeName)
			t.send(chatID, msg)
			CrossPlatformSync <- "Telegram User Approved: Cordon Node " + nodeName
		}
	} else if len(parts) == 4 && parts[0] == "scale" {
		namespace, deployName, replicas := parts[1], parts[2], parts[3]
		if err := t.Ops.ScaleDeployment(context.Background(), namespace, deployName, replicas); err == nil {
			msg := fmt.Sprintf("✅ Executed: Scaled `%s` to %s replica(s) for cost optimization.", deployName, replicas)
			t.send(chatID, msg)
			CrossPlatformSync <- "Telegram User Approved: Scale " + deployName + " to " + replicas
		} else {
			t.send(chatID, fmt.Sprintf("❌ Failed to scale `%s`: %v", deployName, err))
		}
	}

	callback := tgbotapi.NewCallback(query.ID, "")
	_ = t.Bot.Request(callback)
}
