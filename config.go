package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ModelPath           string `json:"model_path"`
	ClipPath            string `json:"clip_path"`
	Personality         string `json:"personality"`
	MemoryLimit         int    `json:"memory_limit"`
	RememberFirst       bool   `json:"remember_first"`
	DebugLog            bool   `json:"debug_log"`
	GPULayers           int    `json:"gpu_layers"`
	ServerURL           string `json:"server_url"`
	ServerMode          string `json:"server_mode"` // "local" or "remote"
}

var defaultConfig = Config{
	Personality:   "You are a helpful AI assistant.",
	MemoryLimit:   5,
	RememberFirst: true,
	DebugLog:      true,
	GPULayers:     0,
	ServerURL:     "http://127.0.0.1:8080",
	ServerMode:    "local",
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

	// Ensure critical defaults
	if config.ServerURL == "" {
		config.ServerURL = defaultConfig.ServerURL
	}
	if config.ServerMode == "" {
		config.ServerMode = defaultConfig.ServerMode
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

func ScanModels(dir string) ([]string, error) {
	var models []string
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if !file.IsDir() && (filepath.Ext(file.Name()) == ".gguf" || filepath.Ext(file.Name()) == ".bin") {
			models = append(models, file.Name())
		}
	}
	return models, nil
}
