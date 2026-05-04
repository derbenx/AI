package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	if a.server != nil {
		return nil
	}

	modelPath := filepath.Join("gguf", a.config.ModelPath)
	if _, err := os.Stat(modelPath); err != nil {
		return fmt.Errorf("model not found: %s", modelPath)
	}

	executable := "llama-server"
	if runtime.GOOS == "windows" {
		executable = "llama-server.exe"
	}
	execPath := filepath.Join("llama", executable)

	args := []string{
		"-m", modelPath,
		"--port", "8080",
		"-ngl", fmt.Sprintf("%d", a.config.GPULayers),
	}

	if a.config.ClipPath != "" {
		clipPath := filepath.Join("clip", a.config.ClipPath)
		if _, err := os.Stat(clipPath); err == nil {
			args = append(args, "--mmproj", clipPath)
		}
	}

	a.logDebug(fmt.Sprintf("Starting server: %s %s", execPath, strings.Join(args, " ")))

	cmd := exec.Command(execPath, args...)

	// Hide window on Windows
	cmd.SysProcAttr = getSysProcAttr()

	// Redirect output to debug log or discard
	if a.config.DebugLog {
		f, _ := os.OpenFile("llama-server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		cmd.Stdout = f
		cmd.Stderr = f
	}

	err := cmd.Start()
	if err != nil {
		return err
	}

	// Wait for server to be ready
	started := false
	for i := 0; i < 30; i++ {
		// Check if process is still running
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return fmt.Errorf("llama-server exited prematurely. Check llama-server.log for details.")
		}

		resp, err := http.Get("http://127.0.0.1:8080/health")
		if err == nil {
			status := resp.StatusCode
			resp.Body.Close()
			if status == http.StatusOK {
				started = true
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if !started {
		cmd.Process.Kill()
		return fmt.Errorf("llama-server failed to start within 30 seconds")
	}

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

func (a *App) SendMessage(text string, imagePath string) error {
	if a.server == nil {
		err := a.StartServer()
		if err != nil {
			return err
		}
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
	resp, err := http.Post("http://127.0.0.1:8080/v1/chat/completions", "application/json", bytes.NewBuffer(jsonBody))
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
