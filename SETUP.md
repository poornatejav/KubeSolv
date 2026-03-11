# 🔧 KubeSolv Setup Guide

Step-by-step instructions to set up the GitHub repository, CI/CD pipeline, secrets management, and production environment.

---

## Table of Contents
1. [Create & Push the GitHub Repository](#step-1-create--push-the-github-repository)
2. [Configure GitHub Actions Secrets](#step-2-configure-github-actions-secrets)
3. [Set Up the Production Environment (Approval Gate)](#step-3-set-up-the-production-environment)
4. [Install Sealed Secrets (Kubernetes)](#step-4-install-sealed-secrets-for-kubernetes)
5. [Seal Your Secrets for Deployment](#step-5-seal-your-secrets)
6. [Trigger the CI Pipeline](#step-6-trigger-the-ci-pipeline)
7. [Create a Release](#step-7-create-a-release)
8. [Rotate Secrets](#step-8-rotate-secrets)

---

## Step 1: Create & Push the GitHub Repository

### Option A: Create from Command Line
```bash
# Install GitHub CLI if not already installed
brew install gh

# Authenticate
gh auth login

# Create the repo (public, so GHCR images are free)
gh repo create KubeSolv --public --source=. --push

# Verify
gh repo view --web
```

### Option B: Create via GitHub Web UI
```bash
# 1. Go to https://github.com/new
# 2. Name: KubeSolv
# 3. Visibility: Public (free GHCR images)
# 4. Do NOT initialize with README (we already have one)
# 5. Click "Create repository"

# Then push your local repo:
git remote add origin https://github.com/<YOUR_USERNAME>/KubeSolv.git
git push -u origin main
```

---

## Step 2: Configure GitHub Actions Secrets

The CI/CD pipeline doesn't need your app secrets (Gemini, Slack, Telegram) — GHCR authentication uses the built-in `GITHUB_TOKEN` automatically.

However, if you want to add **image signing** or **external registry push** later, you'll configure secrets here.

### How to add secrets:
```
GitHub Repo → Settings → Secrets and variables → Actions → New repository secret
```

### Required secrets for CI/CD:
| Secret Name | Required | Description |
|---|---|---|
| `GITHUB_TOKEN` | ✅ Auto-provided | Built-in, no action needed. Used for GHCR login. |

### Optional secrets (for future enhancements):
| Secret Name | Required | Description |
|---|---|---|
| `COSIGN_PRIVATE_KEY` | ❌ Optional | For image signing with Cosign |
| `DOCKERHUB_TOKEN` | ❌ Optional | If pushing to Docker Hub instead of GHCR |

> **Note:** Your app secrets (Gemini, Slack, Telegram API keys) are **NOT** needed in GitHub. They are managed via **Sealed Secrets** in your Kubernetes cluster (see Step 4).

---

## Step 3: Set Up the Production Environment

This enables the **manual approval gate** in the release pipeline. When a release candidate is built, the pipeline pauses and waits for you (or a designated reviewer) to approve before promoting to production.

### Steps:
```
1. Go to: GitHub Repo → Settings → Environments
2. Click "New environment"
3. Name: production
4. Check "Required reviewers"
5. Add yourself (and any other approvers)
6. Click "Save protection rules"
```

### Optional: Add deployment branch rules
```
Under "Deployment branches":
→ Select "Selected branches"
→ Add pattern: release/*
→ This ensures only release branches can trigger production deploys
```

---

## Step 4: Install Sealed Secrets for Kubernetes

[Bitnami Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets) is a **free, open-source** tool that encrypts your Kubernetes Secrets so they can be safely stored in git. Only the Sealed Secrets controller running in your cluster can decrypt them.

### Install the controller:
```bash
# Install Sealed Secrets controller via Helm
helm repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets
helm repo update

helm install sealed-secrets sealed-secrets/sealed-secrets \
  --namespace kube-system \
  --set-string fullnameOverride=sealed-secrets-controller
```

### Install the CLI tool (kubeseal):
```bash
# macOS
brew install kubeseal

# Linux
KUBESEAL_VERSION=$(curl -s https://api.github.com/repos/bitnami-labs/sealed-secrets/releases/latest | jq -r '.tag_name' | sed 's/v//')
curl -OL "https://github.com/bitnami-labs/sealed-secrets/releases/download/v${KUBESEAL_VERSION}/kubeseal-${KUBESEAL_VERSION}-linux-amd64.tar.gz"
tar -xvzf kubeseal-${KUBESEAL_VERSION}-linux-amd64.tar.gz kubeseal
sudo install -m 755 kubeseal /usr/local/bin/kubeseal
```

### Verify installation:
```bash
kubectl get pods -n kube-system | grep sealed-secrets
# Should show the controller running

kubeseal --version
# Should print version number
```

---

## Step 5: Seal Your Secrets

### 1. Create your real secret file (locally, never commit this):
```bash
cp config/secrets/secret-template.yaml /tmp/my-kubesolv-secret.yaml
```

### 2. Edit with your real values:
```bash
# Open and fill in your real API keys
nano /tmp/my-kubesolv-secret.yaml
```

Fill in:
| Key | Where to Get It |
|---|---|
| `GEMINI_API_KEY` | [Google AI Studio](https://aistudio.google.com/apikey) |
| `SLACK_BOT_TOKEN` | [Slack API Apps](https://api.slack.com/apps) → OAuth & Permissions |
| `SLACK_APP_TOKEN` | [Slack API Apps](https://api.slack.com/apps) → Basic Information → App-Level Tokens |
| `SLACK_CHANNEL_ID` | Right-click channel in Slack → "View channel details" → scroll to bottom |
| `TELEGRAM_BOT_TOKEN` | Talk to [@BotFather](https://t.me/BotFather) on Telegram |
| `TELEGRAM_CHAT_ID` | Send a message to your bot, then visit `https://api.telegram.org/bot<TOKEN>/getUpdates` |

### 3. Create the namespace:
```bash
kubectl apply -f config/secrets/namespace.yaml
```

### 4. Seal the secret:
```bash
kubeseal --format yaml < /tmp/my-kubesolv-secret.yaml > config/secrets/sealed-secret.yaml
```

### 5. Apply the sealed secret:
```bash
kubectl apply -f config/secrets/sealed-secret.yaml
```

### 6. Verify:
```bash
# The controller will automatically unseal it into a regular Secret
kubectl get secret kubesolv-credentials -n kubesolv-system
# Should show the secret exists

# Verify a key (optional)
kubectl get secret kubesolv-credentials -n kubesolv-system -o jsonpath='{.data.GEMINI_API_KEY}' | base64 -d
```

### 7. Clean up:
```bash
# Delete the unencrypted file — NEVER leave real secrets on disk
rm /tmp/my-kubesolv-secret.yaml
```

### 8. Commit the sealed secret (safe!):
```bash
git add config/secrets/sealed-secret.yaml
git commit -m "chore: add sealed secret for KubeSolv credentials"
git push
```

---

## Step 6: Trigger the CI Pipeline

After pushing to `main`, the CI pipeline will automatically run:

```bash
# Push triggers ci.yml
git push origin main

# Watch the pipeline
gh run watch
```

### What runs:
```
✅ Lint (golangci-lint v2.8.0)
✅ Test (50+ unit tests with race detection)
✅ Security Scan (govulncheck + gosec)
✅ Build & Push (multi-arch image → ghcr.io/<you>/kubesolv:dev-latest)
✅ SBOM Generation (Syft)
✅ Trivy Container Scan
```

### Verify the image:
```bash
# Check GHCR packages
gh api user/packages/container/KubeSolv/versions --jq '.[].metadata.container.tags'

# Or visit:
# https://github.com/<YOUR_USERNAME>/KubeSolv/pkgs/container/KubeSolv
```

---

## Step 7: Create a Release

### 1. Create a release branch:
```bash
git checkout -b release/v1.0.0
git push origin release/v1.0.0
```

### 2. The release pipeline will:
```
✅ Lint
✅ Test
✅ Security Scan (strict — fails on HIGH vulnerabilities)
✅ Build RC image (ghcr.io/<you>/kubesolv:rc-<sha>)
✅ Trivy Scan (strict)
✅ SBOM generation
⏸️  PAUSE — Awaiting manual approval in "production" environment
```

### 3. Approve the release:
```
1. Go to: GitHub Repo → Actions → "KubeSolv Release Pipeline"
2. Click the waiting run
3. Click "Review deployments"
4. Select "production"
5. Click "Approve and deploy"
```

### 4. After approval:
```
✅ RC image promoted to: ghcr.io/<you>/kubesolv:v1.0.0
✅ RC image also tagged as: ghcr.io/<you>/kubesolv:latest
✅ GitHub Release created with auto-generated changelog
```

### 5. Deploy the release to your cluster:
```bash
make deploy IMG=ghcr.io/<YOUR_USERNAME>/kubesolv:v1.0.0
```

---

## Step 8: Rotate Secrets

### When to rotate:
- Immediately if secrets were exposed (our old `.env` was committed)
- Every 90 days as a best practice
- When team members leave

### How to rotate:

#### 1. Generate new API keys:
- **Gemini**: [Google AI Studio](https://aistudio.google.com/apikey) → Revoke old key, create new
- **Slack Bot/App Tokens**: [Slack API](https://api.slack.com/apps) → Regenerate tokens
- **Telegram**: Talk to [@BotFather](https://t.me/BotFather) → `/revoke` then `/newbot`

#### 2. Create a new sealed secret:
```bash
# Edit the template with new values
cp config/secrets/secret-template.yaml /tmp/rotated-secret.yaml
nano /tmp/rotated-secret.yaml

# Re-seal
kubeseal --format yaml < /tmp/rotated-secret.yaml > config/secrets/sealed-secret.yaml

# Apply
kubectl apply -f config/secrets/sealed-secret.yaml

# Restart the operator to pick up new secrets
kubectl rollout restart deployment kubesolv-controller-manager -n kubesolv-system

# Clean up
rm /tmp/rotated-secret.yaml
```

#### 3. Commit and push:
```bash
git add config/secrets/sealed-secret.yaml
git commit -m "chore: rotate KubeSolv credentials"
git push
```

---

## Environment Variables Reference

| Variable | Required | Description |
|---|---|---|
| `GEMINI_API_KEY` | ✅ Yes | Google Gemini AI API key for AI analysis |
| `SLACK_BOT_TOKEN` | ⚡ Optional | Slack bot OAuth token (starts with `xoxb-`) |
| `SLACK_APP_TOKEN` | ⚡ Optional | Slack app-level token for WebSocket (starts with `xapp-`) |
| `SLACK_CHANNEL_ID` | ⚡ Optional | Slack channel for alerts (e.g., `C0AEXM2C90W`) |
| `TELEGRAM_BOT_TOKEN` | ⚡ Optional | Telegram bot token from @BotFather |
| `TELEGRAM_CHAT_ID` | ⚡ Optional | Telegram chat/group ID for alerts |
| `PROMETHEUS_URL` | ⚡ Optional | Prometheus server URL (default: `http://localhost:9090`) |

> **⚡ Optional** = KubeSolv works without these, but the corresponding feature (Slack/Telegram/Prometheus) will be disabled.

---

## Secrets Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    Your Machine                          │
│                                                          │
│  secret-template.yaml ─── Fill in real values            │
│         │                                                │
│         ▼                                                │
│   kubeseal CLI ──── Encrypts with cluster public key     │
│         │                                                │
│         ▼                                                │
│  sealed-secret.yaml ── Safe to commit to Git!            │
└────────────┬─────────────────────────────────────────────┘
             │
             ▼ (git push)
┌──────────────────────────────────────────────────────────┐
│                  GitHub Repository                        │
│                                                          │
│  sealed-secret.yaml ── Encrypted, nobody can read it     │
└────────────┬─────────────────────────────────────────────┘
             │
             ▼ (kubectl apply)
┌──────────────────────────────────────────────────────────┐
│                Kubernetes Cluster                         │
│                                                          │
│  Sealed Secrets Controller                               │
│         │                                                │
│         ▼ (auto-decrypts)                                │
│  Regular K8s Secret ── Available to KubeSolv pods         │
│         │                                                │
│         ▼ (env vars)                                     │
│  KubeSolv Operator Pod                                   │
│    GEMINI_API_KEY=xxx                                    │
│    SLACK_BOT_TOKEN=xxx                                   │
│    ...                                                   │
└──────────────────────────────────────────────────────────┘
```

**Why Sealed Secrets?**
- ✅ **Free** — open-source, no vendor lock-in
- ✅ **Git-native** — encrypted secrets live alongside your code
- ✅ **Cluster-scoped** — only YOUR cluster can decrypt them
- ✅ **Audit trail** — secret changes are tracked in git history
- ✅ **No external service** — no Vault server, no SaaS to pay for
