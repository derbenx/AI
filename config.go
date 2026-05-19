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
	TriggerEnabled    bool    `json:"trigger_enabled"`
	TriggerCmd        string  `json:"trigger_cmd"`
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
	CodePrompt:    "You are a programmer who likes to get the program working with minimal chatter! Check your todo tool for list of things that need to be done. You can ask other AI for help see listbots for details.",
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

