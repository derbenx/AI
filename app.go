package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx          context.Context
	config       Config
	specs        SystemSpecs
	server       *LLMServer
	interactions []Interaction
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
	_, err := os.Stat(execPath)
	return err == nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.config = LoadConfig()

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
	a.config = config
	err := SaveConfig(config)
	if err != nil {
		return fmt.Sprintf("Error saving config: %v", err)
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
