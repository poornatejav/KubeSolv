// package alert

// import (
// 	"bytes"
// 	"encoding/json"
// 	"fmt"
// 	"net/http"
// )

// type SlackBot struct {
// 	WebhookURL string
// }

// func NewSlackBot(webhookURL string) *SlackBot {
// 	if webhookURL == "" {
// 		return nil
// 	}
// 	return &SlackBot{WebhookURL: webhookURL}
// }

// func (s *SlackBot) Broadcast(title, message string) error {
// 	// Format the message for Slack
// 	// We use a simple "Block" layout for better visibility
// 	payload := map[string]interface{}{
// 		"text": fmt.Sprintf("🚨 *%s*\n%s", title, message),
// 	}

// 	data, _ := json.Marshal(payload)
// 	resp, err := http.Post(s.WebhookURL, "application/json", bytes.NewBuffer(data))

// 	if err != nil {
// 		return err
// 	}
// 	if resp.StatusCode != 200 {
// 		return fmt.Errorf("slack api error: %d", resp.StatusCode)
// 	}
// 	return nil
// }
//-----------------------------------------------------

package alert

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type SlackBot struct {
	Client    *slack.Client
	Socket    *socketmode.Client
	ClientSet *kubernetes.Clientset
	ChannelID string // Default channel for alerts
}

func NewSlackBot(appToken, botToken string, clientSet *kubernetes.Clientset) *SlackBot {
	if appToken == "" || botToken == "" {
		return nil
	}

	// Create Slack Client with Socket Mode
	api := slack.New(
		botToken,
		slack.OptionAppLevelToken(appToken),
		// slack.OptionDebug(true), // Uncomment to debug connection issues
		slack.OptionLog(log.New(os.Stdout, "slack-bot: ", log.Lshortfile|log.LstdFlags)),
	)

	socketClient := socketmode.New(
		api,
		socketmode.OptionLog(log.New(os.Stdout, "socket-mode: ", log.Lshortfile|log.LstdFlags)),
	)

	return &SlackBot{
		Client:    api,
		Socket:    socketClient,
		ClientSet: clientSet,
	}
}

// Start Listening (Blocking Loop)
func (s *SlackBot) Start() {
	go func() {
		for evt := range s.Socket.Events {
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				fmt.Println("⚡ Slack connecting...")
			case socketmode.EventTypeConnectionError:
				fmt.Println("❌ Slack connection failed. Retrying...")
			case socketmode.EventTypeConnected:
				fmt.Println("✅ Slack Connected via Socket Mode!")

			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}

				s.Socket.Ack(*evt.Request)

				switch eventsAPIEvent.Type {
				case slackevents.CallbackEvent:
					innerEvent := eventsAPIEvent.InnerEvent
					switch ev := innerEvent.Data.(type) {

					// Case A: Regular Messages
					case *slackevents.MessageEvent:
						// 1. Ignore bot's own messages
						if ev.BotID != "" {
							continue
						}

						// 2. CRITICAL FIX: Ignore messages in Public Channels
						// We allow AppMentionEvent to handle public channels.
						// We only handle MessageEvent if it is a DM ("im").
						if ev.ChannelType != "im" {
							continue
						}

						s.routeMessage(ev.Channel, ev.Text)
						if s.ChannelID == "" {
							s.ChannelID = ev.Channel
						}

					// Case B: Mentions (@KubeSolv)
					case *slackevents.AppMentionEvent:
						// Always handle mentions (Public or Private)
						s.routeMessage(ev.Channel, ev.Text)
						if s.ChannelID == "" {
							s.ChannelID = ev.Channel
						}
					}
				}
			}
		}
	}()

	s.Socket.Run()
}

