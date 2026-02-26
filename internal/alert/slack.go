// package alert

// import (
// 	"context"
// 	"fmt"
// 	"log"
// 	"os"
// 	"regexp"
// 	"strings"

// 	"github.com/slack-go/slack"
// 	"github.com/slack-go/slack/slackevents"
// 	"github.com/slack-go/slack/socketmode"
// 	"kubesolv/internal/ops"
// )

// type SlackBot struct {
// 	Client    *slack.Client
// 	Socket    *socketmode.Client
// 	Ops       *ops.OpsManager
// 	ChannelID string
// }

// func NewSlackBot(appToken, botToken, channelID string, opsManager *ops.OpsManager) *SlackBot {
// 	if appToken == "" || botToken == "" {
// 		return nil
// 	}

// 	api := slack.New(botToken, slack.OptionAppLevelToken(appToken), slack.OptionLog(log.New(os.Stdout, "slack: ", log.Lshortfile|log.LstdFlags)))
// 	socket := socketmode.New(api)

// 	return &SlackBot{
// 		Client:    api,
// 		Socket:    socket,
// 		Ops:       opsManager,
// 		ChannelID: channelID,
// 	}
// }

// func (s *SlackBot) Start() {
// 	go func() {
// 		for evt := range s.Socket.Events {
// 			switch evt.Type {
// 			case socketmode.EventTypeEventsAPI:
// 				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
// 				if !ok {
// 					continue
// 				}
// 				s.Socket.Ack(*evt.Request)

// 				if eventsAPIEvent.Type == slackevents.CallbackEvent {
// 					innerEvent := eventsAPIEvent.InnerEvent
// 					switch ev := innerEvent.Data.(type) {
// 					case *slackevents.MessageEvent:
// 						if ev.BotID != "" || ev.ChannelType != "im" {
// 							continue
// 						}
// 						s.routeMessage(ev.Channel, ev.Text)
// 					case *slackevents.AppMentionEvent:
// 						s.routeMessage(ev.Channel, ev.Text)
// 					}
// 				}
// 			case socketmode.EventTypeInteractive:
// 				s.Socket.Ack(*evt.Request)
// 				interaction, ok := evt.Data.(slack.InteractionCallback)
// 				if !ok {
// 					continue
// 				}
// 				if len(interaction.ActionCallback.BlockActions) > 0 {
// 					action := interaction.ActionCallback.BlockActions[0]
// 					s.handleInteraction(action.ActionID, interaction.Channel.ID, interaction.Message.Timestamp)
// 				}
// 			}
// 		}
// 	}()
// 	s.Socket.Run()
// }

// func cleanSlackMessage(msg string) string {
// 	text := regexp.MustCompile(`<@[^>]+>`).ReplaceAllString(msg, "")
// 	return strings.TrimSpace(strings.ToLower(text))
// }

// func (s *SlackBot) routeMessage(channelID, msg string) {
// 	cleanMsg := cleanSlackMessage(msg)
// 	parts := strings.Fields(cleanMsg)

// 	if len(parts) == 0 {
// 		return
// 	}

// 	cmd := parts[0]

// 	if cmd == "status" || cmd == "health" || cmd == "list" {
// 		s.send(channelID, s.Ops.GetHealthReport())
// 	} else if cmd == "metrics" || cmd == "usage" || cmd == "cpu" {
// 		s.send(channelID, s.Ops.GetResourceUsage())
// 	} else if cmd == "events" || cmd == "warn" {
// 		s.send(channelID, s.Ops.GetRecentEvents())
// 	} else if cmd == "logs" {
// 		if len(parts) > 1 {
// 			podName := parts[1]
// 			logs := s.Ops.GetLogs(podName)
// 			if logs == "" || strings.Contains(logs, "Error") {
// 				s.send(channelID, fmt.Sprintf("❌ Could not fetch logs for `%s`. Verify the pod name.", podName))
// 				return
// 			}

