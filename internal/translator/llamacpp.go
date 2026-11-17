//go:build windows

package translator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"khutaf/internal/config"
)

const maxLlamaLogBytes = 4096

// LlamaCppStatus describes the managed llama-server.exe process state.
type LlamaCppStatus struct {
	Running    bool   `json:"running"`
	Ready      bool   `json:"ready"`
	PID        int    `json:"pid"`
	Port       int    `json:"port"`
	BaseURL    string `json:"baseUrl"`
	ModelPath  string `json:"modelPath"`
	ModelName  string `json:"modelName"`
	LastError  string `json:"lastError"`
	RecentLogs string `json:"recentLogs"`
}

type boundedLog struct {
	mu  sync.Mutex
	buf []byte
}

func (l *boundedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	if len(l.buf) > maxLlamaLogBytes {
		l.buf = append([]byte(nil), l.buf[len(l.buf)-maxLlamaLogBytes:]...)
	}
	return len(p), nil
}

func (l *boundedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.buf)
}

func (l *boundedLog) Reset() {
	l.mu.Lock()
	l.buf = nil
	l.mu.Unlock()
}

// LlamaCppManager supervises a local llama-server.exe process.
type LlamaCppManager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan struct{}
	running   bool
	ready     bool
	pid       int
	port      int
	modelPath string
	lastError string
	logs      boundedLog
	client    *http.Client
}

// NewLlamaCppManager creates a new llama.cpp process supervisor.
func NewLlamaCppManager() *LlamaCppManager {
	return &LlamaCppManager{
		port:   8080,
		client: &http.Client{Timeout: 1500 * time.Millisecond},
	}
}

// Start launches llama-server.exe with cfg and waits until /health responds 200 OK.
func (m *LlamaCppManager) Start(cfg config.LlamaCppSettings) error {
	bin := strings.TrimSpace(cfg.BinaryPath)
	model := strings.TrimSpace(cfg.ModelPath)
	if bin == "" {
		return errors.New("llama-server.exe path is required")
	}
	if model == "" {
		return errors.New("GGUF model path is required")
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("llama-server binary not found: %w", err)
	}
	if _, err := os.Stat(model); err != nil {
		return fmt.Errorf("GGUF model not found: %w", err)
	}

	_ = m.Stop()

	port := cfg.Port
	if port <= 0 {
		port = 8080
	}
	ctxSize := cfg.CtxSize
	if ctxSize <= 0 {
		ctxSize = 4096
	}

	args := []string{
		"-m", model,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-ngl", strconv.Itoa(cfg.GPULayers),
		"-c", strconv.Itoa(ctxSize),
	}
	if extra := strings.Fields(cfg.ExtraArgs); len(extra) > 0 {
		args = append(args, extra...)
	}

	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	m.logs.Reset()
	cmd.Stdout = &m.logs
	cmd.Stderr = &m.logs

	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		m.lastError = err.Error()
		m.mu.Unlock()
		return fmt.Errorf("start llama-server: %w", err)
	}

	done := make(chan struct{})
	m.mu.Lock()
	m.cmd = cmd
	m.done = done
	m.running = true
	m.ready = false
	m.pid = cmd.Process.Pid
	m.port = port
	m.modelPath = model
	m.lastError = ""
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if m.cmd == cmd {
			m.running = false
			m.ready = false
			m.pid = 0
			m.cmd = nil
			if err != nil {
				m.lastError = err.Error()
			}
		}
		m.mu.Unlock()
		close(done)
	}()

	// Wait up to 60s for /health to become ready, or fail early if the process exits.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			m.mu.Lock()
			errMsg := m.lastError
			m.mu.Unlock()
			if errMsg == "" {
				errMsg = "process exited unexpectedly"
			}
			return fmt.Errorf("llama-server exited: %s", errMsg)
		default:
		}
		if m.probeHealth(port) {
			m.mu.Lock()
			if m.cmd == cmd {
				m.ready = true
			}
			m.mu.Unlock()
			return nil
		}
		time.Sleep(350 * time.Millisecond)
	}

	return errors.New("llama-server started but health check timed out")
}

// Stop terminates the managed llama-server.exe process if running.
func (m *LlamaCppManager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	done := m.done
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
	m.mu.Lock()
	if m.cmd == cmd {
		m.cmd = nil
		m.running = false
		m.ready = false
		m.pid = 0
	}
	m.mu.Unlock()
	return nil
}

// Close stops the managed server on application shutdown.
func (m *LlamaCppManager) Close() error {
	return m.Stop()
}

// Status returns the current llama-server status and recent logs.
func (m *LlamaCppManager) Status() LlamaCppStatus {
	m.mu.Lock()
	st := LlamaCppStatus{
		Running:    m.running,
		Ready:      m.ready,
		PID:        m.pid,
		Port:       m.port,
		BaseURL:    fmt.Sprintf("http://127.0.0.1:%d/v1", m.port),
		ModelPath:  m.modelPath,
		LastError:  m.lastError,
		RecentLogs: m.logs.String(),
	}
	m.mu.Unlock()

	if st.ModelPath != "" {
		st.ModelName = filepath.Base(st.ModelPath)
	}
	if st.Running && !st.Ready {
		if m.probeHealth(st.Port) {
			m.mu.Lock()
			m.ready = true
			m.mu.Unlock()
			st.Ready = true
		}
	}
	return st
}

func (m *LlamaCppManager) probeHealth(port int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, path := range []string{"/health", "/v1/models"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
		if err != nil {
			continue
		}
		resp, err := m.client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
	}
	return false
}
