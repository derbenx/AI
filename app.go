package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx          context.Context
	config       Config
	specs        SystemSpecs
	server       *LLMServer
	isStarting   bool
	interactions []Interaction
	todoList     string
	aiNotes      string
	isCodeActive bool
	sessionLog   string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) CheckServerExecutable() bool {
	executable := "llama-server"
	if runtime.GOOS == "windows" {
		executable = "llama-server.exe"
	}
	execPath := filepath.Join("llama", executable)
	if _, err := os.Stat(execPath); err != nil {
		return false
	}

	// Check for modular backends on Windows (newer llama.cpp versions)
	if runtime.GOOS == "windows" {
		files, err := os.ReadDir("llama")
		if err == nil {
			for _, f := range files {
				if strings.HasPrefix(f.Name(), "ggml-") && strings.HasSuffix(f.Name(), ".dll") {
					return true
				}
			}
		}
		// If no ggml-*.dll found, it might be an older static build, or it's missing backends.
		// We'll return true but the log will show the error if it fails to load.
	}

	return true
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
	a.StopServer()
}

func (a *App) GetSpecs() SystemSpecs {
	return a.specs
}

func (a *App) GetConfig() Config {
	return a.config
}

func (a *App) SaveSettings(config Config) string {
	oldModel := a.config.ModelPath
	oldLayers := a.config.GPULayers
	oldClip := a.config.ClipPath
	oldURL := a.config.ServerURL

	a.config = config
	err := SaveConfig(config)
	if err != nil {
		return fmt.Sprintf("Error saving config: %v", err)
	}

	// If the mode changed to remote, stop local server
	if a.config.ServerMode == "remote" && a.IsServerRunning() {
		a.StopServer()
		return "Settings saved. Switched to remote mode (local server stopped)."
	}

	// If model or heavy settings changed, restart local server
	if (oldModel != config.ModelPath || oldLayers != config.GPULayers || oldClip != config.ClipPath || oldURL != config.ServerURL) && a.IsServerRunning() && a.isLocalServer() {
		a.StopServer()
		go func() {
			time.Sleep(1 * time.Second)
			a.StartServer()
		}()
		return "Settings saved. Server is restarting with new model/settings..."
	}

	return "Settings saved"
}

func (a *App) ListModels() []string {
	models, _ := ScanModels("gguf")
	return models
}

func (a *App) ListClips() []string {
	clips, _ := ScanModels("clip")
	return clips
}

func (a *App) GetBalancedLayers(modelName string) int {
	path := filepath.Join("gguf", modelName)
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	sizeGB := float64(info.Size()) / (1024 * 1024 * 1024)
	return a.specs.CalculateBalancedGPU(sizeGB)
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
	switch strings.ToLower(role) {
	case "user":
		roleLabel = "(User)"
	case "ai":
		roleLabel = "(AI)"
	case "tool-output", "tool":
		roleLabel = "(Tool)"
	case "system":
		roleLabel = "(System)"
	}

	f.WriteString(fmt.Sprintf("[%s] %s %s\n", timestamp, roleLabel, content))
}

func (a *App) GetDefaultCodePrompt() string {
	return defaultConfig.CodePrompt
}

func (a *App) TestTool(command string) string {
	return a.ExecuteTool(command)
}
