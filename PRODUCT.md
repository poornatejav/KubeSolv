# KubeSolv — Autonomous AI Site Reliability Engineer

> **Your Kubernetes cluster, managed by AI. Monitored 24/7. Issues resolved in seconds, not hours.**

---

## Executive Summary

KubeSolv is an **autonomous AI-powered Kubernetes operator** that acts as an always-on Site Reliability Engineer (SRE) for your cluster. Built on the Kubebuilder framework and powered by **Google Gemini AI**, KubeSolv continuously monitors your cluster health, detects incidents in real-time, performs intelligent root cause analysis, and autonomously remediates issues — all while keeping your engineering team in the loop through native **Slack** and **Telegram** integration.

KubeSolv doesn't just alert — it **acts**. When a pod crashes, KubeSolv analyzes the logs, determines the cause, and either fixes it automatically or presents a one-click approval button to your team. It does the work of a senior SRE, saving your team hundreds of hours of manual toil each month.

---

## The Problem

Modern Kubernetes environments face escalating operational challenges:

| Challenge | Impact |
|---|---|
| **Alert Fatigue** | Teams drown in thousands of alerts daily, missing critical incidents |
| **Slow Incident Response** | Mean Time to Recovery (MTTR) stretches to hours as engineers manually diagnose issues |
| **Manual Toil** | Repetitive tasks like scaling, restarting, and cordoning consume 30-40% of SRE time |
| **Knowledge Gaps** | Not every team member has deep Kubernetes expertise for complex troubleshooting |
| **Off-Hours Coverage** | Production incidents at 3 AM wait until morning for resolution |
| **Cost Overprovisioning** | Over-allocated resources waste cloud budget without visibility |

---

## How KubeSolv Solves It

### 🤖 AI-Powered Incident Analysis
KubeSolv uses Google Gemini AI to perform deep root cause analysis on every incident. When a pod enters `CrashLoopBackOff`, KubeSolv doesn't just tell you — it reads the container logs, identifies the cause (segfault, OOM, config error), and recommends the precise fix.

### 🛠️ Autonomous Remediation
Based on its analysis, KubeSolv can autonomously execute safe remediation actions:
- **Rollback** failing deployments to the last known good version
- **Scale** deployments up or down based on real-time traffic patterns
- **Patch memory limits** for OOM-killed containers
- **Cordon** nodes under pressure to prevent new pod scheduling
- **Apply network policies** to unsecured namespaces

### 🛡️ Human-in-the-Loop Guardrails
KubeSolv never executes privileged operations blindly. For any action that modifies production state, it sends **interactive approval buttons** to your Slack or Telegram channel. Operations execute only when a human clicks "Approve."

### 💬 Native ChatOps
Talk to your cluster in natural language. Ask questions like:
- *"What is the health of the production namespace?"*
- *"Why is the frontend pod crashing?"*
- *"Show me the logs for the payment service."*
- *"Scale the API gateway to 5 replicas."*

KubeSolv uses **AI function calling** to autonomously fetch data, correlate information, and respond with actionable insights.

### 💰 Cost Optimization
KubeSolv continuously analyzes resource utilization via Prometheus metrics and uses AI to identify over-provisioned deployments. It recommends right-sizing adjustments with one-click approval to reduce your cloud spend.

---

## Key Features

