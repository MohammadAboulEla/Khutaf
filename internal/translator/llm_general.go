package translator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"khutaf/internal/config"
)

var reThinkBlock = regexp.MustCompile(`(?s)<think>.*?(?:</think>|$)\s*`)

// LLM is an OpenAI-compatible chat completions client
// (OpenAI, Gemini, DeepSeek, Groq, OpenRouter, Ollama, LM Studio, llama-server).
type LLM struct {
	cfg    config.LLMSettings
	client *http.Client
}

// NewLLM creates a client for the given settings.
func NewLLM(cfg config.LLMSettings) *LLM {
	return &LLM{
		cfg: cfg,
		client: &http.Client{
			Timeout:   60 * time.Second,
			Transport: newHTTPTransport(),
		},
	}
}

// Name implements Provider.
func (l *LLM) Name() string {
	return "llm:" + l.cfg.Model + ":" + settingsHash(
		l.cfg.BaseURL,
		l.cfg.SystemPrompt,
		l.cfg.SourceLang,
		l.cfg.TargetLang,
		strconv.FormatFloat(l.cfg.Temperature, 'f', 3, 64),
	)
}

func settingsHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

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
	if gp := GlossaryPromptFromContext(ctx); gp != "" {
		prompt += "\n\n" + gp
	}
	return doChatCompletion(ctx, l.client, l.cfg.BaseURL, l.cfg.APIKey, l.cfg.Model, l.cfg.Temperature, []chatMessage{
		{Role: "system", Content: prompt},
		{Role: "user", Content: text},
	})
}

func doChatCompletion(ctx context.Context, client *http.Client, baseURL, apiKey, model string, temp float64, messages []chatMessage) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temp,
	})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
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
	return strings.TrimSpace(reThinkBlock.ReplaceAllString(parsed.Choices[0].Message.Content, "")), nil
}
