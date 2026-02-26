# 🤖 KubeSolv Autonomous AI Operator

KubeSolv is a next-generation **Autonomous AI Site Reliability Engineer (SRE)** for your Kubernetes clusters. 

Built using the Kubebuilder framework and powered by Google's **Gemini AI**, KubeSolv doesn't just read your cluster state—it actively manages, remediates, and safeguards it. It integrates directly with your team's Slack or Telegram channels, allowing you to troubleshoot production incidents using natural language.

---

## ✨ Key Features

1. **Native ChatOps Integration**: Talk to your cluster over Slack or Telegram. ("Why is the frontend crashing?", "Are there any node pressure warnings?")
2. **AI Function Calling**: KubeSolv uses native LLM function execution to autonomously fetch health reports, analyze container logs, and pull pod details.
3. **Automated Remediations (SRE Tools)**:
   - Scale deployments dynamically
   - Restart failing rollout sequences
   - Force-delete stuck pods
   - Cordon/Uncordon stressed nodes
4. **Human-in-the-Loop Guardrails 🛡️**: High-privilege operations (like scaling or deleting) are never executed blindly. KubeSolv pauses the agent loop and sends **Interactive Approval Buttons** to your chat. Operations only execute when a human clicks "Approve."

---

## 🚀 Getting Started (Local Testing)

If you want to test KubeSolv locally against your current `~/.kube/config` context:

### Prerequisites
- Go v1.22+
- Access to a Kubernetes cluster (e.g., Docker Desktop, Minikube, or EKS/GKE)
- API Keys for Gemini, Slack, and/or Telegram

### 1. Export Secrets
```sh
export GEMINI_API_KEY="your-gemini-key"
export TELEGRAM_BOT_TOKEN="your-telegram-bot-token"
export SLACK_BOT_TOKEN="xoxb-your-slack-bot-token"
export SLACK_APP_TOKEN="xapp-your-slack-app-token"
export SLACK_CHANNEL_ID="your-slack-channel"
```

### 2. Run the Operator
```sh
make run
```
The operator will boot and begin listening to your messaging platforms. Send a message to the bot (e.g., *"What is the health of the default namespace?"*) to watch the AI orchestrate!

---

## 🌍 Deploying to Production (In-Cluster)

For production environments, KubeSolv is packaged as a standard Kubernetes Operator deployment. Since it connects to external LLMs and Chat APIs, you must securely manage your API keys using Kubernetes `Secrets`.

> **For a detailed, non-technical walkthrough on how to securely deploy KubeSolv to production via Kubernetes Manifests, please read the [USER_GUIDE.md](./USER_GUIDE.md).**

---

## 🛠️ Development

**Build the Docker Image:**
```sh
make docker-build docker-push IMG=<your-registry>/kubesolv:latest
```

**Install CRDs and Deploy:**
```sh
make install 
make deploy IMG=<your-registry>/kubesolv:latest
```

**Run Unit Tests:**
```sh
make test
```

## License
Licensed under the Apache License, Version 2.0.
