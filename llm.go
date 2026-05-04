package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ImageURLContent struct {
	Type     string   `json:"type"`
	ImageURL ImageURL `json:"image_url"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

type LLMServer struct {
	cmd *exec.Cmd
}

func (a *App) StartServer() error {
	if a.server != nil || a.isStarting {
		return nil
	}
	a.isStarting = true
	defer func() { a.isStarting = false }()

	absModelPath, _ := filepath.Abs(filepath.Join("gguf", a.config.ModelPath))
	if _, err := os.Stat(absModelPath); err != nil {
		return fmt.Errorf("model not found: %s", absModelPath)
	}

	executable := "llama-server"
	if runtime.GOOS == "windows" {
		executable = "llama-server.exe"
	}

	llamaDir, _ := filepath.Abs("llama")
	execPath := filepath.Join(llamaDir, executable)

	if _, err := os.Stat(execPath); err != nil {
		return fmt.Errorf("llama-server not found at %s. Please ensure the 'llama' folder contains the executable.", execPath)
	}

	args := []string{
		"-m", absModelPath,
		"--port", "8080",
		"-ngl", fmt.Sprintf("%d", a.config.GPULayers),
		"--host", "127.0.0.1",
	}

	if a.config.ClipPath != "" {
		clipPath, _ := filepath.Abs(filepath.Join("clip", a.config.ClipPath))
		if _, err := os.Stat(clipPath); err == nil {
			args = append(args, "--mmproj", clipPath)
		}
	}

	a.logDebug(fmt.Sprintf("Starting server in %s: %s %s", llamaDir, execPath, strings.Join(args, " ")))

	cmd := exec.Command(execPath, args...)
	cmd.Dir = llamaDir // Set working directory so it finds DLLs

	// Hide window on Windows
	cmd.SysProcAttr = getSysProcAttr()

	// Capture output for streaming
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	err := cmd.Start()
	if err != nil {
		return err
	}

	readyChan := make(chan bool, 1)

	// Stream logs to frontend and watch for "listening" message
	scanLogs := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			wailsruntime.EventsEmit(a.ctx, "server-log", line)
			if strings.Contains(line, "server is listening on") {
				select {
				case readyChan <- true:
				default:
				}
			}
		}
	}

	go scanLogs(stdout)
	go scanLogs(stderr)

	// Background HTTP health check
	go func() {
		for i := 0; i < 300; i++ {
			resp, err := http.Get(a.config.ServerURL + "/health")
			if err == nil {
				status := resp.StatusCode
				resp.Body.Close()
				if status == http.StatusOK {
					select {
					case readyChan <- true:
					default:
					}
					return
				}
			}
			time.Sleep(1 * time.Second)
		}
	}()

	// Wait for server to be ready (longer timeout for old hardware)
	wailsruntime.EventsEmit(a.ctx, "server-status", "Booting...")

	started := false
	timeout := time.After(300 * time.Second)

	select {
	case <-readyChan:
		started = true
	case <-timeout:
		started = false
	}

	// Double check if process is still running
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return fmt.Errorf("llama-server exited prematurely. Check logs in the Server tab.")
	}

	if !started {
		cmd.Process.Kill()
		return fmt.Errorf("llama-server failed to start within 5 minutes")
	}

	wailsruntime.EventsEmit(a.ctx, "server-status", "Running")
	a.server = &LLMServer{cmd: cmd}
	return nil
}

func (a *App) StopServer() {
	if a.server != nil && a.server.cmd != nil {
		if runtime.GOOS == "windows" {
			a.server.cmd.Process.Kill()
		} else {
			a.server.cmd.Process.Signal(os.Interrupt)
		}
		a.server = nil
		a.logDebug("Server stopped")
	}
}

func (a *App) IsServerRunning() bool {
	return a.server != nil
}

func (a *App) isLocalServer() bool {
	return a.config.ServerMode == "local"
}

func (a *App) SendMessage(text string, imagePath string) error {
	if a.server == nil && a.isLocalServer() {
		return fmt.Errorf("Server not running. Please start a llama_server from the Server tab.")
	}

	var content []any
	content = append(content, TextContent{Type: "text", Text: text})

	if imagePath != "" {
		imgData, err := os.ReadFile(imagePath)
		if err == nil {
			mime := "image/jpeg"
			if strings.HasSuffix(imagePath, ".png") {
				mime = "image/png"
			}
			base64Img := base64.StdEncoding.EncodeToString(imgData)
			content = append(content, ImageURLContent{
				Type: "image_url",
				ImageURL: ImageURL{
					URL: fmt.Sprintf("data:%s;base64,%s", mime, base64Img),
				},
			})
		}
	}

	// Build context with memory
	messages := []Message{
		{Role: "system", Content: a.config.Personality},
	}

	// Add history (to be implemented in memory.go)
	history := a.getHistoryForModel()
	messages = append(messages, history...)

	messages = append(messages, Message{Role: "user", Content: content})

	reqBody := ChatCompletionRequest{
		Model:    "gpt-3.5-turbo", // llama-server often ignores this but expects it
		Messages: messages,
		Stream:   true,
	}

	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(a.config.ServerURL+"/v1/chat/completions", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Handle streaming response
	fullResponse := ""
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk ChatCompletionResponse
		err = json.Unmarshal([]byte(data), &chunk)
		if err == nil && len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			fullResponse += content
			wailsruntime.EventsEmit(a.ctx, "token", content)
		}
	}

	// Save to memory
	a.addToHistory(text, fullResponse)
	wailsruntime.EventsEmit(a.ctx, "done", fullResponse)

	return nil
}