| Feature | Description |
|---|---|
| **Real-Time Monitoring** | Watches all pods and nodes, detects issues within seconds |
| **AI Root Cause Analysis** | Gemini AI analyzes logs and cluster state to diagnose issues |
| **Autonomous Remediation** | Rollbacks, scaling, memory patches, network policies |
| **Interactive Approvals** | Slack/Telegram buttons for human-in-the-loop safety |
| **Smart Scaling** | Traffic-based horizontal pod autoscaling using log volume analysis |
| **OOM Protection** | Detects OOM kills and proposes memory limit increases with guardrails |
| **Node Health Monitoring** | Alerts on memory/disk/PID pressure with automated cordoning |
| **Security Auto-Heal** | Detects unsecured namespaces and applies default-deny network policies |
| **Cost Optimization** | AI-driven analysis of over-provisioned deployments |
| **Cross-Platform Sync** | Actions approved on Slack are reported to Telegram, and vice versa |
| **Pod Lifecycle Tracking** | New pod detection, state transition monitoring, recovery alerts |
| **Deduplication** | Smart alert deduplication prevents notification fatigue |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                        │
│                                                             │
│  ┌───────────┐   ┌───────────┐   ┌───────────┐            │
│  │  Pod       │   │  Pod      │   │  Node     │            │
│  │  Watcher   │   │  Analyzer │   │  Monitor  │            │
│  └─────┬─────┘   └─────┬─────┘   └─────┬─────┘            │
│        │               │               │                    │
│        └───────────────┼───────────────┘                    │
│                        │                                    │
│              ┌─────────▼──────────┐                         │
│              │  KubeSolv Operator  │                         │
│              │  (Reconciler Loop) │                         │
│              └────────┬───────────┘                         │
│                       │                                     │
│         ┌─────────────┼──────────────┐                      │
│         │             │              │                      │
│  ┌──────▼──────┐ ┌────▼─────┐ ┌─────▼──────┐              │
│  │ Gemini AI   │ │Prometheus│ │  ChatOps   │              │
│  │ (Analysis)  │ │(Metrics) │ │(Slack/TG)  │              │
│  └─────────────┘ └──────────┘ └────────────┘              │
└─────────────────────────────────────────────────────────────┘
```

**How it works:**
1. **KubeSolv Operator** runs as a standard Kubernetes deployment inside your cluster
2. The **Reconciler Loop** watches all pods and nodes for changes and health status
3. When an incident is detected, the **Gemini AI engine** performs root cause analysis
4. **Prometheus integration** provides CPU/memory metrics for smart scaling and cost optimization
5. **ChatOps layer** delivers alerts and interactive approvals to Slack and/or Telegram
6. **Cross-platform sync** ensures actions approved on one platform are reported on the other

---

## Use Cases

### 🔄 CrashLoopBackOff Remediation
**Scenario:** A new deployment pushes a buggy container that crashes repeatedly.

**KubeSolv Response:**
1. Detects the `CrashLoopBackOff` state within seconds
2. Reads the container logs and identifies a segmentation fault
3. AI determines the safest action is a rollback (confidence: 95%)
4. Sends an "Approve Rollback" button to Slack
5. On approval, rolls back to the previous stable ReplicaSet
6. Notifies the team: *"Pod recovered! Deployment rolled back successfully."*

### 📈 Traffic-Based Smart Scaling
**Scenario:** A flash sale drives 10x normal traffic to your e-commerce API.

**KubeSolv Response:**
1. Detects elevated log throughput (>10 logs/sec) indicating high load
2. Calculates the optimal replica count based on traffic volume
3. Scales the deployment from 2 → 4 replicas
4. As traffic normalizes, automatically scales back down
5. Respects configured min/max replica boundaries

### 💾 OOM Kill Protection
**Scenario:** A data processing pod is killed due to insufficient memory.

**KubeSolv Response:**
1. Detects the `OOMKilled` termination reason
2. Calculates a safe new memory limit (doubles current, capped at 1Gi guardrail)
3. Sends approval request: *"Approve 256Mi memory bump for data-processor"*
4. On approval, patches the deployment's container resource limits
5. Kubernetes automatically restarts the pod with new limits

### 🔒 Security Policy Enforcement
**Scenario:** A new namespace is created without any network policies.

**KubeSolv Response:**
1. Detects the absence of network policies in the namespace
2. Autonomously applies a default-deny ingress/egress policy
3. Notifies: *"No NetworkPolicy found in namespace 'staging'. Applied default-deny policy."*

---

## Integrations

| Integration | Purpose | Status |
|---|---|---|
| **Google Gemini AI** | Root cause analysis, cost optimization, natural language chat | ✅ Production |
| **Slack** | Alerts, interactive approvals, ChatOps conversations | ✅ Production |
| **Telegram** | Alerts, interactive approvals, ChatOps conversations | ✅ Production |
| **Prometheus** | CPU/memory metrics for scaling and cost analysis | ✅ Production |
| **Kubernetes Metrics Server** | Pod resource usage reporting | ✅ Production |

---

## Security Model

KubeSolv is designed with **defense in depth**:

1. **Principle of Least Privilege** — RBAC roles grant only the permissions needed for monitoring and remediation
2. **Human-in-the-Loop** — All destructive operations require explicit human approval via interactive buttons
3. **Guardrails** — Memory scaling is capped at 1Gi; replica counts respect min/max boundaries
4. **Secrets Management** — API keys are stored as Kubernetes Secrets, never hardcoded
5. **Network Isolation** — Auto-applies default-deny network policies to unsecured namespaces
6. **AI Confidence Threshold** — Auto-fix only executes when AI confidence exceeds 90%
7. **Alert Deduplication** — Prevents notification floods with configurable cooldown periods
8. **Distroless Base Image** — Production image uses `gcr.io/distroless/static:nonroot` for minimal attack surface

---

## Getting Started

### Prerequisites
- Kubernetes cluster (v1.28+)
- Go 1.24+ (for development)
- API keys for Google Gemini, Slack, and/or Telegram

### Quick Start (Local Development)
```bash
# 1. Set environment variables
export GEMINI_API_KEY="your-key"
export SLACK_BOT_TOKEN="xoxb-your-token"
export SLACK_APP_TOKEN="xapp-your-token"
export SLACK_CHANNEL_ID="C12345678"

# 2. Run the operator locally
make run
```

### Production Deployment
```bash
# 1. Create Kubernetes secret with API keys
kubectl create secret generic kubesolv-credentials \
  --from-literal=GEMINI_API_KEY=your-key \
  --from-literal=SLACK_BOT_TOKEN=xoxb-your-token \
  -n kubesolv-system

# 2. Deploy the operator
make deploy IMG=ghcr.io/your-org/kubesolv:latest
```

> 📘 See [USER_GUIDE.md](./USER_GUIDE.md) for detailed production deployment instructions.

---

## CI/CD Pipeline

KubeSolv includes a production-grade CI/CD pipeline:

| Stage | Tools | Description |
|---|---|---|
| **Lint** | golangci-lint v2 | Code quality and style enforcement |
| **Test** | Go test + race detector | 50+ unit tests with coverage reporting |
| **Security** | govulncheck, gosec, Trivy | Dependency vulnerabilities, SAST, container scanning |
| **Build** | Docker Buildx | Multi-arch images (amd64, arm64) |
| **SBOM** | Syft | Software bill of materials for supply chain security |
| **Release** | GitHub Actions + Environments | Manual approval gate for production releases |

---

## Roadmap

| Feature | Status |
|---|---|
| Multi-cluster support | 🔮 Planned |
| Custom remediation playbooks | 🔮 Planned |
| Web dashboard | 🔮 Planned |
| PagerDuty integration | 🔮 Planned |
| Helm chart distribution | 🔮 Planned |
| Audit logging | 🔮 Planned |
| Multi-tenant isolation | 🔮 Planned |

---

## License

Licensed under the Apache License, Version 2.0.

---

*KubeSolv — Because your cluster deserves an AI-powered SRE, not just another monitoring tool.*
