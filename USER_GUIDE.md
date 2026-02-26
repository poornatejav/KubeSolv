# 📘 KubeSolv User Deployment Guide (Production & Cluster Setup)

Welcome to KubeSolv! This guide is designed for developers, managers, and stakeholders to securely deploy our **Autonomous AI Kubernetes Operator** into a production environment. 

KubeSolv needs to talk to the Kubernetes API, Gemini's AI Brain, and your company's Chat Platforms. In a production cluster, you **should not** hardcode these passwords or run them on a local laptop. Instead, we use Kubernetes **Secrets**.

Here is how you securely deploy KubeSolv, step-by-step.

---

## Step 1: Gather Your API Keys
You will need to generate or retrieve the following keys:
1. **Google Gemini AI Key**: To power the AI's reasoning.
2. **Slack Bot Token** (`xoxb-...`): Allows the bot to post messages.
3. **Slack App Token** (`xapp-...`): Allows the bot to use WebSockets to listen to your channels securely.
4. **Telegram Bot Token**: (Optional) If you use Telegram instead of Slack.

---

## Step 2: Create a Secure Kubernetes Secret
Instead of putting passwords in standard text files, we will inject them into a Kubernetes Secret. This encrypts them at rest.

Create a file on your machine called `kubesolv-secrets.yaml`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kubesolv-credentials
  namespace: kubesolv-system
type: Opaque
stringData:
  GEMINI_API_KEY: "paste-your-key-here"
  SLACK_BOT_TOKEN: "xoxb-paste-here"
  SLACK_APP_TOKEN: "xapp-paste-here"
  SLACK_CHANNEL_ID: "C12345678" # The channel ID the bot will listen to
  TELEGRAM_BOT_TOKEN: "paste-telegram-token" # (Optional)
```

Apply the secret to your cluster:
```sh
kubectl apply -f kubesolv-secrets.yaml
```

---

## Step 3: Configure the Operator Deployment
When we build the KubeSolv docker image, we need to tell Kubernetes where to find those passwords. 

The installation framework (Kustomize) automatically creates a `Deployment` for the pod. You simply need to verify that your cluster fetches the secret. The KubeSolv pods are configured to pull environment variables directly from the `kubesolv-credentials` secret you just made.

---

## Step 4: Install and Deploy to the Cluster
Now that the cluster is holding onto your secrets, you can deploy the actual AI Operator engine into the `kubesolv-system` namespace.

Run the following command from the root of the KubeSolv project repository:

```sh
# This installs the custom definitions and deploys the operator
make install
make deploy IMG=your-registry/kubesolv:v1.0.0
```

*(Note: Speak to your Lead DevOps engineer to get the correct `IMG` tag for your company's container registry).*

---

## Step 5: Verify the AI is Online
KubeSolv will spin up a pod inside the `kubesolv-system` namespace. 
Wait a few seconds, then type a message into your Slack or Telegram channel:

> **You:** "Hey KubeSolv, are you online? What's the status of the cluster?"
> **KubeSolv:** "I'm online and ready! The cluster is currently reporting 5 healthy nodes."

Congratulations! You have successfully deployed an autonomous AI Site Reliability Engineer into your production infrastructure! 🎊
