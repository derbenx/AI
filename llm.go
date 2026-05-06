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
		"--jinja",
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

func (a *App) getURL(path string) string {
	url := a.config.ServerURL
	if url == "" {
		url = "http://127.0.0.1:8080"
	}
	url = strings.TrimSuffix(url, "/")
	return url + path
}

func (a *App) SendMessage(text string, imagePath string) error {
	return a.processMessage(text, imagePath, false)
}

func (a *App) SendCodeMessage(text string) error {
	return a.processMessage(text, "", true)
}

func (a *App) processMessage(text string, imagePath string, isCodeMode bool) error {
	a.processingLock.Lock()
	defer a.processingLock.Unlock()

	if isCodeMode && !a.isCodeActive {
		return nil // Task cancelled
	}

	// Intercept user commands (not from AI/Tool)
	if !strings.HasPrefix(text, "(Tool) ") {
		trimmed := strings.TrimSpace(text)
		isCommand := false
		var tool, args string

		if strings.HasPrefix(trimmed, ":") {
			isCommand = true
			cmdBody := strings.TrimPrefix(trimmed, ":")
			if idx := strings.Index(cmdBody, ":"); idx != -1 {
				tool = strings.TrimSpace(cmdBody[:idx])
				args = strings.TrimSpace(cmdBody[idx+1:])
			} else {
				parts := strings.SplitN(cmdBody, " ", 2)
				tool = parts[0]
				if len(parts) > 1 {
					args = parts[1]
				}
			}
		} else if idx := strings.Index(trimmed, ":"); idx != -1 {
			// Check if the prefix is a known tool
			possibleTool := strings.TrimSpace(trimmed[:idx])
			if a.toolExists(possibleTool) {
				isCommand = true
				tool = possibleTool
				args = strings.TrimSpace(trimmed[idx+1:])
			}
		}

		if isCommand {
			if tool == "resume" {
				a.isCodeActive = true
				// Release lock before recursive call to avoid deadlock
				a.processingLock.Unlock()
				err := a.processMessage("(Tool) resume: "+args, "", true)
				a.processingLock.Lock()
				return err
			}
			output := a.ExecuteTool(text, false)
			a.logChat("tool-output", fmt.Sprintf("[%s] %s", text, output))
			wailsruntime.EventsEmit(a.ctx, "internal-tool-message", fmt.Sprintf("Command Output: %s", output))
			wailsruntime.EventsEmit(a.ctx, "done", "") // Signal UI that we're done processing command
			return nil
		}
	}

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
	systemPrompt := a.config.Personality
	if isCodeMode {
		// Use Code Prompt as System Prompt to ensure persistence
		systemPrompt = a.config.Personality + "\n\n" + a.config.CodePrompt
		systemPrompt = strings.ReplaceAll(systemPrompt, "[qa]", fmt.Sprintf("%d", a.config.MemoryLimit))
		systemPrompt = strings.ReplaceAll(systemPrompt, "[tools]", a.toolHelp(""))
		// Only emit once at the very start of a session
		if !strings.HasPrefix(text, "(Tool) ") {
			wailsruntime.EventsEmit(a.ctx, "system-prompt-display", systemPrompt)
		}
	}

	if !strings.HasPrefix(text, "(Tool) ") {
		a.logChat("system", systemPrompt)
	}

	if strings.HasPrefix(text, "(Tool) ") {
		a.logChat("tool", text)
		wailsruntime.EventsEmit(a.ctx, "internal-tool-message", text)
	} else {
		a.logChat("user", text)
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}

	// Add history
	history := a.getHistoryForModel()
	messages = append(messages, history...)

	messages = append(messages, Message{Role: "user", Content: content})

	reqBody := ChatCompletionRequest{
		Model:    "gpt-3.5-turbo", // llama-server often ignores this but expects it
		Messages: messages,
		Stream:   true,
	}

	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(a.getURL("/v1/chat/completions"), "application/json", bytes.NewBuffer(jsonBody))
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
	a.logChat("ai", fullResponse)
	wailsruntime.EventsEmit(a.ctx, "done", fullResponse)

	// If in code mode, check for tool calls
	if isCodeMode && a.isCodeActive {
		a.handleToolCalls(fullResponse)
	}

	return nil
}

func (a *App) handleToolCalls(response string) {
	lines := strings.Split(response, "\n")
	foundCommand := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Look for command: tool args
		if strings.HasPrefix(line, "command:") {
			foundCommand = true
			cmd := strings.TrimPrefix(line, "command:")
			cmd = strings.TrimSpace(cmd)

			if cmd == "done:" {
				wailsruntime.EventsEmit(a.ctx, "code-finished", "AI has completed the task.")
				return
			}

			output := a.ExecuteTool(cmd, true)
			a.logChat("tool-output", fmt.Sprintf("[%s] %s", cmd, output))

			// Automatically send output back to AI
			go func() {
				// time.Sleep(200 * time.Millisecond) // Removed sleep as mutex handles timing
				if a.isCodeActive {
					a.processMessage(fmt.Sprintf("(Tool) %s: %s", cmd, output), "", true)
				}
			}()
			return // Handle one command at a time to keep it sequential
		}
	}

	if !foundCommand {
		// If no command found, the AI might be done or just chatting.
		// We signal completion to break the loop.
		wailsruntime.EventsEmit(a.ctx, "code-finished", "AI finished responding without a tool call.")
	}
}
