package main

import (
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

type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (a *App) ListAvailableTools() ([]ToolInfo, error) {
	toolsDir := filepath.Join("build", "bin", "tools")
	if _, err := os.Stat(toolsDir); os.IsNotExist(err) {
		err := os.MkdirAll(toolsDir, 0755)
		if err != nil {
			return nil, err
		}
	}

	files, err := os.ReadDir(toolsDir)
	if err != nil {
		return nil, err
	}

	var tools []ToolInfo
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".txt") {
			name := strings.TrimSuffix(file.Name(), ".txt")
			description, _ := os.ReadFile(filepath.Join(toolsDir, file.Name()))
			tools = append(tools, ToolInfo{
				Name:        name,
				Description: string(description),
			})
		}
	}
	return tools, nil
}

func (a *App) ExecuteTool(command string) string {
	parts := strings.SplitN(command, " ", 2)
	tool := parts[0]
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	// Check if tool is allowed
	if !a.isToolAllowed(tool) {
		return fmt.Sprintf("Error: Tool '%s' is not allowed or not found.", tool)
	}

	switch tool {
	case "fread":
		return a.toolFRead(args)
	case "fwrite":
		return a.toolFWrite(args)
	case "rm":
		return a.toolRM(args)
	case "ls":
		return a.toolLS(args)
	case "note":
		return a.toolNote(args)
	case "url":
		return a.toolURL(args, false)
	case "urltxt":
		return a.toolURL(args, true)
	case "run":
		return a.toolRun(args)
	case "kill":
		return a.toolKill()
	case "help":
		return a.toolHelp()
	default:
		// Try dynamic execution
		return a.toolDynamic(tool, args)
	}
}

func (a *App) securePath(path string) (string, error) {
	fullPath := filepath.Join(a.config.ProjectFolder, path)
	rel, err := filepath.Rel(a.config.ProjectFolder, fullPath)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("access denied: path must be within project folder")
	}
	return fullPath, nil
}

func (a *App) toolFRead(path string) string {
	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if info.Size() > 2*1024*1024 {
		return fmt.Sprintf("This file is %.2fMB, use head or tail.", float64(info.Size())/(1024*1024))
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return string(content)
}

func (a *App) toolFWrite(args string) string {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		return "Error: fwrite requires <path> <content>"
	}
	path := parts[0]
	content := parts[1]

	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	a.backupFile(fullPath)

	err = os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err != nil {
		return fmt.Sprintf("Error creating directories: %v", err)
	}

	err = os.WriteFile(fullPath, []byte(content), 0644)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("File '%s' written successfully.", path)
}

