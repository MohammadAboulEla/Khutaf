package translator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"khutaf/internal/config"
)

// LLM is an OpenAI-compatible chat completions client
// (OpenAI, Gemini, DeepSeek, Groq, OpenRouter, Ollama, LM Studio, llama-server).
type LLM struct {
	cfg    config.LLMSettings
	client *http.Client
}

// NewLLM creates a client for the given settings.
func NewLLM(cfg config.LLMSettings) *LLM {
	return &LLM{cfg: cfg, client: &http.Client{Timeout: 60 * time.Second}}
}

// Name implements Provider.
func (l *LLM) Name() string { return "llm:" + l.cfg.Model }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Translate implements Provider.
func (l *LLM) Translate(ctx context.Context, text string) (string, error) {
	if strings.TrimSpace(l.cfg.BaseURL) == "" || l.cfg.Model == "" {
		return "", errors.New("LLM base URL and model are required")
	}
	prompt := strings.NewReplacer("{source}", l.cfg.SourceLang, "{target}", l.cfg.TargetLang).Replace(l.cfg.SystemPrompt)
	body, err := json.Marshal(chatRequest{
		Model: l.cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: prompt},
			{Role: "user", Content: text},
		},
		Temperature: l.cfg.Temperature,
	})
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(l.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if l.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.cfg.APIKey)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("HTTP %d: invalid response: %.200s", resp.StatusCode, raw)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK || len(parsed.Choices) == 0 {
		return "", fmt.Errorf("HTTP %d: empty response", resp.StatusCode)
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
