// package alert

// import (
// 	"bytes"
// 	"encoding/json"
// 	"fmt"
// 	"net/http"
// 	"sync"
// 	"time"
// )

// type TelegramBot struct {
// 	Token       string
// 	Subscribers map[string]bool
// 	mu          sync.RWMutex
// }

// func NewTelegramBot(token string) *TelegramBot {
// 	return &TelegramBot{
// 		Token:       token,
// 		Subscribers: make(map[string]bool),
// 	}
// }

// func (t *TelegramBot) Listen() {
// 	offset := 0
// 	fmt.Println("🎧 KubeSolv is listening (Conversational Mode)...")

// 	for {
// 		url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=10", t.Token, offset)
// 		resp, err := http.Get(url)
// 		if err != nil {
// 			time.Sleep(3 * time.Second)
// 			continue
// 		}

// 		var update Response
// 		if err := json.NewDecoder(resp.Body).Decode(&update); err == nil && update.Ok {
// 			for _, result := range update.Result {
// 				offset = result.UpdateID + 1

// 				chatID := fmt.Sprintf("%d", result.Message.Chat.ID)
// 				text := result.Message.Text
// 				username := result.Message.Chat.Username

// 				// --- NEW LOGIC: Conversational ---

// 				// 1. Check if they are new
// 				t.mu.Lock()
// 				isNew := !t.Subscribers[chatID]
// 				t.Subscribers[chatID] = true // Always subscribe them
// 				t.mu.Unlock()

// 				if isNew {
// 					fmt.Printf("👤 New Connection: %s says '%s'\n", username, text)
// 					t.SendOne(chatID, "👋 **Connected!**\nI've linked you to the cluster logs.\nI will ping you immediately if any pod crashes.")
// 				} else {
// 					// 2. Reply to existing users (Basic Chat)
// 					fmt.Printf("💬 Chat from %s: %s\n", username, text)
// 					t.SendOne(chatID, "✅ **System Nominal.**\nI am watching your pods. No active crashes detected right now.")
// 				}
// 			}
// 		}
// 		resp.Body.Close()
// 	}
// }

// func (t *TelegramBot) Broadcast(pod, namespace, reason, diagnosis string) {
// 	t.mu.RLock()
// 	defer t.mu.RUnlock()

// 	if len(t.Subscribers) == 0 {
// 		fmt.Println("⚠️ Alert generated, but nobody is connected via Telegram.")
// 		return
// 	}

// 	msg := fmt.Sprintf("🚨 *KubeSolv Alert*\n\n📦 *Pod:* `%s`\n📍 *Ns:* `%s`\n⚠️ *Issue:* %s\n\n🧠 *Diagnosis:*\n%s",
// 		pod, namespace, reason, diagnosis)

// 	for chatID := range t.Subscribers {
// 		go t.SendOne(chatID, msg)
// 	}
// }

// func (t *TelegramBot) SendOne(chatID, text string) {
// 	payload := map[string]string{"chat_id": chatID, "text": text, "parse_mode": "Markdown"}
// 	data, _ := json.Marshal(payload)
// 	http.Post(fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.Token), "application/json", bytes.NewBuffer(data))
// }

// // --- API Structs ---
// type Response struct {
// 	Ok     bool     `json:"ok"`
// 	Result []Update `json:"result"`
// }
// type Update struct {
// 	UpdateID int     `json:"update_id"`
// 	Message  Message `json:"message"`
// }
// type Message struct {
// 	Text string `json:"text"`
// 	Chat Chat   `json:"chat"`
// }
// type Chat struct {
// 	ID       int64  `json:"id"`
// 	Username string `json:"username"`
// }

//-----------------------------------------------------

package alert

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type TelegramBot struct {
	Bot       *tgbotapi.BotAPI
	ChatID    int64
	ClientSet *kubernetes.Clientset
}

func NewTelegramBot(token string, chatID int64, clientSet *kubernetes.Clientset) *TelegramBot {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil
	}
	return &TelegramBot{Bot: bot, ChatID: chatID, ClientSet: clientSet}
}

func (t *TelegramBot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := t.Bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Auto-detect ChatID on first contact
		if t.ChatID == 0 {
			t.ChatID = update.Message.Chat.ID
			t.send("👋 KubeSolv Connected! I'm listening. Try asking 'How are my pods?'")
		}

		// --- NATURAL LANGUAGE ROUTER ---
		msg := strings.ToLower(update.Message.Text)
		t.routeMessage(msg)
	}
}

