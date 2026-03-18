// Copyright 2024 KubeSolv Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package alert

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"kubesolv/internal/ai"
	"kubesolv/internal/ops"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/generative-ai-go/genai"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

var telegramLog = ctrl.Log.WithName("telegram")

// ── Types ──────────────────────────────────────────────────────────

// InviteCode represents a time-limited, usage-limited registration code.
type InviteCode struct {
	Code      string
	ExpiresAt time.Time
	MaxUses   int
	UsedCount int
}

// RateEntry tracks failed registration attempts for brute-force protection.
type RateEntry struct {
	Count       int
	WindowStart time.Time
}

// TelegramBot supports multi-user alerts with invite-based registration,
// brute-force protection, and first-approver-wins action deduplication.
type TelegramBot struct {
	Bot           *tgbotapi.BotAPI
	AdminChatID   int64 // from env TELEGRAM_CHAT_ID — the deployer
	AdminUsername string
	Ops           *ops.OpsManager
	AI            *ai.Client
	kubeClient    *kubernetes.Clientset

	// Runtime state — all protected by their respective sync.Map
	ChatIDs         sync.Map // int64 -> string (chatID -> username)
	InviteCodes     sync.Map // string -> *InviteCode
	ExecutedActions sync.Map // string -> bool (action dedup)
	FailedAttempts  sync.Map // int64 -> *RateEntry (brute-force protection)
}

// NewTelegramBot creates a new TelegramBot instance.
// The admin is automatically registered. Call SetKubeClient() after
// construction to enable ConfigMap persistence.
func NewTelegramBot(token string, adminChatID int64, opsManager *ops.OpsManager, aiClient *ai.Client) *TelegramBot {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil
	}
	return &TelegramBot{
		Bot:         bot,
		AdminChatID: adminChatID,
		Ops:         opsManager,
		AI:          aiClient,
	}
}

// SetKubeClient enables ConfigMap-based subscriber persistence.
// Call this after NewTelegramBot and before Start().
func (t *TelegramBot) SetKubeClient(kc *kubernetes.Clientset) {
	t.kubeClient = kc
}

// ── ConfigMap Persistence ──────────────────────────────────────────

const subscriberConfigMap = "kubesolv-subscribers"
const subscriberNamespace = "kubesolv"

// loadSubscribers reads registered chat IDs from the kubesolv-subscribers ConfigMap.
// If the ConfigMap does not exist, it creates one with empty data.
func (t *TelegramBot) loadSubscribers(ctx context.Context) {
	if t.kubeClient == nil {
		return
	}

	cm, err := t.kubeClient.CoreV1().ConfigMaps(subscriberNamespace).Get(ctx, subscriberConfigMap, metav1.GetOptions{})
	if err != nil {
		// Create it with empty data
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subscriberConfigMap,
				Namespace: subscriberNamespace,
			},
			Data: map[string]string{"chat_ids": ""},
		}
		_, createErr := t.kubeClient.CoreV1().ConfigMaps(subscriberNamespace).Create(ctx, newCM, metav1.CreateOptions{})
		if createErr != nil {
			telegramLog.Error(createErr, "Failed to create kubesolv-subscribers ConfigMap")
		} else {
			telegramLog.Info("Created kubesolv-subscribers ConfigMap")
		}
		return
	}

	ids, ok := cm.Data["chat_ids"]
	if !ok || ids == "" {
		return
	}

	for _, entry := range strings.Split(ids, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) == 2 {
			if id, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				t.ChatIDs.Store(id, parts[1])
			}
		}
	}
	telegramLog.Info("Loaded subscribers from ConfigMap")
}

// saveSubscribers serializes all registered chat IDs to the ConfigMap.
func (t *TelegramBot) saveSubscribers(ctx context.Context) {
	if t.kubeClient == nil {
		return
	}

	var entries []string
	t.ChatIDs.Range(func(key, value any) bool {
		entries = append(entries, fmt.Sprintf("%d:%s", key.(int64), value.(string)))
		return true
	})
	data := strings.Join(entries, ",")

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subscriberConfigMap,
			Namespace: subscriberNamespace,
		},
		Data: map[string]string{"chat_ids": data},
	}

	_, err := t.kubeClient.CoreV1().ConfigMaps(subscriberNamespace).Get(ctx, subscriberConfigMap, metav1.GetOptions{})
	if err != nil {
		_, err = t.kubeClient.CoreV1().ConfigMaps(subscriberNamespace).Create(ctx, cm, metav1.CreateOptions{})
	} else {
		_, err = t.kubeClient.CoreV1().ConfigMaps(subscriberNamespace).Update(ctx, cm, metav1.UpdateOptions{})
	}
	if err != nil {
		telegramLog.Error(err, "Failed to persist subscribers to ConfigMap")
	}
}

