package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/generative-ai-go/genai"
	"golang.org/x/time/rate"
	"google.golang.org/api/option"
)

// ── Config ──

var (
	geminiAPIKey    string
	validKeys       map[string]bool
	rateLimiters    sync.Map // licenseKey -> *rate.Limiter
	geminiClient    *genai.Client
	geminiModel     *genai.GenerativeModel
	chatSessions    sync.Map // sessionID -> *genai.ChatSession
	sessionMutex    sync.Mutex
)

func main() {
	geminiAPIKey = os.Getenv("GEMINI_API_KEY")
	if geminiAPIKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}

	// Parse valid license keys
	validKeys = make(map[string]bool)
	for _, key := range strings.Split(os.Getenv("VALID_LICENSE_KEYS"), ",") {
		key = strings.TrimSpace(key)
		if key != "" {
			validKeys[key] = true
		}
	}
	if len(validKeys) == 0 {
		log.Fatal("VALID_LICENSE_KEYS is required (comma-separated)")
	}

	// Init Gemini client
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(geminiAPIKey))
	if err != nil {
		log.Fatalf("Failed to create Gemini client: %v", err)
	}
	geminiClient = client
	geminiModel = geminiClient.GenerativeModel("gemini-2.5-flash")
	geminiModel.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text("You are KubeSolv AI, an expert Kubernetes SRE. Analyze incidents, optimize costs, and provide expert guidance."),
		},
	}

	// Start session reaper
	go sessionReaper()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ai/chat", withAuth(handleChat))
	mux.HandleFunc("/v1/ai/evaluate-incident", withAuth(handleEvaluateIncident))
	mux.HandleFunc("/v1/ai/cost-optimize", withAuth(handleCostOptimize))
	mux.HandleFunc("/healthz", handleHealthz)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("KubeSolv AI Proxy starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// ── Auth Middleware ──

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		key := strings.TrimPrefix(auth, "Bearer ")
		if !validKeys[key] {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Rate limiting: 100 requests/minute per key
		limiter := getLimiter(key)
		if !limiter.Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}

		next(w, r)
	}
}

func getLimiter(key string) *rate.Limiter {
	if v, ok := rateLimiters.Load(key); ok {
		return v.(*rate.Limiter)
	}
	// 100 requests per minute = ~1.67/sec, burst of 10
	limiter := rate.NewLimiter(rate.Every(600*time.Millisecond), 10)
	rateLimiters.Store(key, limiter)
	return limiter
}

// ── Handlers ──

type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type ChatResponse struct {
	Response string `json:"response"`
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	session := getOrCreateSession(req.SessionID)
	resp, err := session.SendMessage(r.Context(), genai.Text(req.Message))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var text string
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		if t, ok := resp.Candidates[0].Content.Parts[0].(genai.Text); ok {
			text = string(t)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ChatResponse{Response: text})
}

type IncidentRequest struct {
	IncidentType string `json:"incident_type"`
	ResourceName string `json:"resource_name"`
	Namespace    string `json:"namespace"`
	Details      string `json:"details"`
	Logs         string `json:"logs"`
}

func handleEvaluateIncident(w http.ResponseWriter, r *http.Request) {
	var req IncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	prompt := fmt.Sprintf(`You are a Senior SRE. Analyze this incident:
Incident: %s
Resource: %s (Namespace: %s)
Details: %s
Logs: %s

Return JSON: {"should_auto_fix": bool, "action": "string", "confidence": int, "reason": "string", "parameters": "string"}`,
		req.IncidentType, req.ResourceName, req.Namespace, req.Details, req.Logs)

	resp, err := geminiModel.GenerateContent(r.Context(), genai.Text(prompt))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeGeminiResponse(w, resp)
}

type CostRequest struct {
	Namespace       string  `json:"namespace"`
	Deployment      string  `json:"deployment"`
	CurrentReplicas int32   `json:"current_replicas"`
	CPUUsage        float64 `json:"cpu_usage"`
	MemoryUsage     float64 `json:"memory_usage"`
}

func handleCostOptimize(w http.ResponseWriter, r *http.Request) {
	var req CostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	prompt := fmt.Sprintf(`Analyze cost optimization for:
Deployment: %s (Namespace: %s)
Current Replicas: %d, CPU: %.2f cores, Memory: %.2f MB

Return JSON: {"optimize": bool, "recommended_replicas": int, "reason": "string"}`,
		req.Deployment, req.Namespace, req.CurrentReplicas, req.CPUUsage, req.MemoryUsage)

	resp, err := geminiModel.GenerateContent(r.Context(), genai.Text(prompt))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeGeminiResponse(w, resp)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// ── Helpers ──

func writeGeminiResponse(w http.ResponseWriter, resp *genai.GenerateContentResponse) {
	w.Header().Set("Content-Type", "application/json")
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		if t, ok := resp.Candidates[0].Content.Parts[0].(genai.Text); ok {
			// Forward the raw Gemini text (it's already JSON for structured prompts)
			_, _ = w.Write([]byte(string(t)))
			return
		}
	}
	_, _ = w.Write([]byte(`{"error":"empty response"}`))
}

func getOrCreateSession(sessionID string) *genai.ChatSession {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()

	if v, ok := chatSessions.Load(sessionID); ok {
		return v.(*genai.ChatSession)
	}

	session := geminiModel.StartChat()
	chatSessions.Store(sessionID, session)
	return session
}

func sessionReaper() {
	// Simple reaper: clear all sessions every 30 minutes
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		chatSessions.Range(func(key, _ any) bool {
			chatSessions.Delete(key)
			return true
		})
		log.Println("Session cache cleared")
	}
}