func (t *TelegramBot) routeMessage(msg string) {
	// 1. Status / Health Check
	if strings.Contains(msg, "status") || strings.Contains(msg, "health") || strings.Contains(msg, "how are") || strings.Contains(msg, "overview") {
		t.handleStatus()
		return
	}

	// 2. Pod Listing / Debugging
	if strings.Contains(msg, "pods") || strings.Contains(msg, "list") || strings.Contains(msg, "broken") || strings.Contains(msg, "crash") {
		t.handleGetPods()
		return
	}

	// 3. Greetings / Help
	if strings.Contains(msg, "hi") || strings.Contains(msg, "hello") || strings.Contains(msg, "help") || strings.Contains(msg, "start") {
		t.send("🤖 *I am KubeSolv.*\n\nYou can ask me things like:\n" +
			"• \"How is the cluster status?\"\n" +
			"• \"List all my pods\"\n" +
			"• \"Show me broken apps\"\n" +
			"• \"Health check\"")
		return
	}

	// 4. Default Fallback
	t.send("🤔 I didn't catch that. Try asking about *status* or *pods*.")
}

func (t *TelegramBot) handleStatus() {
	if t.ClientSet == nil {
		t.send("⚠️ No Cluster Access Configured.")
		return
	}

	// Fetch ALL pods from ALL namespaces
	pods, err := t.ClientSet.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.send(fmt.Sprintf("❌ Error fetching status: %v", err))
		return
	}

	// Counters for User Apps
	uRun, uFail, uPend := 0, 0, 0
	// Counters for System Infra
	sRun, sFail, sPend := 0, 0, 0

	for _, p := range pods.Items {
		// Define what counts as "System"
		// You can add more namespaces here if you have them (e.g., "monitoring")
		isSystem := (p.Namespace == "kube-system" ||
			p.Namespace == "local-path-storage" ||
			p.Namespace == "ingress-nginx")

		switch p.Status.Phase {
		case "Running":
			if isSystem {
				sRun++
			} else {
				uRun++
			}
		case "Failed", "Unknown":
			if isSystem {
				sFail++
			} else {
				uFail++
			}
		case "Pending":
			if isSystem {
				sPend++
			} else {
				uPend++
			}
		}
	}

	// Determine Health State
	emoji := "🟢"
	summaryText := "✨ *Cluster is Healthy*"

	if uFail > 0 || uPend > 0 {
		emoji = "⚠️"
		summaryText = "🔥 *Your Apps need attention.*"
	} else if sFail > 0 {
		emoji = "🔧"
		summaryText = "⚠️ *System Infrastructure is unstable.*"
	}

	// Format Message
	msg := fmt.Sprintf("%s *Cluster Health Report*\n\n"+
		"👤 *User Apps (Default)*\n"+
		"✅ Running: *%d*\n"+
		"❌ Failed: *%d*\n"+
		"⏳ Pending: *%d*\n\n"+
		"⚙️ *System Infra (Kube-System)*\n"+
		"✅ Running: *%d*\n"+
		"❌ Failed: *%d*\n"+
		"⏳ Pending: *%d*\n\n"+
		"%s",
		emoji, uRun, uFail, uPend, sRun, sFail, sPend, summaryText)

	t.send(msg)
}

func (t *TelegramBot) handleGetPods() {
	if t.ClientSet == nil {
		return
	}

	// Get ALL pods in default namespace
	pods, err := t.ClientSet.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.send("❌ Error fetching pods.")
		return
	}

	var sb strings.Builder
	sb.WriteString("📦 *Pod Status (Default NS):*\n\n")

	hasIssues := false
	for _, p := range pods.Items {
		icon := "🟢"
		statusText := string(p.Status.Phase)

		// Check container statuses for deeper issues (CrashLoop, ImagePull)
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				icon = "🔴"
				statusText = cs.State.Waiting.Reason // e.g., CrashLoopBackOff
				hasIssues = true
			} else if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
				icon = "🔴"
				statusText = "Error"
				hasIssues = true
			}
		}

		// Formatting: Bold the broken ones
		if icon == "🔴" {
			sb.WriteString(fmt.Sprintf("%s *%s*\n   └ _%s_\n", icon, p.Name, statusText))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s\n", icon, p.Name))
		}
	}

	if !hasIssues {
		sb.WriteString("\n✨ All systems operational.")
	}

	t.send(sb.String())
}

// Broadcast is used by the Controller to push alerts
func (t *TelegramBot) Broadcast(source, namespace, reason, message string) {
	if t.ChatID == 0 {
		return
	}
	// We ignore source/namespace args in the message body for cleaner alerts,
	// relying on the formatted message passed in.
	t.send(fmt.Sprintf("🚨 *Alert: %s*\n\n%s", reason, message))
}

func (t *TelegramBot) send(text string) {
	if t.ChatID == 0 {
		return
	}
	msg := tgbotapi.NewMessage(t.ChatID, text)
	msg.ParseMode = "Markdown"
	t.Bot.Send(msg)
}
