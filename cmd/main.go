package main

import (
	"crypto/tls"
	"flag"
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

	"github.com/joho/godotenv"
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

func main() {
	_ = godotenv.Load()

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
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	aiClient, _ := ai.NewClient(apiKey)

	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		os.Exit(1)
	}

	metricsClient, _ := metricsv.NewForConfig(mgr.GetConfig())

	opsManager := ops.NewManager(kubeClient, metricsClient)

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	var telegramBot *alert.TelegramBot

	if botToken != "" {
		var telegramChatID int64
		if chatIDStr := os.Getenv("TELEGRAM_CHAT_ID"); chatIDStr != "" {
			telegramChatID, _ = strconv.ParseInt(chatIDStr, 10, 64)
		}
		telegramBot = alert.NewTelegramBot(botToken, telegramChatID, opsManager, aiClient)
		if telegramBot != nil {
			go telegramBot.Start()
		}
	}

	slackAppToken := os.Getenv("SLACK_APP_TOKEN")
	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")
	slackChannelID := os.Getenv("SLACK_CHANNEL_ID")
	var slackBot *alert.SlackBot

	if slackAppToken != "" && slackBotToken != "" && slackChannelID != "" {
		slackBot = alert.NewSlackBot(slackAppToken, slackBotToken, slackChannelID, opsManager, aiClient)
		if slackBot != nil {
			go slackBot.Start()
		}
	}

	promURL := os.Getenv("PROMETHEUS_URL")
	if promURL == "" {
		promURL = "http://localhost:9090"
	}

	promClient, err := metrics.NewPrometheusClient(promURL)
	if err != nil {
		setupLog.Info("⚠️ Failed to initialize Prometheus client. Cost optimization disabled.", "error", err)
		promClient = nil
	}

	if err := (&controller.KubeSolvConfigReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		AI:         aiClient,
		ClientSet:  kubeClient,
		Telegram:   telegramBot,
		Slack:      slackBot,
		Prometheus: promClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KubeSolvConfig")
		os.Exit(1)
	}

	if err := (&controller.NodeReconciler{
		Client:   mgr.GetClient(),
		Telegram: telegramBot,
		Slack:    slackBot,
	}).SetupWithManager(mgr); err != nil {
		os.Exit(1)
	}

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

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		os.Exit(1)
	}
}