// 			formattedLogs := fmt.Sprintf("📜 *Logs for %s:*\n```\n%s\n```", podName, logs)
// 			s.send(channelID, formattedLogs)
// 		} else {
// 			s.send(channelID, "⚠️ Usage: `@KubeSolv logs <pod-name>`")
// 		}
// 	} else if cmd == "hi" || cmd == "help" {
// 		s.send(channelID, "👋 *KubeSolv Ready.*\nCommands: `status`, `metrics`, `events`, `logs <pod>`")
// 	}
// }

// func (s *SlackBot) Broadcast(title, message string) error {
// 	if s.ChannelID == "" {
// 		return nil
// 	}
// 	_, _, err := s.Client.PostMessage(s.ChannelID, slack.MsgOptionText(fmt.Sprintf("🚨 *%s*\n%s", title, message), false))
// 	return err
// }

// func (s *SlackBot) send(channelID string, text string) {
// 	if len(text) > 2900 {
// 		text = text[:2900] + "\n...[truncated]"
// 		if strings.Contains(text, "```") && !strings.HasSuffix(text, "```") {
// 			text += "\n```"
// 		}
// 	}
// 	_, _, _ = s.Client.PostMessage(channelID, slack.MsgOptionText(text, false))
// }

// func (s *SlackBot) BroadcastWithAction(title, message, actionID, buttonText string) error {
// 	if s.ChannelID == "" {
// 		return nil
// 	}
// 	headerText := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("🚨 *%s*\n%s", title, message), false, false)
// 	headerSection := slack.NewSectionBlock(headerText, nil, nil)

// 	btnText := slack.NewTextBlockObject("plain_text", buttonText, false, false)
// 	btn := slack.NewButtonBlockElement(actionID, "click", btnText)
// 	actionBlock := slack.NewActionBlock("action_block", btn)

// 	_, _, err := s.Client.PostMessage(s.ChannelID, slack.MsgOptionBlocks(headerSection, actionBlock))
// 	return err
// }

// func (s *SlackBot) handleInteraction(actionID string, channelID string, triggerID string) {
// 	parts := strings.Split(actionID, "|")
// 	if len(parts) == 5 && parts[0] == "patch_mem" {
// 		namespace, deployName, containerName, newLimit := parts[1], parts[2], parts[3], parts[4]
// 		if err := s.Ops.PatchMemoryLimit(namespace, deployName, containerName, newLimit); err == nil {
// 			msg := fmt.Sprintf("✅ Approved: Increased memory to %s for %s.", newLimit, deployName)
// 			s.Broadcast("Action Approved", msg)
// 			CrossPlatformSync <- "Slack User Approved: Patch Memory for " + deployName
// 		}
// 	} else if len(parts) == 3 && parts[0] == "rollback" {
// 		namespace, deployName := parts[1], parts[2]
// 		if err := s.Ops.RollbackDeployment(namespace, deployName); err == nil {
// 			msg := fmt.Sprintf("⏪ Approved: Initiated Rollback for %s.", deployName)
// 			s.Broadcast("Action Approved", msg)
// 			CrossPlatformSync <- "Slack User Approved: Rollback for " + deployName
// 		}
// 	} else if len(parts) == 2 && parts[0] == "cordon_node" {
// 		nodeName := parts[1]
// 		// Using a background context for the ops call
// 		if err := s.Ops.CordonNode(context.Background(), nodeName); err == nil {
// 			msg := fmt.Sprintf("🚧 Approved: Node `%s` cordoned (marked unschedulable).", nodeName)
// 			s.Broadcast("Action Approved", msg)
// 			CrossPlatformSync <- "Slack User Approved: Cordon Node " + nodeName
// 		}
// 	}
// }
//---------------------------------------

package alert

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"kubesolv/internal/ai"
	"kubesolv/internal/ops"
)

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
	s.Socket.Run()
}

