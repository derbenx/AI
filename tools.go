package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	command = strings.TrimSpace(strings.TrimPrefix(command, ":"))

	var tool, args string
	if colonIdx := strings.Index(command, ":"); colonIdx != -1 {
		tool = strings.TrimSpace(command[:colonIdx])
		args = strings.TrimSpace(command[colonIdx+1:])
	} else {
		parts := strings.SplitN(command, " ", 2)
		tool = parts[0]
		if len(parts) > 1 {
			args = parts[1]
		}
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
		return a.toolTodo(args)
	case "url":
		return a.toolURL(args, false)
	case "urltxt":
		return a.toolURL(args, true)
	case "build":
		return a.toolBuild()
	case "run":
		return a.toolRun()
	case "kill":
		return a.toolKill()
	case "lines":
		return a.toolLines(args)
	case "fcopy":
		return a.toolFCopy(args)
	case "mkdir":
		return a.toolMkdir(args)
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

func (a *App) toolFRead(args string) string {
	parts := a.splitArgs(args)
	if len(parts) == 0 {
		return "Error: fread requires a file path."
	}
	path := parts[0]
	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	operation := ""
	count := 0
	if len(parts) > 2 {
		operation = strings.ToLower(parts[1])
		count, _ = strconv.Atoi(parts[2])
	}

	if info.Size() > 2*1024*1024 && operation == "" {
		return fmt.Sprintf("This file is %.2fMB, use head or tail.", float64(info.Size())/(1024*1024))
	}

	if operation == "head" || operation == "tail" {
		if count <= 0 {
			count = 50 // Default
		}
		file, err := os.Open(fullPath)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		defer file.Close()

		var result []string
		if operation == "head" {
			scanner := bufio.NewScanner(file)
			for i := 0; i < count && scanner.Scan(); i++ {
				result = append(result, scanner.Text())
			}
		} else {
			// Efficient tail for large files
			stat, _ := file.Stat()
			size := stat.Size()
			bufSize := int64(64 * 1024) // 64KB buffer
			if bufSize > size {
				bufSize = size
			}

			foundLines := 0
			pos := size
			var finalLines []string

			for pos > 0 && foundLines <= count {
				readSize := bufSize
				if pos < bufSize {
					readSize = pos
				}
				pos -= readSize
				file.Seek(pos, 0)
				buf := make([]byte, readSize)
				file.Read(buf)

				text := string(buf)
				lines := strings.Split(text, "\n")

				for i := len(lines) - 1; i >= 0; i-- {
					// Skip the very last newline if it's the end of file
					if pos+readSize == size && i == len(lines)-1 && lines[i] == "" {
						continue
					}

					finalLines = append([]string{lines[i]}, finalLines...)
					foundLines++
					if foundLines > count {
						break
					}
				}
			}
			if len(finalLines) > count {
				finalLines = finalLines[len(finalLines)-count:]
			}
			return strings.Join(finalLines, "\n")
		}
		return strings.Join(result, "\n")
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
	if len(parts) < 3 {
		return "Error: fwrite requires <path> <operation: write|append> <content>. Use quotes if path has spaces."
	}
	path := parts[0]
	operation := strings.ToLower(parts[1])

	// More robust content extraction: find where the operation ends in the original string
	// We look for the operation word that is NOT inside the quoted path
	content := ""
	opIndex := -1

	// If path was quoted, it will be parts[0]. Find its end in args.
	searchStart := 0
	if strings.Contains(args, path) {
		searchStart = strings.Index(args, path) + len(path)
		if searchStart < len(args) && (args[searchStart] == '"' || args[searchStart] == '\'') {
			searchStart++
		}
	}

	opIndex = strings.Index(strings.ToLower(args[searchStart:]), operation)
	if opIndex != -1 {
		opEnd := searchStart + opIndex + len(operation)
		content = strings.TrimLeft(args[opEnd:], " ")
	} else {
		content = strings.Join(parts[2:], " ")
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

	if operation == "append" {
		f, err := os.OpenFile(fullPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return fmt.Sprintf("File '%s' appended successfully.", path)
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

func (a *App) toolNote(args string) string {
	parts := a.splitArgs(args)
	content := a.GetAINotes()
	lines := strings.Split(content, "\n")

	if len(parts) == 0 || parts[0] == "0" {
		return content
	}

	id, err := strconv.Atoi(parts[0])
	if err != nil || id < 1 || id > len(lines)+1 {
		return fmt.Sprintf("Error: Invalid note ID. List has %d lines.", len(lines))
	}

	if len(parts) > 1 {
		newNote := strings.Join(parts[1:], " ")
		if id <= len(lines) {
			lines[id-1] = newNote
		} else {
			lines = append(lines, newNote)
		}
		newContent := strings.Join(lines, "\n")
		a.UpdateAINotes(newContent)
		wailsruntime.EventsEmit(a.ctx, "notes-updated", newContent)
		return fmt.Sprintf("Note %d updated.", id)
	}

	if id > len(lines) {
		return "Error: Note ID does not exist."
	}
	return fmt.Sprintf("Note %d: %s", id, lines[id-1])
}

func (a *App) toolLines(path string) string {
	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	return fmt.Sprintf("%d", count)
}

func (a *App) toolFCopy(args string) string {
	parts := a.splitArgs(args)
	if len(parts) < 2 {
		return "Error: fcopy requires <src> <dst>"
	}
	src, err := a.securePath(parts[0])
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	dst, err := a.securePath(parts[1])
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	s, err := os.Open(src)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer d.Close()

	_, err = io.Copy(d, s)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Copied '%s' to '%s'.", parts[0], parts[1])
}

func (a *App) toolMkdir(path string) string {
	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	err = os.MkdirAll(fullPath, 0755)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Directory '%s' created.", path)
}

func (a *App) toolTodo(args string) string {
	parts := a.splitArgs(args)
	content := a.GetTodoList()
	lines := strings.Split(content, "\n")

	if len(parts) == 0 || parts[0] == "0" {
		return content
	}

	id, err := strconv.Atoi(parts[0])
	if err != nil || id < 1 || id > len(lines) {
		return fmt.Sprintf("Error: Invalid line ID. List has %d lines.", len(lines))
	}

	if len(parts) > 1 {
		action := strings.ToLower(parts[1])
		if action == "started" || action == "done" {
			lines[id-1] = strings.TrimSpace(lines[id-1]) + " [" + action + "]"
			newContent := strings.Join(lines, "\n")
			a.UpdateTodoList(newContent)
			wailsruntime.EventsEmit(a.ctx, "todo-updated", newContent)
			return fmt.Sprintf("Task %d marked as %s.", id, action)
		}
	}

	return fmt.Sprintf("Line %d: %s", id, lines[id-1])
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

	src, err := os.Open(fullPath)
	if err != nil {
		return
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return
	}
	defer dst.Close()

	io.Copy(dst, src)
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

func (a *App) toolBuild() string {
	cmdStr := strings.ReplaceAll(a.config.BuildCommand, "{app}", a.config.AppName)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}

	cmd.Dir = a.config.ProjectFolder
	cmd.SysProcAttr = getSysProcAttr()

	logPath := filepath.Join(a.config.ProjectFolder, "build.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Sprintf("Error creating build.log: %v", err)
	}
	defer logFile.Close()

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err = cmd.Run()
	if err != nil {
		return fmt.Sprintf("Build failed: %v. Look at 'build.log' for errors.", err)
	}

	return "Done, look at 'build.log' for errors."
}

func (a *App) toolRun() string {
	if currentProcess != nil && currentProcess.Process != nil {
		return "Error: A process is already running. Kill it first."
	}

	cmdStr := strings.ReplaceAll(a.config.RunCommand, "{app}", a.config.AppName)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}

	cmd.Dir = a.config.ProjectFolder
	cmd.SysProcAttr = getSysProcAttr()

	logPath := filepath.Join(a.config.ProjectFolder, "run.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Sprintf("Error creating run.log: %v", err)
	}
	// Note: We don't close logFile here because cmd.Start uses it in background

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err = cmd.Start()
	if err != nil {
		logFile.Close()
		return fmt.Sprintf("Error starting process: %v", err)
	}

	currentProcess = cmd

	go func() {
		defer logFile.Close()
		cmd.Wait()
		currentProcess = nil
	}()

	// Wait a few seconds to collect initial data
	time.Sleep(3 * time.Second)

	return "Done, look at 'run.log' for errors."
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
		"fread":  `{"tool_name": "fread", "description": "Reads file content. If the file is large, the system will reject a full read; use 'head' or 'tail' to retrieve specific segments.", "parameters": {"type": "object", "properties": {"file_path": {"type": "string", "description": "Relative path to the file (e.g., 'main.go' or 'logs/build.log')."}, "operation": {"type": "string", "enum": ["tail", "head"], "description": "Optional: Read from the top (head) or bottom (tail) of the file."}, "count": {"type": "integer", "minimum": 1, "maximum": 1000, "description": "Number of lines to read when using head or tail."}}, "required": ["file_path"]}}`,
		"fwrite": `{"tool_name": "fwrite", "description": "Modifies or overwrites the content of a specified file", "parameters": {"type": "object", "properties": {"file_path": {"type": "string", "description": "The relative path to the file to be modified (e.g., 'src/main.py')."}, "operation": {"type": "string", "enum": ["write", "append"], "description": "The action to perform on the file. write replaces, append adds to the end."}, "content": {"type": "string", "description": "The new content that should be written to the file."}}, "required": ["file_path", "operation", "content"]}}`,
		"rm":     `{"tool_name": "rm", "description": "Remove a file (with backup to !trash).", "usage": "rm <path>", "parameters": {"path": "Relative path to the file."}}`,
		"ls":     `{"tool_name": "ls", "description": "List files and directories in a path or using a glob pattern.", "usage": "ls [path_or_pattern]", "parameters": {"path_or_pattern": "Relative path or glob pattern (e.g., *.go)."}}`,
		"lines":  `{"tool_name": "lines", "description": "Count the number of lines in a file.", "usage": "lines <path>", "parameters": {"path": "Relative path to the file."}}`,
		"fcopy":  `{"tool_name": "fcopy", "description": "Copy or rename a file.", "usage": "fcopy <src> <dst>", "parameters": {"src": "Source path.", "dst": "Destination path."}}`,
		"mkdir":  `{"tool_name": "mkdir", "description": "Create a new directory.", "usage": "mkdir <path>", "parameters": {"path": "Path to create."}}`,
		"note":   `{"tool_name": "note", "description": "Writes or retrieves persistent technical notes to assist with AI long-term memory.", "parameters": {"type": "object", "properties": {"id": {"type": "integer", "description": "The line number for the note. 0 is used to read the entire list."}, "content": {"type": "string", "description": "The text to be saved, required only for 'write' action."}}, "required": ["id"]}}`,
		"todo":   `{"tool_name": "todo", "description": "Manages the users project checklist to track progress and prevent task drift.", "parameters": {"type": "object", "properties": {"id": {"type": "integer", "description": "The specific line number to use. Use 0 to read the entire list."}, "action": {"type": "string", "enum": ["started", "done"], "description": "started and done append that word to the line like a checklist."}}, "required": ["id"]}}`,
		"url":    `{"tool_name": "url", "description": "Download a URL to a file in the !url folder.", "usage": "url <url>", "parameters": {"url": "The full URL to download."}}`,
		"urltxt": `{"tool_name": "urltxt", "description": "Download a URL and extract text to a file in the !url folder.", "usage": "urltxt <url>", "parameters": {"url": "The full URL to process."}}`,
		"build":  `{"tool_name": "build", "description": "Execute the build command defined in settings. Captures output to build.log.", "usage": "build", "parameters": {}}`,
		"run":    `{"tool_name": "run", "description": "Execute the run command defined in settings. Captures output to run.log and waits 3 seconds.", "usage": "run", "parameters": {}}`,
		"kill":   `{"tool_name": "kill", "description": "Terminate the running process using the kill command defined in settings.", "usage": "kill", "parameters": {}}`,
		"help":   `{"tool_name": "help", "description": "List available tools or get detailed info for one tool.", "usage": "help [toolname]", "parameters": {"toolname": "Optional tool name to get info for."}}`,
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