// --- NATURAL LANGUAGE ROUTER (Same as Telegram) ---
func (s *SlackBot) routeMessage(channelID, msg string) {
	msg = strings.ToLower(msg)

	// 1. Status Check
	if strings.Contains(msg, "status") || strings.Contains(msg, "health") || strings.Contains(msg, "overview") {
		s.handleStatus(channelID)
		return
	}

	// 2. Pod Listing
	if strings.Contains(msg, "pods") || strings.Contains(msg, "list") || strings.Contains(msg, "crash") {
		s.handleGetPods(channelID)
		return
	}

	// 3. Greeting
	if strings.Contains(msg, "hi") || strings.Contains(msg, "hello") {
		s.send(channelID, "👋 *I am KubeSolv.*\nTry asking: _\"How is the cluster?\"_ or _\"List pods\"_")
		return
	}

	if strings.Contains(msg, "status") || strings.Contains(msg, "health") || strings.Contains(msg, "overview") || strings.Contains(msg, "issues") {
		s.handleStatus(channelID)
		return
	}
}

func (s *SlackBot) handleStatus(channelID string) {
	if s.ClientSet == nil {
		s.send(channelID, "⚠️ No Cluster Access.")
		return
	}

	// Fetch ALL pods from ALL namespaces
	pods, err := s.ClientSet.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		s.send(channelID, "❌ Error fetching pods.")
		return
	}

	// Counters for User Apps
	uRun, uFail, uPend := 0, 0, 0
	// Counters for System Infra
	sRun, sFail, sPend := 0, 0, 0

	for _, p := range pods.Items {
		// Define what counts as "System"
		isSystem := (p.Namespace == "kube-system" ||
			p.Namespace == "local-path-storage" ||
			p.Namespace == "ingress-nginx" ||
			p.Namespace == "monitoring")

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

	// Determine Health State (Based mostly on User Apps)
	emoji := "🟢"
	summaryText := "✨ *Cluster is Healthy*"

	if uFail > 0 || uPend > 0 {
		emoji = "⚠️"
		summaryText = "🔥 *Your Apps need attention.*"
	} else if sFail > 0 {
		emoji = "🔧"
		summaryText = "⚠️ *System Infrastructure is unstable.*"
	}

	// Format Message with Two Sections
	msg := fmt.Sprintf("%s *Cluster Health Report*\n\n"+
		"👤 *User Apps (Default/Custom)*\n"+
		"   ✅ Running: *%d* ❌ Failed: *%d* ⏳ Pending: *%d*\n\n"+
		"⚙️ *System Infra (Kube-System)*\n"+
		"   ✅ Running: *%d* ❌ Failed: *%d* ⏳ Pending: *%d*\n\n"+
		"%s",
		emoji, uRun, uFail, uPend, sRun, sFail, sPend, summaryText)

	s.send(channelID, msg)
}

func (s *SlackBot) handleGetPods(channelID string) {
	if s.ClientSet == nil {
		return
	}
	pods, _ := s.ClientSet.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{})

	var sb strings.Builder
	sb.WriteString("📦 *Pod Status (Default NS):*\n\n")

	hasIssues := false
	for _, p := range pods.Items {
		icon := "🟢"
		statusText := string(p.Status.Phase)

		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				icon = "🔴"
				statusText = cs.State.Waiting.Reason
				hasIssues = true
			} else if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
				icon = "🔴"
				statusText = "Error"
				hasIssues = true
			}
		}

		if icon == "🔴" {
			// Bold the broken ones
			sb.WriteString(fmt.Sprintf("%s *%s*\n\t└ _%s_\n", icon, p.Name, statusText))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s\n", icon, p.Name))
		}
	}

	if !hasIssues {
		sb.WriteString("\n✨ All systems operational.")
	}

	s.send(channelID, sb.String())
}

// Broadcast is called by the Controller to alert the user
func (s *SlackBot) Broadcast(title, message string) error {
	// If we haven't talked yet, we can't broadcast (Socket mode limitation compared to Webhook)
	// PRO TIP: Hardcode a channel ID here if you want alerts before you say "Hi"
	if s.ChannelID == "" {
		fmt.Println("⚠️ Slack Alert skipped: No channel known yet. Say 'Hi' to the bot first.")
		return nil
	}

	// Prettify Alert Block
	_, _, err := s.Client.PostMessage(s.ChannelID,
		slack.MsgOptionText(fmt.Sprintf("🚨 *%s*\n%s", title, message), false),
	)
	return err
}

func (s *SlackBot) send(channelID, text string) {
	s.Client.PostMessage(channelID, slack.MsgOptionText(text, false))
}
