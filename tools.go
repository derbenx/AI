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
	toolsDir := filepath.Join(a.getExecDir(), "tools")
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

	builtIns := a.getBuiltInTools()

	var tools []ToolInfo
	seen := make(map[string]bool)

	// Add built-ins first
	for name, defaultDesc := range builtIns {
		desc := []byte(defaultDesc)
		if d, err := os.ReadFile(filepath.Join(toolsDir, name+".txt")); err == nil {
			desc = d
		} else if d, err := os.ReadFile(filepath.Join(toolsDir, name+".json")); err == nil {
			desc = d
		}
		tools = append(tools, ToolInfo{
			Name:        name,
			Description: string(desc),
		})
		seen[name] = true
	}

	// Add dynamic tools
	for _, file := range files {
		if !file.IsDir() && (strings.HasSuffix(file.Name(), ".txt") || strings.HasSuffix(file.Name(), ".json")) {
			name := strings.TrimSuffix(strings.TrimSuffix(file.Name(), ".txt"), ".json")
			if seen[name] {
				continue
			}
			description, _ := os.ReadFile(filepath.Join(toolsDir, file.Name()))
			tools = append(tools, ToolInfo{
				Name:        name,
				Description: string(description),
			})
			seen[name] = true
		}
	}
	return tools, nil
}

func (a *App) ExecuteTool(command string) string {
	command = strings.TrimPrefix(command, ":")
	parts := strings.SplitN(command, " ", 2)
	tool := strings.TrimSuffix(parts[0], ":")
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
	case "todo":
		return a.toolTodo()
	case "update_todo":
		return a.toolUpdateTodo(args)
	case "url":
		return a.toolURL(args, false)
	case "urltxt":
		return a.toolURL(args, true)
	case "run":
		return a.toolRun(args)
	case "kill":
		return a.toolKill()
	case "help":
		return a.toolHelp(args)
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

func (a *App) splitArgs(args string) []string {
	var result []string
	var current strings.Builder
	inQuotes := false
	quoteChar := rune(0)

	for _, r := range args {
		if (r == '"' || r == '\'') && !inQuotes {
			inQuotes = true
			quoteChar = r
		} else if r == quoteChar && inQuotes {
			inQuotes = false
			quoteChar = rune(0)
		} else if r == ' ' && !inQuotes {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

func (a *App) toolFWrite(args string) string {
	parts := a.splitArgs(args)
	if len(parts) < 2 {
		return "Error: fwrite requires <path> <content>. Use quotes if path has spaces."
	}
	path := parts[0]
	content := strings.Join(parts[1:], " ")
	// If the user used quotes for content, it might be better to use the original string slice from first space
	if firstSpace := strings.Index(args, " "); firstSpace != -1 {
		// Attempt to extract content more accurately
		remaining := strings.TrimSpace(args[firstSpace:])
		if strings.HasPrefix(remaining, path) {
			// path was likely quoted, find where it ends
			pathEnd := strings.Index(args, path) + len(path)
			if strings.HasPrefix(args[pathEnd:], "\"") || strings.HasPrefix(args[pathEnd:], "'") {
				pathEnd++
			}
			content = strings.TrimSpace(args[pathEnd:])
		} else {
			content = remaining
		}
	}

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

func (a *App) toolTodo() string {
	// We need a way to get the todo from UI.
	// For now we'll request it via event or just return a placeholder
	// if we haven't implemented GetTodoList yet.
	return a.GetTodoList()
}

func (a *App) toolUpdateTodo(todo string) string {
	a.UpdateTodoList(todo)
	wailsruntime.EventsEmit(a.ctx, "todo-updated", todo)
	return "Todo list updated."
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

	out, err := os.Create(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer out.Close()

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
	toolsDir := filepath.Join(a.getExecDir(), "tools")
	executable := filepath.Join(toolsDir, tool)
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}

	if _, err := os.Stat(executable); err != nil {
		return fmt.Sprintf("Error: Unknown tool or executable '%s' not found.", tool)
	}

	cmd := exec.Command(executable, a.splitArgs(args)...)
	cmd.Dir = a.config.ProjectFolder
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error executing tool '%s': %v\nOutput: %s", tool, err, string(out))
	}
	return string(out)
}

func (a *App) getBuiltInTools() map[string]string {
	return map[string]string{
		"fread":  `{"name": "fread", "description": "Read the content of a file.", "usage": "fread <path>", "parameters": {"path": "Relative path to the file."}}`,
		"fwrite": `{"name": "fwrite", "description": "Write content to a file. Overwrites if exists (with backup).", "usage": "fwrite <path> <content>", "parameters": {"path": "Relative path to the file.", "content": "The text content to write."}}`,
		"rm":     `{"name": "rm", "description": "Remove a file (with backup to !trash).", "usage": "rm <path>", "parameters": {"path": "Relative path to the file."}}`,
		"ls":     `{"name": "ls", "description": "List files and directories in a path or using a glob pattern.", "usage": "ls [path_or_pattern]", "parameters": {"path_or_pattern": "Relative path or glob pattern (e.g., *.go)."}}`,
		"note":   `{"name": "note", "description": "Persist technical data or memory to the UI notes list.", "usage": "note <text>", "parameters": {"text": "The information to persist."}}`,
		"todo":        `{"name": "todo", "description": "Read the current user checklist.", "usage": "todo", "parameters": {}}`,
		"update_todo": `{"name": "update_todo", "description": "Update the user todo checklist in the UI.", "usage": "update_todo <new_todo_content>", "parameters": {"new_todo_content": "The full new content for the todo list."}}`,
		"url":    `{"name": "url", "description": "Download a URL to a file in the !url folder.", "usage": "url <url>", "parameters": {"url": "The full URL to download."}}`,
		"urltxt": `{"name": "urltxt", "description": "Download a URL and extract text to a file in the !url folder.", "usage": "urltxt <url>", "parameters": {"url": "The full URL to process."}}`,
		"run":    `{"name": "run", "description": "Execute the build/run command defined in settings.", "usage": "run [args]", "parameters": {"args": "Optional additional arguments."}}`,
		"kill":   `{"name": "kill", "description": "Terminate the running process.", "usage": "kill", "parameters": {}}`,
		"help":   `{"name": "help", "description": "List available tools or get detailed info for one tool.", "usage": "help [toolname]", "parameters": {"toolname": "Optional tool name to get info for."}}`,
		"done:":  `{"name": "done:", "description": "Signal that the task is complete.", "usage": "done:", "parameters": {}}`,
	}
}

func (a *App) toolHelp(toolname string) string {
	toolsDir := filepath.Join(a.getExecDir(), "tools")
	builtIns := a.getBuiltInTools()

	toolname = strings.TrimSuffix(strings.TrimSpace(toolname), ":")

	if toolname != "" {
		if desc, ok := builtIns[toolname]; ok {
			// Check for override in files
			if d, err := os.ReadFile(filepath.Join(toolsDir, toolname+".txt")); err == nil {
				desc = string(d)
			} else if d, err := os.ReadFile(filepath.Join(toolsDir, toolname+".json")); err == nil {
				desc = string(d)
			}
			return fmt.Sprintf("%s: %s", toolname, desc)
		}
		tools, _ := a.ListAvailableTools()
		for _, t := range tools {
			if t.Name == toolname {
				return fmt.Sprintf("%s: %s", t.Name, strings.TrimSpace(t.Description))
			}
		}
		return fmt.Sprintf("Error: Tool '%s' not found.", toolname)
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
