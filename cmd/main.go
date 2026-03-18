package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	opsv1 "kubesolv/api/v1"
	"kubesolv/internal/ai"
	"kubesolv/internal/alert"
	"kubesolv/internal/controller"
	"kubesolv/internal/metrics"
	"kubesolv/internal/ops"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(opsv1.AddToScheme(scheme))
}

// getEnv reads an environment variable with a fallback default.
// In Kubernetes, these are injected from the kubesolv-credentials Secret via envFrom.
// For local development, export them in your shell or use a .env loader.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "")
	flag.BoolVar(&secureMetrics, "metrics-secure", true, "")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "", "")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// ── Startup Banner ──
	setupLog.Info("Starting KubeSolv — Autonomous AI SRE Operator")
	setupLog.Info("Secrets are loaded from environment variables")
	setupLog.Info("In Kubernetes: set via envFrom on the kubesolv-credentials Secret")
	setupLog.Info("Locally: export variables in your shell before running")

	disableHTTP2 := func(c *tls.Config) {
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	if len(metricsCertPath) > 0 {
		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "f4e6726c.kubesolv.io",
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	// ── AI Client (Gemini or Proxy) ──
	var aiClient *ai.Client
	licenseKey := getEnv("LICENSE_KEY", "")
	aiEndpoint := getEnv("AI_ENDPOINT", "")

	if licenseKey != "" && aiEndpoint != "" {
		// Proxy mode: operator talks to backend, backend talks to Gemini
		aiClient = ai.NewProxyClient(aiEndpoint, licenseKey)
		setupLog.Info("✅ AI client initialized in proxy mode", "endpoint", aiEndpoint)
	} else {
		// Direct mode: operator talks to Gemini directly with API key
		apiKey := getEnv("GEMINI_API_KEY", "")
		if apiKey != "" {
			aiClient, err = ai.NewClient(apiKey)
			if err != nil {
				setupLog.Error(err, "Failed to initialize Gemini AI client")
				aiClient = nil
			} else {
				setupLog.Info("✅ Gemini AI client initialized")
			}
		} else {
			setupLog.Info("⚠️  GEMINI_API_KEY not set — AI analysis disabled")
		}
	}

	// ── Kubernetes Clients ──
	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}

	metricsClient, _ := metricsv.NewForConfig(mgr.GetConfig())

	opsManager := ops.NewManager(kubeClient, metricsClient)

	// ── Telegram Bot ──
	var telegramBot *alert.TelegramBot
	botToken := getEnv("TELEGRAM_BOT_TOKEN", "")
	if botToken != "" {
		var telegramChatID int64
		if chatIDStr := getEnv("TELEGRAM_CHAT_ID", ""); chatIDStr != "" {
			telegramChatID, _ = strconv.ParseInt(chatIDStr, 10, 64)
		}
		telegramBot = alert.NewTelegramBot(botToken, telegramChatID, opsManager, aiClient)
		if telegramBot != nil {
			telegramBot.SetKubeClient(kubeClient)
			go telegramBot.Start()
			setupLog.Info("✅ Telegram bot started", "adminChatID", telegramChatID)
		}
	} else {
		setupLog.Info("⚠️  TELEGRAM_BOT_TOKEN not set — Telegram integration disabled")
	}

	// ── Slack Bot ──
	var slackBot *alert.SlackBot
	slackAppToken := getEnv("SLACK_APP_TOKEN", "")
	slackBotToken := getEnv("SLACK_BOT_TOKEN", "")
	slackChannelID := getEnv("SLACK_CHANNEL_ID", "")
	if slackAppToken != "" && slackBotToken != "" && slackChannelID != "" {
		slackBot = alert.NewSlackBot(slackAppToken, slackBotToken, slackChannelID, opsManager, aiClient)
		if slackBot != nil {
			go slackBot.Start()
			setupLog.Info("✅ Slack bot started", "channel", slackChannelID)
		}
	} else {
		missing := []string{}
		if slackAppToken == "" {
			missing = append(missing, "SLACK_APP_TOKEN")
		}
		if slackBotToken == "" {
			missing = append(missing, "SLACK_BOT_TOKEN")
		}
		if slackChannelID == "" {
			missing = append(missing, "SLACK_CHANNEL_ID")
		}
		setupLog.Info(fmt.Sprintf("⚠️  Slack integration disabled — missing: %s", strings.Join(missing, ", ")))
	}

	// ── Prometheus Client (with auto-discovery) ──
	var promClient *metrics.PrometheusClient
	promURL := metrics.AutoDiscoverPrometheus(context.Background(), kubeClient)
	if promURL != "" {
		promClient, err = metrics.NewPrometheusClient(promURL)
		if err != nil {
			setupLog.Info("⚠️  Prometheus client failed — cost optimization disabled", "error", err)
			promClient = nil
		} else {
			setupLog.Info("✅ Prometheus client initialized", "url", promURL)
		}
	} else {
		setupLog.Info("⚠️  Prometheus not found — cost optimization and CPU-based scaling disabled")
	}

	// ── Integration Status Summary ──
	setupLog.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	setupLog.Info("KubeSolv Integration Status:")
	logStatus("Gemini AI", aiClient != nil)
	logStatus("Slack ChatOps", slackBot != nil)
	logStatus("Telegram ChatOps", telegramBot != nil)
	logStatus("Prometheus Metrics", promClient != nil)
	setupLog.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// ── Controllers ──
	if err := (&controller.KubeSolvConfigReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		AI:         aiClient,
		ClientSet:  kubeClient,
		Telegram:   telegramBot,
		Slack:      slackBot,
		Prometheus: promClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "KubeSolvConfig")
		os.Exit(1)
	}

	if err := (&controller.NodeReconciler{
		Client:   mgr.GetClient(),
		Telegram: telegramBot,
		Slack:    slackBot,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "NodeReconciler")
		os.Exit(1)
	}

	// ── Cross-Platform Sync ──
	go func() {
		for msg := range alert.CrossPlatformSync {
			if telegramBot != nil && strings.HasPrefix(msg, "Slack") {
				telegramBot.Broadcast("cluster", "🔄 Cross-Platform Sync", msg)
			}
			if slackBot != nil && strings.HasPrefix(msg, "Telegram") {
				_ = slackBot.Broadcast("🔄 Cross-Platform Sync", msg)
			}
		}
	}()

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		os.Exit(1)
	}

	setupLog.Info("KubeSolv operator is ready — starting reconciliation loop")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Problem running manager")
		os.Exit(1)
	}
}

func logStatus(name string, active bool) {
	if active {
		setupLog.Info(fmt.Sprintf("  ✅ %s: ACTIVE", name))
	} else {
		setupLog.Info(fmt.Sprintf("  ⚠️  %s: DISABLED", name))
	}
}
