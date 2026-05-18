package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type BotConfig struct {
	Name              string  `json:"name"`
	URL               string  `json:"url"`
	Personality       string  `json:"personality"`
	Temperature       float64 `json:"temperature"`
	OnWrite           bool    `json:"on_write"`
	TriggerCommand    string  `json:"trigger_command"`
	SaveOutputCommand string  `json:"save_output_command"`
}

type Config struct {
	Bots          []BotConfig `json:"bots"`
	MemoryLimit   int         `json:"memory_limit"`
	RememberFirst bool        `json:"remember_first"`
	DebugLog      bool        `json:"debug_log"`
	ProjectFolder string      `json:"project_folder"`
	BuildCommand  string      `json:"build_command"`
	RunCommand    string      `json:"run_command"`
	KillCommand   string      `json:"kill_command"`
	AppName       string      `json:"app_name"`
	CodePrompt    string      `json:"code_prompt"`
	AllowedTools  []string    `json:"allowed_tools"`
	TodoList      string      `json:"todo_list"`
	AINotes       string      `json:"ai_notes"`
}

var defaultConfig = Config{
	Bots: []BotConfig{
		{
			Name:        "Main",
			URL:         "http://127.0.0.1:8080",
			Personality: "You are a specialized AI programming assistant. You follow instructions precisely and provide efficient, high-quality code solutions.",
			Temperature: 0.7,
		},
	},
	MemoryLimit:   15,
	RememberFirst: true,
	DebugLog:      true,
	BuildCommand:  "build {app}",
	RunCommand:    "{app}",
	KillCommand:   "kill {app}",
	CodePrompt:    "You are a programmer who likes to get the program working with minimal chatter! You are NOT GPT-4, you are a specialized coding model. Remember you have a rolling window of [qa] prompt/replies, so make good use of your 'note' tool to persist technical data and memory so you don't forget what you are doing!\n\nThe automation loop continues as long as you keep calling tools. You MUST use a tool to get started!\nTry calling: \nhelp: to list commands\nls: to see the files you can work with.\ntodo: 0 to see the checklist.\nTest if it compiles with, build:\nRead the log file to check for errors with fread: build.log\n\nTo use a tool, start a new line with the tool name followed by a colon and its arguments. Direct tool calls only, no extra chatter on the same line:\n[tools]",
	AllowedTools:  []string{},
}

func LoadConfig() Config {
	data, err := os.ReadFile("ai.json")
	if err != nil {
		return defaultConfig
	}

	config := defaultConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return defaultConfig
	}

	// Ensure at least one bot exists
	if len(config.Bots) == 0 {
		config.Bots = defaultConfig.Bots
	}

	return config
}

func SaveConfig(config Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("ai.json", data, 0644)
}

func (a *App) logDebug(message string) {
	if !a.config.DebugLog {
		return
	}
	f, err := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(fmt.Sprintf("%s\n", message))
}