func cleanSlackMessage(msg string) string {
	text := regexp.MustCompile(`<@[^>]+>`).ReplaceAllString(msg, "")
	return strings.TrimSpace(text)
}

func (s *SlackBot) routeMessage(channelID, msg string) {
	cleanMsg := cleanSlackMessage(msg)

	intent, err := s.AI.ProcessCommand(context.Background(), channelID, cleanMsg)
	if err != nil {
		s.send(channelID, fmt.Sprintf("⚠️ Error analyzing request: %v", err))
		return
	}

	switch intent.Action {
	case "status":
		s.send(channelID, s.Ops.GetHealthReport())
	case "metrics":
		s.send(channelID, s.Ops.GetResourceUsage())
	case "events":
		s.send(channelID, s.Ops.GetRecentEvents())
	case "logs":
		logs := s.Ops.GetLogs(intent.Target)
		if logs == "" || strings.Contains(logs, "Error") {
			s.send(channelID, fmt.Sprintf("❌ Could not fetch logs for `%s`.", intent.Target))
			return
		}
		s.send(channelID, fmt.Sprintf("📜 *Logs for %s:*\n```\n%s\n```", intent.Target, logs))
	case "scale":
		err := s.Ops.ScaleDeployment(context.Background(), intent.Namespace, intent.Target, intent.Value)
		if err != nil {
			s.send(channelID, fmt.Sprintf("❌ Failed to scale: %v", err))
			return
		}
		s.send(channelID, fmt.Sprintf("✅ Executed: Scaled `%s` to %s replicas.\n🤖 %s", intent.Target, intent.Value, intent.Reply))
	default:
		s.send(channelID, intent.Reply)
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

func (s *SlackBot) handleInteraction(actionID string, channelID string, triggerID string) {
	parts := strings.Split(actionID, "|")
	if len(parts) == 5 && parts[0] == "patch_mem" {
		namespace, deployName, containerName, newLimit := parts[1], parts[2], parts[3], parts[4]
		if err := s.Ops.PatchMemoryLimit(namespace, deployName, containerName, newLimit); err == nil {
			msg := fmt.Sprintf("✅ Approved: Increased memory to %s for %s.", newLimit, deployName)
			s.Broadcast("Action Approved", msg)
			CrossPlatformSync <- "Slack User Approved: Patch Memory for " + deployName
		}
	} else if len(parts) == 3 && parts[0] == "rollback" {
		namespace, deployName := parts[1], parts[2]
		if err := s.Ops.RollbackDeployment(namespace, deployName); err == nil {
			msg := fmt.Sprintf("⏪ Approved: Initiated Rollback for %s.", deployName)
			s.Broadcast("Action Approved", msg)
			CrossPlatformSync <- "Slack User Approved: Rollback for " + deployName
		}
	} else if len(parts) == 2 && parts[0] == "cordon_node" {
		nodeName := parts[1]
		if err := s.Ops.CordonNode(context.Background(), nodeName); err == nil {
			msg := fmt.Sprintf("🚧 Approved: Node `%s` cordoned (marked unschedulable).", nodeName)
			s.Broadcast("Action Approved", msg)
			CrossPlatformSync <- "Slack User Approved: Cordon Node " + nodeName
		}
	} else if len(parts) == 4 && parts[0] == "scale" {
		namespace, deployName, replicas := parts[1], parts[2], parts[3]
		if err := s.Ops.ScaleDeployment(context.Background(), namespace, deployName, replicas); err == nil {
			msg := fmt.Sprintf("✅ Executed: Scaled `%s` to %s replica(s) for cost optimization.", deployName, replicas)
			s.Broadcast("Action Approved", msg)
			CrossPlatformSync <- "Slack User Approved: Scale " + deployName + " to " + replicas
		} else {
			s.Broadcast("Action Failed", fmt.Sprintf("❌ Failed to scale `%s`: %v", deployName, err))
		}
	}
}
