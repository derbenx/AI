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
	Role       string      `json:"role"`
	Content    any         `json:"content,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
	Tools       []any     `json:"tools,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []ToolCall `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int          `json:"index"`
				ID       string       `json:"id"`
				Type     string       `json:"type"`
				Function ToolFunction `json:"function"`
			} `json:"tool_calls"`
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
	// Root processMessage shouldn't hold the lock the whole time if it's going to spawn goroutines
	if isCodeMode && !a.isCodeActive {
		return nil // Task cancelled
	}

	botIndex := 0 // Default to main bot
	targetBot := ""
	allBots := false

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

		// Check for @botname or @all
		if strings.HasPrefix(trimmed, "@") {
			parts := strings.SplitN(trimmed, " ", 2)
			targetBot = strings.TrimPrefix(parts[0], "@")
			if len(parts) > 1 {
				text = parts[1]
			}

			if strings.EqualFold(targetBot, "all") {
				allBots = true
			} else {
				for i, bot := range a.config.Bots {
					if strings.EqualFold(bot.Name, targetBot) {
						botIndex = i
						break
					}
				}
			}
		}
	}

	if allBots {
		for i := range a.config.Bots {
			go func(idx int) {
				a.processSingleBotMessage(idx, text, imagePath, isCodeMode)
			}(i)
		}
		return nil
	}

	return a.processSingleBotMessage(botIndex, text, imagePath, isCodeMode)
}

func (a *App) processSingleBotMessage(botIndex int, text string, imagePath string, isCodeMode bool) error {
	return a.processSingleBotMessageFull(botIndex, text, imagePath, isCodeMode, nil)
}

func (a *App) processToolResponses() {
	a.processSingleBotMessageFull(0, "", "", true, nil)
}

func (a *App) processSingleBotMessageFull(botIndex int, text string, imagePath string, isCodeMode bool, toolResponses []Message) error {
	// Only lock for parts that modify shared state like history or logs

	var content []any
	if text != "" || imagePath != "" {
		content = append(content, TextContent{Type: "text", Text: text})
	}

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
	actualModel, _ := a.GetBotModel(botIndex)
	systemPrompt := bot.Personality
	var toolDefinitions []any

	if isCodeMode {
		// Use Code Prompt as System Prompt to ensure persistence
		systemPrompt = bot.Personality + "\n\n" + a.config.CodePrompt
		systemPrompt = strings.ReplaceAll(systemPrompt, "[qa]", fmt.Sprintf("%d", a.config.MemoryLimit))

		// Check if we should use structured tools
		toolDefinitions = a.GetToolDefinitions()
		if len(toolDefinitions) > 0 {
			// Remove [tools] placeholder if using structured tools
			systemPrompt = strings.ReplaceAll(systemPrompt, "[tools]", "")
		} else {
			systemPrompt = strings.ReplaceAll(systemPrompt, "[tools]", a.toolHelp("brief_list", true))
		}

		// Only emit once at the very start of a session
		if !strings.HasPrefix(text, "(Tool) ") {
			wailsruntime.EventsEmit(a.ctx, "system-prompt-display", systemPrompt)
		}
	}

	if !strings.HasPrefix(text, "(Tool) ") {
		a.logChat("system", systemPrompt)
	}

	if len(toolResponses) == 0 {
		if strings.HasPrefix(text, "(Tool) ") {
			a.logChat("tool", text)
			wailsruntime.EventsEmit(a.ctx, "internal-tool-message", text)
		} else {
			if botIndex == 0 { // Only log user message once for the main bot call in @all or normal mode
				a.logChat("user", text)
			}
		}
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}

	// Add history
	a.processingLock.Lock()
	history := a.getHistoryForModel()
	a.processingLock.Unlock()

	messages = append(messages, history...)

	if len(toolResponses) > 0 {
		messages = append(messages, toolResponses...)
	} else if len(content) > 0 {
		messages = append(messages, Message{Role: "user", Content: content})
	}

	reqBody := ChatCompletionRequest{
		Model:       "local-model", // llama-server often ignores this but expects it
		Messages:    messages,
		Stream:      true,
		Temperature: bot.Temperature,
		Tools:       toolDefinitions,
	}

	jsonBody, _ := json.Marshal(reqBody)
	a.logDebug(fmt.Sprintf("LLM Request: %s", string(jsonBody)))

	resp, err := http.Post(a.getURL(botIndex, "/v1/chat/completions"), "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Handle streaming response
	fullResponse := ""
	var toolCalls []ToolCall
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
			delta := chunk.Choices[0].Delta
			content := delta.Content
			reasoning := delta.ReasoningContent

			if reasoning != "" {
				wailsruntime.EventsEmit(a.ctx, "token", map[string]string{"bot": bot.Name, "model": actualModel, "token": "", "reasoning": reasoning})
			}

			if content != "" {
				fullResponse += content
				wailsruntime.EventsEmit(a.ctx, "token", map[string]string{"bot": bot.Name, "model": actualModel, "token": content, "reasoning": ""})
			}

			if len(delta.ToolCalls) > 0 {
				for _, tcChunk := range delta.ToolCalls {
					idx := tcChunk.Index
					if idx >= len(toolCalls) {
						toolCalls = append(toolCalls, ToolCall{
							ID:   tcChunk.ID,
							Type: tcChunk.Type,
							Function: ToolFunction{
								Name:      tcChunk.Function.Name,
								Arguments: tcChunk.Function.Arguments,
							},
						})
					} else {
						if tcChunk.ID != "" {
							toolCalls[idx].ID = tcChunk.ID
						}
						if tcChunk.Type != "" {
							toolCalls[idx].Type = tcChunk.Type
						}
						if tcChunk.Function.Name != "" {
							toolCalls[idx].Function.Name += tcChunk.Function.Name
						}
						if tcChunk.Function.Arguments != "" {
							toolCalls[idx].Function.Arguments += tcChunk.Function.Arguments
						}
					}
				}
			}
		}
	}

	if len(toolCalls) > 0 {
		tcJSON, _ := json.Marshal(toolCalls)
		a.logDebug(fmt.Sprintf("AI %s called tools: %s", bot.Name, string(tcJSON)))
		wailsruntime.EventsEmit(a.ctx, "tool-calls", map[string]any{"bot": bot.Name, "tool_calls": toolCalls})
	}

	// Save to memory (temporarily, might be updated after tool execution)
	a.processingLock.Lock()
	a.addToHistory(text, fullResponse)
	a.logChat(bot.Name, fullResponse)
	a.processingLock.Unlock()

	wailsruntime.EventsEmit(a.ctx, "done", map[string]string{"bot": bot.Name, "content": fullResponse})

	// If in code mode, check for tool calls
	if isCodeMode && a.isCodeActive {
		a.handleToolCallsComplex(bot.Name, fullResponse, toolCalls, text)
	}

	return nil
}