// ── Authorization Helpers ──────────────────────────────────────────

// isAuthorized returns true if chatID is the admin or a registered member.
func (t *TelegramBot) isAuthorized(chatID int64) bool {
	if chatID == t.AdminChatID {
		return true
	}
	_, ok := t.ChatIDs.Load(chatID)
	return ok
}

// isAdmin returns true only for the deployer.
func (t *TelegramBot) isAdmin(chatID int64) bool {
	return chatID == t.AdminChatID
}

// checkBruteForce returns true if chatID should be blocked (>= 5 failures in 10 min).
func (t *TelegramBot) checkBruteForce(chatID int64) bool {
	val, ok := t.FailedAttempts.Load(chatID)
	if !ok {
		return false
	}
	entry := val.(*RateEntry)
	if time.Since(entry.WindowStart) > 10*time.Minute {
		// Window expired — reset
		t.FailedAttempts.Delete(chatID)
		return false
	}
	return entry.Count >= 5
}

// recordFailedAttempt increments the failure counter for brute-force tracking.
func (t *TelegramBot) recordFailedAttempt(chatID int64) {
	val, ok := t.FailedAttempts.Load(chatID)
	if !ok || time.Since(val.(*RateEntry).WindowStart) > 10*time.Minute {
		t.FailedAttempts.Store(chatID, &RateEntry{Count: 1, WindowStart: time.Now()})
		return
	}
	entry := val.(*RateEntry)
	entry.Count++
}