func (a *App) toolRM(path string) string {
	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	a.backupFile(fullPath)

	err = os.Remove(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("File '%s' removed successfully.", path)
}

func (a *App) toolLS(path string) string {
	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	// Support patterns if path contains *
	if strings.Contains(path, "*") || strings.Contains(path, "?") {
		matches, err := filepath.Glob(fullPath)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		if len(matches) == 0 {
			return "No matches found."
		}
		var sb strings.Builder
		for _, match := range matches {
			info, _ := os.Stat(match)
			rel, _ := filepath.Rel(a.config.ProjectFolder, match)
			if info.IsDir() {
				sb.WriteString(fmt.Sprintf("[DIR]  %s\n", rel))
			} else {
				sb.WriteString(fmt.Sprintf("%-6d %s\n", info.Size(), rel))
			}
		}
		return sb.String()
	}

	files, err := os.ReadDir(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var sb strings.Builder
	for _, file := range files {
		info, _ := file.Info()
		if file.IsDir() {
			sb.WriteString(fmt.Sprintf("[DIR]  %s\n", file.Name()))
		} else {
			sb.WriteString(fmt.Sprintf("%-6d %s\n", info.Size(), file.Name()))
		}
	}
	return sb.String()
}

func (a *App) toolNote(note string) string {
	wailsruntime.EventsEmit(a.ctx, "ai-note", note)
	return "Note saved."
}

func (a *App) backupFile(fullPath string) {
	if _, err := os.Stat(fullPath); err != nil {
		return
	}

	trashDir := filepath.Join(a.config.ProjectFolder, "!trash")
	os.MkdirAll(trashDir, 0755)

	timestamp := time.Now().Format("02Jan2006-150405")
	fileName := filepath.Base(fullPath)
	backupPath := filepath.Join(trashDir, fmt.Sprintf("%s.%s", fileName, timestamp))

	input, _ := os.ReadFile(fullPath)
	os.WriteFile(backupPath, input, 0644)
}

func (a *App) toolURL(url string, textOnly bool) string {
	urlDir := filepath.Join(a.config.ProjectFolder, "!url")
	os.MkdirAll(urlDir, 0755)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer resp.Body.Close()

	timestamp := time.Now().Format("02Jan2006-150405")
	safeName := strings.ReplaceAll(strings.ReplaceAll(url, "://", "_"), "/", "_")
	if len(safeName) > 50 {
		safeName = safeName[:50]
	}
	fileName := fmt.Sprintf("%s.%s", safeName, timestamp)
	fullPath := filepath.Join(urlDir, fileName)

	out, err := os.Create(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer out.Close()

	if textOnly {
		body, _ := io.ReadAll(resp.Body)
		content := string(body)
		// Very basic HTML tag removal
		for {
			start := strings.Index(content, "<script")
			if start == -1 {
				break
			}
			end := strings.Index(content[start:], "</script>")
			if end == -1 {
				break
			}
			content = content[:start] + content[start+end+9:]
		}
		for {
			start := strings.Index(content, "<style")
			if start == -1 {
				break
			}
			end := strings.Index(content[start:], "</style>")
			if end == -1 {
				break
			}
			content = content[:start] + content[start+end+8:]
		}

		// Remove tags
		var sb strings.Builder
		inTag := false
		for _, r := range content {
			if r == '<' {
				inTag = true
			} else if r == '>' {
				inTag = false
			} else if !inTag {
				sb.WriteRune(r)
			}
		}

		finalText := strings.TrimSpace(sb.String())
		os.WriteFile(fullPath+".txt", []byte(finalText), 0644)
		return fmt.Sprintf("Saved text to: %s", filepath.Join("!url", fileName+".txt"))
	}

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	return fmt.Sprintf("Saved to: %s", filepath.Join("!url", fileName))
}

var currentProcess *exec.Cmd

func (a *App) toolRun(args string) string {
	if currentProcess != nil && currentProcess.Process != nil {
		return "Error: A process is already running. Kill it first."
	}

	cmdStr := a.config.RunCommand
	if args != "" {
		cmdStr = args
	}

	// Replace placeholders
	cmdStr = strings.ReplaceAll(cmdStr, "{app}", a.config.AppName)
	cmdStr = strings.ReplaceAll(cmdStr, "{user}", a.config.Username)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// This is a simplification. Real runas with password requires more effort.
		// For now we just execute the command.
		cmd = exec.Command("cmd", "/c", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}

	cmd.Dir = a.config.ProjectFolder
	cmd.SysProcAttr = getSysProcAttr()

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		return fmt.Sprintf("Error starting process: %v", err)
	}

	currentProcess = cmd

	go func() {
		// Stream output to chat if needed? The requirement says "messages/prompts/tool replies still scroll in chat"
		// For now just wait and clear currentProcess
		cmd.Wait()
		currentProcess = nil
	}()

	return fmt.Sprintf("Started: %s", cmdStr)
}

func (a *App) toolKill() string {
	if currentProcess == nil || currentProcess.Process == nil {
		// Try running the kill command from config
		cmdStr := strings.ReplaceAll(a.config.KillCommand, "{app}", a.config.AppName)
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", cmdStr)
		}
		cmd.Dir = a.config.ProjectFolder
		err := cmd.Run()
		if err != nil {
			return fmt.Sprintf("Error running kill command: %v", err)
		}
		return "Kill command executed."
	}

	err := currentProcess.Process.Kill()
	if err != nil {
		return fmt.Sprintf("Error killing process: %v", err)
	}
	currentProcess = nil
	return "Process terminated."
}

func (a *App) toolDynamic(tool, args string) string {
	toolsDir := filepath.Join("build", "bin", "tools")
	executable := filepath.Join(toolsDir, tool)
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}

	if _, err := os.Stat(executable); err != nil {
		return fmt.Sprintf("Error: Unknown tool or executable '%s' not found.", tool)
	}

	cmd := exec.Command(executable, strings.Fields(args)...)
	cmd.Dir = a.config.ProjectFolder
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error executing tool '%s': %v\nOutput: %s", tool, err, string(out))
	}
	return string(out)
}

func (a *App) toolHelp() string {
	// Built-in tools
	builtIns := map[string]string{
		"fread":  "Read file content. Usage: fread <path>",
		"fwrite": "Write file content. Usage: fwrite <path> <content>",
		"rm":     "Remove a file. Usage: rm <path>",
		"ls":     "List files. Usage: ls [path]",
		"note":   "Save a note for yourself. Usage: note <text>",
		"url":    "Download URL to file. Usage: url <url>",
		"urltxt": "Download URL as text. Usage: urltxt <url>",
		"run":    "Run the app. Usage: run [args]",
		"kill":   "Kill the app. Usage: kill",
		"help":   "Show this help.",
		"done:":  "Signal completion.",
	}

	tools, _ := a.ListAvailableTools()
	var sb strings.Builder
	sb.WriteString("Available tools:\n")

	for name, desc := range builtIns {
		if a.isToolAllowed(name) {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
		}
	}

	for _, t := range tools {
		if _, exists := builtIns[t.Name]; exists {
			continue // Already listed
		}
		if a.isToolAllowed(t.Name) {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, strings.TrimSpace(t.Description)))
		}
	}
	return sb.String()
}

func (a *App) isToolAllowed(tool string) bool {
	if tool == "help" || tool == "done:" {
		return true
	}
	for _, t := range a.config.AllowedTools {
		if t == tool {
			return true
		}
	}
	return false
}