func (a *App) handleBotTriggers(tool, output string) {
	if tool != "filewrite" && tool != "splicefile" {
		return
	}

	mainBot := a.config.Bots[0]
	if !mainBot.TriggerEnabled || mainBot.TriggerCmd == "" {
		return
	}

	// Output often starts with "File `path` ..."
	file := ""
	if strings.HasPrefix(output, "File `") {
		endIdx := strings.Index(output[6:], "`")
		if endIdx != -1 {
			file = output[6 : 6+endIdx]
		}
	}

	cmd := strings.ReplaceAll(mainBot.TriggerCmd, "[file]", file)
	// Execute the command (which might be a call to another bot @botname)
	go a.processMessage(cmd, "", a.isCodeActive)
}

func (a *App) processMessageByBot(botIndex int, text string) {
	bot := a.config.Bots[botIndex]
	// Basic implementation of non-main bot processing
	// We might want to save the reply to bot.SaveOutputCommand if specified

	systemPrompt := bot.Personality

	reqBody := ChatCompletionRequest{
		Model:       "local-model",
		Messages:    []Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: text}},
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
		reasoning := chunk.Choices[0].Message.ReasoningContent
		a.logChat(bot.Name, reply)
		wailsruntime.EventsEmit(a.ctx, "bot-message", map[string]string{"name": bot.Name, "content": reply, "reasoning": reasoning})

		// If in code mode, send reply back to the main automation loop (background info)
		if a.isCodeActive {
			go a.processMessage(fmt.Sprintf("(Tool) Bot %s replied: %s", bot.Name, reply), "", true)
		}

		// Handle tool bot triggers (on reply)
		if bot.TriggerEnabled && bot.TriggerCmd != "" {
			cmd := strings.ReplaceAll(bot.TriggerCmd, "[reply]", reply)
			a.ExecuteTool(cmd, false)
		}
	}
}