// subscriberCount returns the number of registered team members (excluding admin).
func (t *TelegramBot) subscriberCount() int {
	count := 0
	t.ChatIDs.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// ── Main Loop ──────────────────────────────────────────────────────

// Start begins the Telegram bot update loop. It loads subscribers from
// ConfigMap on startup and sends a ready message to the admin.
func (t *TelegramBot) Start() {
	t.loadSubscribers(context.Background())

	// Startup message to admin
	if t.AdminChatID != 0 {
		memberCount := t.subscriberCount()
		t.send(t.AdminChatID, fmt.Sprintf(
			"✅ *KubeSolv is online*\n"+
				"Connected to your cluster and ready.\n\n"+
				"Team size: %d registered members\n\n"+
				"To invite a teammate: /invite\n"+
				"To see who's registered: /team", memberCount))
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := t.Bot.GetUpdatesChan(u)

	for update := range updates {
		// A) Callback queries (button clicks)
		if update.CallbackQuery != nil {
			t.handleInteraction(update.CallbackQuery)
			continue
		}

		// B) Text messages
		if update.Message == nil || update.Message.Text == "" {
			continue
		}

		chatID := update.Message.Chat.ID
		username := update.Message.From.UserName
		if username == "" {
			username = update.Message.From.FirstName
		}
		text := strings.TrimSpace(update.Message.Text)

		// Capture admin username on first contact
		if t.isAdmin(chatID) && t.AdminUsername == "" && update.Message.From.UserName != "" {
			t.AdminUsername = update.Message.From.UserName
		}

		// /register is open — anyone can attempt it (brute-force protection applies)
		if strings.HasPrefix(text, "/register") {
			t.handleRegister(chatID, username, text)
			continue
		}

		// All other messages require authorization
		if !t.isAuthorized(chatID) {
			t.send(chatID, "👋 Type /register <invite_code> to get cluster alerts.")
			continue
		}

		// Route authorized commands
		switch {
		case text == "/invite":
			if !t.isAdmin(chatID) {
				t.send(chatID, "❌ This command is restricted to the admin.")
				continue
			}
			t.handleGenerateInvite(chatID)
		case text == "/unregister":
			t.handleUnregister(chatID, username)
		case text == "/team":
			t.handleListTeam(chatID)
		case strings.HasPrefix(text, "/remove "):
			if !t.isAdmin(chatID) {
				t.send(chatID, "❌ This command is restricted to the admin.")
				continue
			}
			t.handleRemoveUser(chatID, text)
		case text == "/help":
			t.handleHelp(chatID)
		default:
			t.routeMessage(chatID, text)
		}
	}
}

// ── Command Handlers ───────────────────────────────────────────────

// handleGenerateInvite creates a new invite code and sends it to the admin.
func (t *TelegramBot) handleGenerateInvite(adminChatID int64) {
	code := "KS-" + randomAlphanumeric(8)
	t.InviteCodes.Store(code, &InviteCode{
		Code:      code,
		ExpiresAt: time.Now().Add(24 * time.Hour),
		MaxUses:   10,
		UsedCount: 0,
	})
	t.send(adminChatID, fmt.Sprintf(
		"🔑 *Invite Code Generated*\n"+
			"Code: `%s`\n"+
			"Expires: 24 hours\n"+
			"Max uses: 10\n\n"+
			"Share this with your team via a secure channel (Slack DM, email, etc).\n"+
			"Each engineer types: /register %s", code, code))
}

// handleRegister processes a /register <code> attempt with full security checks.
func (t *TelegramBot) handleRegister(chatID int64, username, rawText string) {
	// 1. Brute-force check
	if t.checkBruteForce(chatID) {
		t.send(chatID, "🚫 Too many attempts. Try again in 10 minutes.")
		return
	}

	// 2. Admin is always registered
	if chatID == t.AdminChatID {
		t.send(chatID, "ℹ️ Admin is already registered.")
		return
	}

	// 3. Already registered
	if t.isAuthorized(chatID) {
		t.send(chatID, "ℹ️ You are already registered.")
		return
	}

	// 4. Parse code
	code := strings.TrimSpace(strings.TrimPrefix(rawText, "/register"))
	if code == "" {
		t.recordFailedAttempt(chatID)
		t.send(chatID, "❌ Usage: /register <invite_code>")
		return
	}

	// 5. Validate code
	val, ok := t.InviteCodes.Load(code)
	if !ok {
		t.recordFailedAttempt(chatID)
		t.send(chatID, "❌ Invalid invite code.")
		return
	}
	inv := val.(*InviteCode)

	if time.Now().After(inv.ExpiresAt) {
		t.recordFailedAttempt(chatID)
		t.send(chatID, "❌ Invite code expired. Ask your admin for a new one.")
		return
	}
	if inv.UsedCount >= inv.MaxUses {
		t.send(chatID, "❌ This invite code has been fully used. Ask your admin for a new one.")
		return
	}

	// 6. All checks passed — register
	inv.UsedCount++
	t.ChatIDs.Store(chatID, username)
	t.saveSubscribers(context.Background())

	t.send(chatID, fmt.Sprintf(
		"✅ *Welcome to KubeSolv!*\n"+
			"Hey @%s, you're now registered.\n"+
			"You'll receive all cluster alerts and can approve actions.\n"+
			"Type /help to see what you can ask me.", username))

	if t.AdminChatID != 0 {
		t.send(t.AdminChatID, fmt.Sprintf(
			"👋 @%s joined the team.\nTeam size: %d members.", username, t.subscriberCount()))
	}
}

// handleUnregister removes the caller from alerts. Admin cannot unregister.
func (t *TelegramBot) handleUnregister(chatID int64, username string) {
	if t.isAdmin(chatID) {
		t.send(chatID, "❌ The admin account cannot be unregistered.")
		return
	}
	if !t.isAuthorized(chatID) {
		t.send(chatID, "❌ You are not registered.")
		return
	}
	t.ChatIDs.Delete(chatID)
	t.saveSubscribers(context.Background())
	t.send(chatID, fmt.Sprintf("👋 @%s, you've been unregistered and will no longer receive alerts.", username))
	if t.AdminChatID != 0 {
		t.send(t.AdminChatID, fmt.Sprintf("ℹ️ @%s has unregistered from cluster alerts. Team size: %d", username, t.subscriberCount()))
	}
}

// handleRemoveUser lets the admin forcibly remove a team member by username.
func (t *TelegramBot) handleRemoveUser(adminChatID int64, rawText string) {
	target := strings.TrimSpace(strings.TrimPrefix(rawText, "/remove"))
	target = strings.TrimPrefix(target, "@")
	if target == "" {
		t.send(adminChatID, "❌ Usage: /remove @username")
		return
	}

	var foundID int64
	var found bool
	t.ChatIDs.Range(func(key, value any) bool {
		if value.(string) == target {
			foundID = key.(int64)
			found = true
			return false
		}
		return true
	})

	if !found {
		t.send(adminChatID, fmt.Sprintf("❌ User @%s not found in the team.", target))
		return
	}

	t.ChatIDs.Delete(foundID)
	t.saveSubscribers(context.Background())
	t.send(adminChatID, fmt.Sprintf("✅ @%s has been removed from cluster alerts.", target))
	t.send(foundID, "ℹ️ You have been removed from KubeSolv alerts by the admin.")
}

// handleListTeam shows all registered team members, sorted alphabetically.
func (t *TelegramBot) handleListTeam(chatID int64) {
	var users []string
	t.ChatIDs.Range(func(_, value any) bool {
		users = append(users, "@"+value.(string))
		return true
	})
	sort.Strings(users)

	adminLabel := "admin"
	if t.AdminUsername != "" {
		adminLabel = "@" + t.AdminUsername
	}

	totalCount := len(users) + 1 // +1 for admin
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👥 *Team (%d members):*\n", totalCount))
	sb.WriteString(fmt.Sprintf("Admin: %s (deployer)\n", adminLabel))
	sb.WriteString("─────────────────\n")
	for _, u := range users {
		sb.WriteString(u + "\n")
	}
	if len(users) == 0 {
		sb.WriteString("_No other members yet. Use /invite to add your team._\n")
	}
	t.send(chatID, sb.String())
}

// handleHelp sends the full command reference.
func (t *TelegramBot) handleHelp(chatID int64) {
	t.send(chatID, "🤖 *KubeSolv Commands*\n\n"+
		"/help           — Show this message\n"+
		"/team           — List registered team members\n"+
		"/unregister     — Remove yourself from alerts\n\n"+
		"*Admin only:*\n"+
		"/invite         — Generate a new invite code\n"+
		"/remove @user   — Remove a team member\n\n"+
		"*Ask me anything:*\n"+
		"'Why is the frontend crashing?'\n"+
		"'Scale payments to 3 replicas'\n"+
		"'Show me logs for api-server'\n"+
		"'What's the health of the cluster?'")
}

// ── AI Routing ─────────────────────────────────────────────────────

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

// ── Messaging ──────────────────────────────────────────────────────

func (t *TelegramBot) send(chatID int64, text string) {
	if len(text) > 4000 {
		text = text[:4000] + "\n...[truncated]"
	}
	msg := tgbotapi.NewMessage(chatID, text)
	_, _ = t.Bot.Send(msg)
}

// Broadcast sends a message to ALL authorized users (admin + registered members).
func (t *TelegramBot) Broadcast(category, title, message string) {
	text := fmt.Sprintf("🚨 *%s*\n%s", title, message)
	sent := make(map[int64]bool)

	// Admin first
	if t.AdminChatID != 0 {
		t.send(t.AdminChatID, text)
		sent[t.AdminChatID] = true
	}

	// All registered members
	t.ChatIDs.Range(func(key, _ any) bool {
		id := key.(int64)
		if !sent[id] {
			t.send(id, text)
			sent[id] = true
		}
		return true
	})
}

// BroadcastWithAction sends an action button to ALL authorized users.
func (t *TelegramBot) BroadcastWithAction(category, title, message, actionID, buttonText string) {
	sent := make(map[int64]bool)

	sendOne := func(chatID int64) {
		if sent[chatID] {
			return
		}
		t.sendWithAction(chatID, title, message, actionID, buttonText)
		sent[chatID] = true
	}

	if t.AdminChatID != 0 {
		sendOne(t.AdminChatID)
	}
	t.ChatIDs.Range(func(key, _ any) bool {
		sendOne(key.(int64))
		return true
	})
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

// ── Interaction Handler (First-Approver-Wins) ──────────────────────

func (t *TelegramBot) handleInteraction(query *tgbotapi.CallbackQuery) {
	actionID := query.Data
	approverChatID := query.Message.Chat.ID
	approverUsername := query.From.UserName
	if approverUsername == "" {
		approverUsername = query.From.FirstName
	}

	// Security: only authorized users can approve
	if !t.isAuthorized(approverChatID) {
		return
	}

	// First-approver wins: atomically claim this action
	_, alreadyExecuted := t.ExecutedActions.LoadOrStore(actionID, true)
	if alreadyExecuted {
		t.send(approverChatID, "⚡ Already executed by a teammate.")
		cb := tgbotapi.NewCallback(query.ID, "Already done")
		_, _ = t.Bot.Request(cb)
		return
	}

	// Acknowledge callback immediately
	cb := tgbotapi.NewCallback(query.ID, "Executing...")
	_, _ = t.Bot.Request(cb)

	parts := strings.Split(actionID, "|")

	if len(parts) == 5 && parts[0] == "patch_mem" {
		namespace, deployName, containerName, newLimit := parts[1], parts[2], parts[3], parts[4]
		if err := t.Ops.PatchMemoryLimit(namespace, deployName, containerName, newLimit); err == nil {
			t.Broadcast("cluster", "✅ Action Executed", fmt.Sprintf("@%s approved: Increased memory limit to `%s` for deployment `%s`.", approverUsername, newLimit, deployName))
			CrossPlatformSync <- "Telegram User Approved: Patch Memory for " + deployName
		} else {
			t.send(approverChatID, fmt.Sprintf("⚠️ [Error] Could not patch memory: %v", err))
		}
	} else if len(parts) == 3 && parts[0] == "rollback" {
		namespace, deployName := parts[1], parts[2]
		if err := t.Ops.RollbackDeployment(namespace, deployName); err == nil {
			t.Broadcast("cluster", "✅ Action Executed", fmt.Sprintf("@%s approved: Rollback for deployment `%s`.", approverUsername, deployName))
			CrossPlatformSync <- "Telegram User Approved: Rollback for " + deployName
		} else {
			t.send(approverChatID, fmt.Sprintf("⚠️ [Error] Could not rollback: %v", err))
		}
	} else if len(parts) == 2 && parts[0] == "cordon" {
		nodeName := parts[1]
		if err := t.Ops.CordonNode(context.Background(), nodeName); err == nil {
			t.Broadcast("cluster", "✅ Action Executed", fmt.Sprintf("@%s approved: Node `%s` cordoned.", approverUsername, nodeName))
			CrossPlatformSync <- "Telegram User Approved: Cordon Node " + nodeName
		} else {
			t.send(approverChatID, fmt.Sprintf("⚠️ [Error] Could not cordon node `%s`: %v", nodeName, err))
		}
	} else if len(parts) == 2 && parts[0] == "uncordon" {
		nodeName := parts[1]
		if err := t.Ops.UncordonNode(context.Background(), nodeName); err == nil {
			t.Broadcast("cluster", "✅ Action Executed", fmt.Sprintf("@%s approved: Node `%s` uncordoned.", approverUsername, nodeName))
			CrossPlatformSync <- "Telegram User Approved: Uncordon Node " + nodeName
		} else {
			t.send(approverChatID, fmt.Sprintf("⚠️ [Error] Could not uncordon node `%s`: %v", nodeName, err))
		}
	} else if len(parts) == 4 && parts[0] == "scale" {
		namespace, deployName, replicas := parts[1], parts[2], parts[3]
		if err := t.Ops.ScaleDeployment(context.Background(), namespace, deployName, replicas); err == nil {
			t.Broadcast("cluster", "✅ Action Executed", fmt.Sprintf("@%s approved: Scaled `%s` to `%s` replica(s).", approverUsername, deployName, replicas))
			CrossPlatformSync <- "Telegram User Approved: Scale " + deployName + " to " + replicas
		} else {
			t.send(approverChatID, fmt.Sprintf("⚠️ [Error] Could not scale deployment `%s`: %v", deployName, err))
		}
	} else if len(parts) == 3 && parts[0] == "del_pod" {
		namespace, podName := parts[1], parts[2]
		if err := t.Ops.DeletePod(context.Background(), namespace, podName); err == nil {
			t.Broadcast("cluster", "✅ Action Executed", fmt.Sprintf("@%s approved: Deleted pod `%s`.", approverUsername, podName))
			CrossPlatformSync <- "Telegram User Approved: Delete Pod " + podName
		} else {
			t.send(approverChatID, fmt.Sprintf("⚠️ [Error] Could not delete pod `%s`: %v", podName, err))
		}
	} else if len(parts) == 3 && parts[0] == "restart_dep" {
		namespace, deployName := parts[1], parts[2]
		if err := t.Ops.RestartDeployment(context.Background(), namespace, deployName); err == nil {
			t.Broadcast("cluster", "✅ Action Executed", fmt.Sprintf("@%s approved: Rollout restart for `%s`.", approverUsername, deployName))
			CrossPlatformSync <- "Telegram User Approved: Restart " + deployName
		} else {
			t.send(approverChatID, fmt.Sprintf("⚠️ [Error] Could not restart deployment `%s`: %v", deployName, err))
		}
	}
}

// ── Helpers ────────────────────────────────────────────────────────

// randomAlphanumeric generates n random uppercase alphanumeric characters using crypto/rand.
func randomAlphanumeric(n int) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[num.Int64()]
	}
	return string(b)
}
