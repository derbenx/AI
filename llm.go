package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
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
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature"`
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
}

func (a *App) getURL(botIndex int, path string) string {
	if botIndex < 0 || botIndex >= len(a.config.Bots) {
		return "http://127.0.0.1:8080" + path
	}
	url := a.config.Bots[botIndex].URL
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

	botIndex := 0 // Default to main bot
	targetBot := ""

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
			if strings.ToLower(tool) == "resume" {
				a.isCodeActive = true
				// Release lock before recursive call to avoid deadlock
				a.processingLock.Unlock()
				err := a.processMessage(args, "", true)
				a.processingLock.Lock()
				return err
			}
			output := a.ExecuteTool(text, false)
			a.logChat("tool-output", output)
			wailsruntime.EventsEmit(a.ctx, "internal-tool-message", output)
			wailsruntime.EventsEmit(a.ctx, "done", "") // Signal UI that we're done processing command
			return nil
		}

		// Check for @botname
		if strings.HasPrefix(trimmed, "@") {
			parts := strings.SplitN(trimmed, " ", 2)
			targetBot = strings.TrimPrefix(parts[0], "@")
			if len(parts) > 1 {
				text = parts[1]
			}
			for i, bot := range a.config.Bots {
				if strings.EqualFold(bot.Name, targetBot) {
					botIndex = i
					break
				}
			}
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
	bot := a.config.Bots[botIndex]
	systemPrompt := bot.Personality
	if isCodeMode {
		// Use Code Prompt as System Prompt to ensure persistence
		systemPrompt = bot.Personality + "\n\n" + a.config.CodePrompt
		systemPrompt = strings.ReplaceAll(systemPrompt, "[qa]", fmt.Sprintf("%d", a.config.MemoryLimit))
		systemPrompt = strings.ReplaceAll(systemPrompt, "[tools]", a.toolHelp("brief_list", true))
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
		Model:       "gpt-3.5-turbo", // llama-server often ignores this but expects it
		Messages:    messages,
		Stream:      true,
		Temperature: bot.Temperature,
	}

	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(a.getURL(botIndex, "/v1/chat/completions"), "application/json", bytes.NewBuffer(jsonBody))
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

func (a *App) handleBotTriggers(tool, output string) {
	for i, bot := range a.config.Bots {
		if i == 0 {
			continue // Main bot already handled
		}
		if bot.OnWrite && (tool == "filewrite" || tool == "splicefile") {
			triggerMsg := fmt.Sprintf("Bot Trigger (OnWrite): File %s was modified.\nOutput: %s", tool, output)
			// Send to triggered bot
			go a.processMessageByBot(i, triggerMsg)
		}
	}
}

func (a *App) processMessageByBot(botIndex int, text string) {
	bot := a.config.Bots[botIndex]
	// Basic implementation of non-main bot processing
	// We might want to save the reply to bot.ReplyFile if specified

	reqBody := ChatCompletionRequest{
		Model:       "gpt-3.5-turbo",
		Messages:    []Message{{Role: "system", Content: bot.Personality}, {Role: "user", Content: text}},
		Stream:      false,
		Temperature: bot.Temperature,
	}

	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(a.getURL(botIndex, "/v1/chat/completions"), "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		a.logChat("system", fmt.Sprintf("Error calling trigger bot %s: %v", bot.Name, err))
		return
	}
	defer resp.Body.Close()

	var chunk ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chunk); err == nil && len(chunk.Choices) > 0 {
		reply := chunk.Choices[0].Message.Content
		a.logChat("ai", fmt.Sprintf("[%s] %s", bot.Name, reply))
		wailsruntime.EventsEmit(a.ctx, "bot-message", map[string]string{"name": bot.Name, "content": reply})

		// If in code mode, send reply back to the main automation loop
		if a.isCodeActive {
			go a.processMessage(fmt.Sprintf("(Tool) Bot %s replied: %s", bot.Name, reply), "", true)
		}

		if bot.ReplyFile != "" {
			fullPath, err := a.securePath(bot.ReplyFile)
			if err == nil {
				os.WriteFile(fullPath, []byte(reply), 0644)
			}
		}
	}
}

func (a *App) handleToolCalls(response string) {
	lines := strings.Split(response, "\n")
	foundCommand := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for @botname: call
		if strings.HasPrefix(line, "@") {
			colonIdx := strings.Index(line, ":")
			if colonIdx != -1 {
				targetBot := strings.TrimPrefix(line[:colonIdx], "@")
				args := strings.TrimSpace(line[colonIdx+1:])

				botIndex := -1
				for i, bot := range a.config.Bots {
					if strings.EqualFold(bot.Name, targetBot) {
						botIndex = i
						break
					}
				}

				if botIndex != -1 {
					foundCommand = true
					a.logChat("system", fmt.Sprintf("Bot Calling Bot: %s", targetBot))
					go a.processMessageByBot(botIndex, args)
					return
				}
			}
		}

		// Look for tool calls (e.g., ls: .)
		colonIdx := strings.Index(line, ":")
		if colonIdx != -1 {
			tool := strings.TrimSpace(line[:colonIdx])
			if a.isToolAllowed(tool) {
				foundCommand = true
				cmd := line
				toolLower := strings.ToLower(tool)

				if toolLower == "done" {
					a.isCodeActive = false
					wailsruntime.EventsEmit(a.ctx, "code-finished", "AI has completed the task.")
					return
				}

				output := a.ExecuteTool(cmd, true)
				a.logChat("tool-output", output)

				// Handle triggers for other bots
				a.handleBotTriggers(toolLower, output)

				// Detect repetition
				if cmd == a.lastToolCmd {
					a.repeatCount++
				} else {
					a.lastToolCmd = cmd
					a.repeatCount = 1
				}

				feedback := fmt.Sprintf("(Tool) %s", output)
				if a.repeatCount >= 3 {
					feedback += "\n\nStop repeating yourself and use the \"help:\" command!"
					if a.repeatCount > 3 {
						// Suggest random tools
						toolsList := a.toolHelp("brief_list", true)
						lines := strings.Split(toolsList, "\n")
						if len(lines) > 0 {
							rand.Seed(time.Now().UnixNano())
							rand.Shuffle(len(lines), func(i, j int) { lines[i], lines[j] = lines[j], lines[i] })
							n := 3
							if len(lines) < n {
								n = len(lines)
							}
							feedback += "\nMaybe try one of these tools?\n" + strings.Join(lines[:n], "\n")
						}
					}
				}

				// Automatically send output back to AI
				go func() {
					if a.isCodeActive {
						a.processMessage(feedback, "", true)
					}
				}()
				return // Handle one command at a time to keep it sequential
			}
		}
	}

	if !foundCommand {
		// If no command found, send feedback back to AI to keep the loop going
		go func() {
			if a.isCodeActive {
				a.processMessage("(Tool) Error no tool called, did you mean help: ? Maybe check todo: ?", "", true)
			}
		}()
	}
}