func (a *App) handleToolCallsComplex(botName, response string, toolCalls []ToolCall, originalUserMsg string) {
	foundCommand := false
	var toolResponses []Message

	if len(toolCalls) > 0 {
		foundCommand = true
		for _, tc := range toolCalls {
			toolName := strings.ToLower(tc.Function.Name)
			var argsMap map[string]any
			json.Unmarshal([]byte(tc.Function.Arguments), &argsMap)

			cmd := ""
			switch toolName {
			case "fileread":
				loc := "all"
				if l, ok := argsMap["location"].(string); ok {
					loc = l
				}
				amount := ""
				if a, ok := argsMap["amount"].(string); ok {
					amount = a
				}
				fname := ""
				if f, ok := argsMap["filename"].(string); ok {
					fname = f
				}
				if loc == "all" {
					cmd = fmt.Sprintf("fileread: %s", fname)
				} else {
					cmd = fmt.Sprintf("fileread: %s %s %s", loc, amount, fname)
				}
			case "filewrite":
				op := "replace"
				if o, ok := argsMap["operation"].(string); ok {
					op = o
				}
				fname := ""
				if f, ok := argsMap["filename"].(string); ok {
					fname = f
				}
				content := ""
				if c, ok := argsMap["content"].(string); ok {
					content = c
				}
				cmd = fmt.Sprintf("filewrite: %s %s %s", op, fname, content)
			case "rm":
				fname := ""
				if f, ok := argsMap["filename"].(string); ok {
					fname = f
				}
				cmd = fmt.Sprintf("rm: %s", fname)
			case "ls":
				path := "."
				if p, ok := argsMap["path"].(string); ok {
					path = p
				}
				cmd = fmt.Sprintf("ls: %s", path)
			case "lines":
				fname := ""
				if f, ok := argsMap["filename"].(string); ok {
					fname = f
				}
				cmd = fmt.Sprintf("lines: %s", fname)
			case "filecopy":
				src := ""
				if s, ok := argsMap["src"].(string); ok {
					src = s
				}
				dst := ""
				if d, ok := argsMap["dst"].(string); ok {
					dst = d
				}
				cmd = fmt.Sprintf("filecopy: %s %s", src, dst)
			case "splicefile":
				start := 0
				if s, ok := argsMap["start_line"].(float64); ok {
					start = int(s)
				}
				end := 0
				if e, ok := argsMap["end_line"].(float64); ok {
					end = int(e)
				}
				fname := ""
				if f, ok := argsMap["filename"].(string); ok {
					fname = f
				}
				content := ""
				if c, ok := argsMap["content"].(string); ok {
					content = c
				}
				cmd = fmt.Sprintf("splicefile: %d %d %s %s", start, end, fname, content)
			case "mkdir":
				path := ""
				if p, ok := argsMap["path"].(string); ok {
					path = p
				}
				cmd = fmt.Sprintf("mkdir: %s", path)
			case "memories":
				id := 0
				if v, ok := argsMap["id"].(float64); ok {
					id = int(v)
				}
				content := ""
				if c, ok := argsMap["content"].(string); ok {
					content = c
				}
				if content != "" {
					cmd = fmt.Sprintf("memories: %d %s", id, content)
				} else {
					cmd = fmt.Sprintf("memories: %d", id)
				}
			case "todo":
				id := 0
				if v, ok := argsMap["id"].(float64); ok {
					id = int(v)
				}
				action := "read"
				if a, ok := argsMap["action"].(string); ok {
					action = a
				}
				if action == "read" {
					cmd = fmt.Sprintf("todo: %d", id)
				} else {
					cmd = fmt.Sprintf("todo: %d %s", id, action)
				}
			case "url":
				u := ""
				if v, ok := argsMap["url"].(string); ok {
					u = v
				}
				cmd = fmt.Sprintf("url: %s", u)
			case "urltxt":
				u := ""
				if v, ok := argsMap["url"].(string); ok {
					u = v
				}
				cmd = fmt.Sprintf("urltxt: %s", u)
			case "build":
				cmd = "build:"
			case "run":
				cmd = "run:"
			case "kill":
				cmd = "kill:"
			case "listbots":
				cmd = "listbots:"
			case "done":
				msg := ""
				if m, ok := argsMap["message"].(string); ok {
					msg = m
				}
				cmd = fmt.Sprintf("done: %s", msg)
			}

			if cmd != "" {
				// Detect repetition
				if cmd == a.lastToolCmd {
					a.repeatCount++
				} else {
					a.lastToolCmd = cmd
					a.repeatCount = 1
				}

				a.logDebug(fmt.Sprintf("Executing Tool from ToolCall: %s", cmd))
				output := a.ExecuteTool(cmd, true)

				if a.repeatCount >= 3 {
					output += "\n\nStop repeating yourself and use the \"help:\" command!"
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
							output += "\nMaybe try one of these tools?\n" + strings.Join(lines[:n], "\n")
						}
					}
				}

				a.logChat("tool-output", output)
				a.handleBotTriggers(toolName, output)

				toolResponses = append(toolResponses, Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    output,
				})

				if toolName == "done" {
					a.isCodeActive = false
					wailsruntime.EventsEmit(a.ctx, "code-finished", "AI has completed the task.")
					// Update history with tool calls and responses before returning
					a.processingLock.Lock()
					a.interactions = a.interactions[:len(a.interactions)-1] // Remove the temporary entry
					a.addToHistoryFull(originalUserMsg, response, toolCalls, toolResponses)
					a.processingLock.Unlock()
					return
				}
			}
		}

		// Update history with full info
		a.processingLock.Lock()
		a.interactions = a.interactions[:len(a.interactions)-1] // Remove the temporary entry
		a.addToHistoryFull(originalUserMsg, response, toolCalls, toolResponses)
		a.processingLock.Unlock()

		// Feedback to AI
		go func() {
			if a.isCodeActive && strings.EqualFold(botName, a.config.Bots[0].Name) {
				a.processToolResponses()
			}
		}()
		return
	}

	lines := strings.Split(response, "\n")
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
						// Only main bot handles tool calls to keep it sequential
						if strings.EqualFold(botName, a.config.Bots[0].Name) {
							a.processMessage(feedback, "", true)
						}
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
				// Only main bot loops back
				if strings.EqualFold(botName, a.config.Bots[0].Name) {
					a.processMessage("(Tool) Error no tool called, did you mean help: ? Maybe check todo: ?", "", true)
				}
			}
		}()
	}
}
