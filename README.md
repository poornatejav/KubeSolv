# 🤖 KubeSolv — Autonomous AI SRE Operator

[![CI Pipeline](https://github.com/poornatejav/KubeSolv/actions/workflows/ci.yml/badge.svg)](https://github.com/poornatejav/KubeSolv/actions/workflows/ci.yml)
[![Release Pipeline](https://github.com/poornatejav/KubeSolv/actions/workflows/release.yml/badge.svg)](https://github.com/poornatejav/KubeSolv/actions/workflows/release.yml)
[![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-green.svg)](LICENSE)

KubeSolv is a next-generation **Autonomous AI Site Reliability Engineer (SRE)** for Kubernetes clusters.

Built on the Kubebuilder framework and powered by Google's **Gemini AI**, KubeSolv continuously monitors your cluster, performs AI-driven root cause analysis, and autonomously remediates incidents — with human-in-the-loop guardrails via **Slack** and **Telegram**.

> 📘 **For a detailed product overview, architecture, and use cases, see [PRODUCT.md](./PRODUCT.md).**

---

## ✨ Key Features

- **🤖 AI Root Cause Analysis** — Gemini AI analyzes container logs and cluster state to diagnose issues
- **🔄 Autonomous Remediation** — Rollbacks, scaling, memory patching, network policy enforcement
- **🛡️ Human-in-the-Loop** — Interactive approval buttons for all privileged operations
- **💬 ChatOps** — Natural language conversations via Slack and Telegram
- **📈 Smart Scaling** — Traffic-based horizontal autoscaling using log volume analysis
- **💰 Cost Optimization** — AI-driven analysis of over-provisioned deployments
- **🔒 Security Auto-Heal** — Default-deny network policies for unsecured namespaces

---

## 🚀 Quick Start

### Prerequisites
- Go 1.24+
- Kubernetes cluster (Docker Desktop, Minikube, EKS, GKE, etc.)
- API keys: Gemini AI, Slack (optional), Telegram (optional)

### Local Development
```bash
# Export API keys
export GEMINI_API_KEY="your-key"
export SLACK_BOT_TOKEN="xoxb-your-token"
export SLACK_APP_TOKEN="xapp-your-token"
export SLACK_CHANNEL_ID="your-channel"
export TELEGRAM_BOT_TOKEN="your-telegram-token"

# Run the operator
make run
```

### Production Deployment
```bash
# Install CRDs and deploy operator
make install
make deploy IMG=ghcr.io/your-org/kubesolv:latest
```

> 📘 See [USER_GUIDE.md](./USER_GUIDE.md) for complete production deployment instructions.

---

## 🧪 Testing

```bash
# Run all unit tests
go test ./... -race -v

# Run with coverage
go test ./... -race -coverprofile=cover.out
go tool cover -func=cover.out
```

---

## 🏗️ CI/CD Pipeline

| Stage | Description |
|---|---|
| **Lint** | golangci-lint v2 with 15+ linters |
| **Test** | 50+ unit tests with race detection |
| **Security** | govulncheck + gosec + Trivy container scan |
| **Build** | Multi-arch Docker images (amd64/arm64) |
| **SBOM** | Software Bill of Materials via Syft |
| **Release** | Manual approval gate → promoted to `latest` |

- **`main` branch** → builds `dev-latest` image to GHCR
- **`release/*` branch** → builds RC image → approval gate → promoted to versioned release

---

## 📁 Project Structure

```
cmd/main.go                    Operator entrypoint
api/v1/                        CRD type definitions (KubeSolvConfig)
internal/
  controller/                  Reconciler logic (pod + node watchers)
  ai/                          Gemini AI client (chat, decisions, cost analysis)
  alert/                       ChatOps (Slack + Telegram bots)
  ops/                         Cluster operations (scale, restart, cordon, pod mgmt)
  metrics/                     Prometheus client for CPU/memory metrics
config/                        Kubernetes manifests (CRDs, RBAC, deployment)
.github/workflows/             CI/CD pipeline definitions
```

---

## License

Licensed under the Apache License, Version 2.0.
