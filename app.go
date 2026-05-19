package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx            context.Context
	config         Config
	specs          SystemSpecs
	isStarting     bool
	interactions   []Interaction
	todoList       string
	aiNotes        string
	isCodeActive   bool
	sessionLog     string
	processingLock sync.Mutex
	lastToolCmd    string
	repeatCount    int
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

func (a *App) getExecDir() string {
	ex, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(ex)
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.config = LoadConfig()

	a.todoList = a.config.TodoList
	a.aiNotes = a.config.AINotes

	baseDir := a.getExecDir()

	// Ensure directories exist
	os.MkdirAll(filepath.Join(baseDir, "gguf"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "clip"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "llama"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "tools"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "logs"), 0755)

	logPrefix := a.config.AppName
	if logPrefix == "" {
		logPrefix = filepath.Base(a.config.ProjectFolder)
	}
	if logPrefix == "." || logPrefix == "" {
		logPrefix = "AICoder"
	}
	a.StartNewSession()

	// Handle file drops
	wailsruntime.OnFileDrop(a.ctx, func(x, y int, paths []string) {
		if len(paths) > 0 {
			wailsruntime.EventsEmit(a.ctx, "file-dropped", paths[0])
		}
	})

	var err error
	a.specs, err = GetSystemSpecs()
	if err != nil {
		a.logDebug(fmt.Sprintf("Error getting system specs: %v", err))
	}

	a.logDebug("App started")
}

func (a *App) shutdown(ctx context.Context) {
}

func (a *App) GetSpecs() SystemSpecs {
	return a.specs
}

func (a *App) GetConfig() Config {
	return a.config
}

func (a *App) SaveSettings(config Config) string {
	a.config = config
	err := SaveConfig(config)
	if err != nil {
		return fmt.Sprintf("Error saving config: %v", err)
	}

	return "Settings saved"
}

func (a *App) GetImageBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	mime := "image/jpeg"
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".png" {
		mime = "image/png"
	} else if ext == ".webp" {
		mime = "image/webp"
	} else if ext == ".gif" {
		mime = "image/gif"
	}

	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
}

func (a *App) GetFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) GetTodoList() string {
	wailsruntime.EventsEmit(a.ctx, "request-todo")
	// Wails EventsEmit doesn't return values directly from frontend.
	// We need a different approach. Let's use a simple state in the App struct.
	return a.todoList
}

func (a *App) UpdateTodoList(todo string) {
	a.todoList = todo
	a.config.TodoList = todo
	SaveConfig(a.config)
}

func (a *App) GetAINotes() string {
	return a.aiNotes
}

func (a *App) UpdateAINotes(notes string) {
	a.aiNotes = notes
	// config field is lowercase for json tagging
	a.config.AINotes = notes
	SaveConfig(a.config)
}

func (a *App) SetCodeActive(active bool) {
	a.isCodeActive = active
}

func (a *App) StartNewSession() {
	baseDir := a.getExecDir()
	logPrefix := a.config.AppName
	if logPrefix == "" {
		logPrefix = filepath.Base(a.config.ProjectFolder)
	}
	if logPrefix == "." || logPrefix == "" {
		logPrefix = "AICoder"
	}
	a.sessionLog = filepath.Join(baseDir, "logs", fmt.Sprintf("%s-%s.log", logPrefix, time.Now().Format("02Jan2006-150405")))
	a.logChat("system", "--- New Session Started ---")
}

func (a *App) logChat(role, content string) {
	f, err := os.OpenFile(a.sessionLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	timestamp := time.Now().Format("15:04:05")

	roleLabel := role
	roleLower := strings.ToLower(role)
	switch roleLower {
	case "user":
		roleLabel = "(User)"
	case "ai":
		roleLabel = "(AI)"
	case "tool-output", "tool":
		roleLabel = "(Tool)"
	case "system":
		roleLabel = "(System)"
	default:
		// Use bot name
		roleLabel = fmt.Sprintf("[%s]", role)
	}

	f.WriteString(fmt.Sprintf("[%s] %s %s\n", timestamp, roleLabel, content))
}

func (a *App) GetDefaultCodePrompt() string {
	return defaultConfig.CodePrompt
}

func (a *App) GetBotModel(botIndex int) (string, error) {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// Try /props first
	url := a.getURL(botIndex, "/props")
	resp, err := client.Get(url)
	if err == nil {
		defer resp.Body.Close()
		var props struct {
			DefaultGenerationSettings struct {
				Model string `json:"model"`
			} `json:"default_generation_settings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&props); err == nil && props.DefaultGenerationSettings.Model != "" && props.DefaultGenerationSettings.Model != "." {
			modelPath := strings.ReplaceAll(props.DefaultGenerationSettings.Model, "\\", "/")
			return filepath.Base(modelPath), nil
		}
	}

	// Fallback to /v1/models (OpenAI compatible)
	url = a.getURL(botIndex, "/v1/models")
	resp, err = client.Get(url)
	if err == nil {
		defer resp.Body.Close()
		var oaiModels struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&oaiModels); err == nil && len(oaiModels.Data) > 0 {
			modelPath := strings.ReplaceAll(oaiModels.Data[0].ID, "\\", "/")
			return filepath.Base(modelPath), nil
		}
	}

	return "Unknown Model", nil
}

func (a *App) TestTool(command string) string {
	return a.ExecuteTool(command, false)
}
