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
	"encoding/json"

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
	syntax := a.getBuiltInToolSyntax()

	var tools []ToolInfo
	seen := make(map[string]bool)

	// Add built-ins first
	for name, defaultDesc := range builtIns {
		desc := []byte(defaultDesc)
		// For UI tooltips, use syntax if available
		if s, ok := syntax[name]; ok {
			desc = []byte(s)
		}

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

func (a *App) sanitizeOutput(output string) string {
	if a.config.ProjectFolder == "" {
		return output
	}
	// Replace absolute project path with ./
	// First ensure consistent slash direction for the search
	projFolder := filepath.ToSlash(a.config.ProjectFolder)
	if !strings.HasSuffix(projFolder, "/") {
		projFolder += "/"
	}

	sanitized := strings.ReplaceAll(filepath.ToSlash(output), projFolder, "./")
	return sanitized
}

func (a *App) ExecuteTool(command string, isAI bool) string {
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

	toolLower := strings.ToLower(tool)

	// Check if tool is allowed for AI. User has access to all existing tools.
	if isAI && !a.isToolAllowed(toolLower) {
		return fmt.Sprintf("Error: Tool '%s' is not allowed or not found.", tool)
	} else if !isAI && !a.toolExists(toolLower) {
		return fmt.Sprintf("Error: Tool '%s' not found.", tool)
	}

	var output string
	switch toolLower {
	case "fileread":
		output = a.toolFRead(args)
	case "filewrite":
		output = a.toolFWrite(args)
	case "rm":
		output = a.toolRM(args)
	case "ls":
		output = a.toolLS(args)
	case "memories":
		output = a.toolNote(args)
	case "todo":
		output = a.toolTodo(args)
	case "url":
		output = a.toolURL(args, false)
	case "urltxt":
		output = a.toolURL(args, true)
	case "build":
		output = a.toolBuild()
	case "run":
		output = a.toolRun()
	case "kill":
		output = a.toolKill()
	case "lines":
		output = a.toolLines(args)
	case "filecopy":
		output = a.toolFCopy(args)
	case "splicefile":
		output = a.toolSpliceFile(args)
	case "listbots":
		output = a.toolListBots()
	case "mkdir":
		output = a.toolMkdir(args)
	case "help":
		output = a.toolHelp(args, isAI)
	case "done":
		output = a.toolDone(args)
	default:
		// Try dynamic execution
		output = a.toolDynamic(toolLower, args)
	}
	return a.sanitizeOutput(output)
}

func (a *App) securePath(path string) (string, error) {
	// Support Windows paths by converting backslashes and cleaning
	path = filepath.Clean(filepath.FromSlash(strings.ReplaceAll(path, "\\", "/")))
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
		return "Error: fileread requires arguments. See 'help: fileread'"
	}

	var operation, mode, path string
	var count int
	var isAdvanced bool

	if len(parts) >= 3 {
		op := strings.ToLower(parts[0])
		if op == "head" || op == "tail" {
			operation = op
			modeStr := strings.ToLower(parts[1])
			if strings.HasSuffix(modeStr, "b") {
				mode = "bytes"
				count, _ = strconv.Atoi(strings.TrimSuffix(modeStr, "b"))
			} else {
				mode = "lines"
				count, _ = strconv.Atoi(modeStr)
			}
			path = parts[2]
			isAdvanced = true
		}
	}

	if !isAdvanced {
		// Default to all
		path = parts[len(parts)-1]
		operation = "all"
	}

	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if operation == "all" && info.Size() > 2*1024*1024 {
		return fmt.Sprintf("This file is %.2fMB, use head or tail. Example: `fileread: head 50 %s`", float64(info.Size())/(1024*1024), path)
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer file.Close()

	if operation == "head" {
		if mode == "bytes" {
			buf := make([]byte, count)
			n, err := file.Read(buf)
			if err != nil && err != io.EOF {
				return fmt.Sprintf("Error: %v", err)
			}
			return string(buf[:n])
		} else {
			var result []string
			scanner := bufio.NewScanner(file)
			for i := 0; i < count && scanner.Scan(); i++ {
				result = append(result, scanner.Text())
			}
			return strings.Join(result, "\n")
		}
	} else if operation == "tail" {
		if mode == "bytes" {
			size := info.Size()
			if int64(count) > size {
				count = int(size)
			}
			file.Seek(size-int64(count), 0)
			buf := make([]byte, count)
			n, err := file.Read(buf)
			if err != nil && err != io.EOF {
				return fmt.Sprintf("Error: %v", err)
			}
			return string(buf[:n])
		} else {
			// Efficient tail for large files
			size := info.Size()
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
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	// Escape backticks to prevent breaking the markdown block
	safeContent := strings.ReplaceAll(string(content), "```", "` ` `")
	return fmt.Sprintf("File content of `%s`:\n```\n%s\n```", path, safeContent)
}

func (a *App) splitArgs(args string) []string {
	var result []string
	var current strings.Builder
	inQuotes := false
	quoteChar := rune(0)
	escaped := false

	for i, r := range args {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			// Only escape if it's followed by a quote or another backslash
			// This allows backslashes in Windows paths without explicit escaping
			if i+1 < len(args) {
				next := args[i+1]
				if next == '"' || next == '\'' || next == '\\' {
					escaped = true
					continue
				}
			}
		}
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
		return "Error: filewrite requires <operation: replace|append> <path> <content>. See 'help: filewrite'"
	}
	operation := strings.ToLower(parts[0])
	path := parts[1]

	// More robust content extraction: find where the path ends in the original string
	content := ""

	// If path was quoted, we need to find it correctly in the args string
	// This is a bit tricky because splitArgs already processed it.
	// Let's use a simpler approach: join parts[2:] with spaces, but that might lose original spacing.
	// Re-calculating content from raw args:
	searchStart := 0
	opIdx := strings.Index(strings.ToLower(args), operation)
	if opIdx != -1 {
		searchStart = opIdx + len(operation)
		// skip spaces
		for searchStart < len(args) && args[searchStart] == ' ' {
			searchStart++
		}
		// now we are at the path.
		if searchStart < len(args) {
			if args[searchStart] == '"' || args[searchStart] == '\'' {
				quote := args[searchStart]
				searchStart++ // move inside quote
				pathEnd := strings.Index(args[searchStart:], string(quote))
				if pathEnd != -1 {
					searchStart += pathEnd + 1 // move past quote
				}
			} else {
				pathEnd := strings.Index(args[searchStart:], " ")
				if pathEnd != -1 {
					searchStart += pathEnd // move to the space after path
				}
			}
		}
		content = strings.TrimLeft(args[searchStart:], " ")
	} else {
		content = strings.Join(parts[2:], " ")
	}

	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	backupPath, backedUp := a.backupFile(fullPath)

	err = os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err != nil {
		return fmt.Sprintf("Error creating directories: %v", err)
	}

	var writeErr error
	if operation != "replace" && operation != "append" {
		return "Error: filewrite operation must be 'replace' or 'append'. See 'help: filewrite'"
	}

	if operation == "append" {
		f, err := os.OpenFile(fullPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			writeErr = err
		} else {
			if _, err := f.WriteString(content); err != nil {
				writeErr = err
			}
			f.Close()
		}
	} else {
		writeErr = os.WriteFile(fullPath, []byte(content), 0644)
	}

	if writeErr != nil {
		msg := ""
		if backedUp {
			msg = fmt.Sprintf("Existing file was backed up to %s -- Writing data failed: %v.", backupPath, writeErr)
			// Attempt restore
			restoreErr := a.restoreFile(backupPath, fullPath)
			if restoreErr != nil {
				msg += fmt.Sprintf(" Restoring backup failed: %v. Maybe try to rm: %s or fwrite: with a different filename? You could also suggest the AI make a note of the error with a note:.", restoreErr, path)
			} else {
				msg += " Original file restored from backup."
			}
		} else {
			msg = fmt.Sprintf("Error writing to file '%s': %v", path, writeErr)
		}
		return msg
	}

	opDone := operation + "ed"
	if operation == "replace" {
		opDone = "replaced"
	}
	successMsg := fmt.Sprintf("File `%s` %s successfully.", path, opDone)
	if backedUp {
		successMsg += fmt.Sprintf(" (Backup saved to `%s`)", backupPath)
	}
	return successMsg
}

func (a *App) restoreFile(relBackupPath, fullTargetPath string) error {
	fullBackupPath, err := a.securePath(relBackupPath)
	if err != nil {
		return err
	}

	src, err := os.Open(fullBackupPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(fullTargetPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func (a *App) toolRM(path string) string {
	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	backupPath, backedUp := a.backupFile(fullPath)

	err = os.Remove(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	msg := fmt.Sprintf("File `%s` removed successfully.", path)
	if backedUp {
		msg += fmt.Sprintf(" (Backup saved to `%s`)", backupPath)
	}
	return msg
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

	stat, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	if !stat.IsDir() {
		return fmt.Sprintf("Listing files:\n %-6d %s", stat.Size(), filepath.Base(fullPath))
	}

	files, err := os.ReadDir(fullPath)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("Listing files:\n")
	for _, file := range files {
		info, _ := file.Info()
		if file.IsDir() {
			sb.WriteString(fmt.Sprintf(" [DIR]  %s\n", file.Name()))
		} else {
			sb.WriteString(fmt.Sprintf(" %-6d %s\n", info.Size(), file.Name()))
		}
	}
	return sb.String()
}

func (a *App) toolNote(args string) string {
	parts := a.splitArgs(args)
	content := a.GetAINotes()
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}

	if len(parts) == 0 || parts[0] == "0" {
		if content == "" {
			return "No notes found."
		}
		return content
	}

	id, err := strconv.Atoi(parts[0])
	if err != nil || id < 1 || id > 1000 {
		return fmt.Sprintf("Error: Invalid memory ID. ID must be between 1 and 1000. See 'help: memories'")
	}

	if len(parts) > 1 {
		newNote := strings.Join(parts[1:], " ")
		// Remove linebreaks to keep note on a single line
		newNote = strings.ReplaceAll(strings.ReplaceAll(newNote, "\n", " "), "\r", " ")

		if id <= len(lines) {
			lines[id-1] = newNote
		} else {
			// Pad with empty lines if necessary
			for len(lines) < id-1 {
				lines = append(lines, "")
			}
			lines = append(lines, newNote)
		}
		newContent := strings.Join(lines, "\n")
		a.UpdateAINotes(newContent)
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "notes-updated", newContent)
		}
		return fmt.Sprintf("Note %d updated.", id)
	}

	if id > len(lines) {
		return fmt.Sprintf("Note %d: ", id)
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
	return fmt.Sprintf("%d. You can read it with `fileread: %s`", count, path)
}

func (a *App) toolFCopy(args string) string {
	parts := a.splitArgs(args)
	if len(parts) < 2 {
		return "Error: filecopy requires <src> <dst>"
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

func (a *App) toolListBots() string {
	if len(a.config.Bots) == 0 {
		return "No bots configured."
	}
	var sb strings.Builder
	sb.WriteString("@all\n")
	for i, bot := range a.config.Bots {
		model, _ := a.GetBotModel(i)
		sb.WriteString(fmt.Sprintf("@%s (%s)\n", bot.Name, model))
	}
	return sb.String()
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
	return fmt.Sprintf("Directory `%s` created. Use `ls: %s` to see it.", path, path)
}

func (a *App) toolSpliceFile(args string) string {
	parts := a.splitArgs(args)
	if len(parts) < 4 {
		return "Error: splicefile requires <start_line> <end_line> <file> <content>. See 'help: splicefile'"
	}

	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return "Error: start_line must be a number."
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		return "Error: end_line must be a number."
	}
	path := parts[2]

	// Content is everything after the 3rd argument (path).
	// Since path might be quoted, we need a reliable way to find the end of the path in the raw args string.
	content := ""

	// We'll re-parse the raw string to find where the 3rd argument ends.
	// We skip over the first 3 tokens.
	tokenCount := 0
	inQuotes := false
	quoteChar := rune(0)
	escaped := false

	searchIdx := 0
	for i, r := range args {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			if i+1 < len(args) {
				next := args[i+1]
				if next == '"' || next == '\'' || next == '\\' {
					escaped = true
					continue
				}
			}
		}

		if (r == '"' || r == '\'') && !inQuotes {
			inQuotes = true
			quoteChar = r
		} else if r == quoteChar && inQuotes {
			inQuotes = false
			quoteChar = rune(0)
		} else if r == ' ' && !inQuotes {
			// Found end of a token
			if i > 0 && args[i-1] != ' ' {
				tokenCount++
				if tokenCount == 3 {
					searchIdx = i + 1
					break
				}
			}
		}
	}

	// If it reached the end of the string while parsing the 3rd token
	if tokenCount < 3 {
		tokenCount++
		if tokenCount == 3 {
			// No content provided?
			searchIdx = len(args)
		}
	}

	content = strings.TrimLeft(args[searchIdx:], " ")
	if content == "" && len(parts) >= 4 {
		content = strings.Join(parts[3:], " ")
	}

	fullPath, err := a.securePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err)
	}

	lines := strings.Split(string(data), "\n")

	// Adjust 1-based indexing to 0-based
	startIdx0 := start - 1
	endIdx0 := end - 1

	if startIdx0 < 0 { startIdx0 = 0 }
	if endIdx0 < startIdx0 - 1 { endIdx0 = startIdx0 - 1 }
	if startIdx0 > len(lines) { startIdx0 = len(lines) }
	if endIdx0 >= len(lines) { endIdx0 = len(lines) - 1 }

	newLines := make([]string, 0)
	newLines = append(newLines, lines[:startIdx0]...)
	if content != "" {
		newLines = append(newLines, strings.Split(content, "\n")...)
	}
	newLines = append(newLines, lines[endIdx0+1:]...)

	backupPath, backedUp := a.backupFile(fullPath)
	err = os.WriteFile(fullPath, []byte(strings.Join(newLines, "\n")), 0644)
	if err != nil {
		if backedUp {
			a.restoreFile(backupPath, fullPath)
		}
		return fmt.Sprintf("Error writing file: %v", err)
	}

	msg := fmt.Sprintf("File `%s` spliced successfully.", path)
	if backedUp {
		msg += fmt.Sprintf(" (Backup saved to `%s`)", backupPath)
	}
	return msg
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
		return fmt.Sprintf("Error: Invalid line ID. Number expected for first parameter. See 'help: todo'")
	}

	if len(parts) > 1 {
		action := strings.ToLower(parts[1])
		if action == "done" {
			if !strings.HasSuffix(lines[id-1], " [done]") {
				lines[id-1] = strings.TrimSpace(lines[id-1]) + " [done]"
				newContent := strings.Join(lines, "\n")
				a.UpdateTodoList(newContent)
				if a.ctx != nil {
					wailsruntime.EventsEmit(a.ctx, "todo-updated", newContent)
				}
				return fmt.Sprintf("Task %d marked as done.", id)
			}
			return fmt.Sprintf("Task %d is already done.", id)
		} else if action == "reset" {
			if strings.HasSuffix(lines[id-1], " [done]") {
				lines[id-1] = strings.TrimSuffix(lines[id-1], " [done]")
				newContent := strings.Join(lines, "\n")
				a.UpdateTodoList(newContent)
				if a.ctx != nil {
					wailsruntime.EventsEmit(a.ctx, "todo-updated", newContent)
				}
				return fmt.Sprintf("Task %d reset.", id)
			}
			return fmt.Sprintf("Task %d was not marked as done.", id)
		}
	}

	return fmt.Sprintf("Line %d: %s", id, lines[id-1])
}

func (a *App) backupFile(fullPath string) (string, bool) {
	if _, err := os.Stat(fullPath); err != nil {
		return "", false
	}

	trashDir := filepath.Join(a.config.ProjectFolder, "!trash")
	os.MkdirAll(trashDir, 0755)

	timestamp := time.Now().Format("02Jan2006-150405")
	fileName := filepath.Base(fullPath)
	backupPath := filepath.Join(trashDir, fmt.Sprintf("%s.%s", fileName, timestamp))

	src, err := os.Open(fullPath)
	if err != nil {
		return "", false
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return "", false
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		return "", false
	}

	rel, _ := filepath.Rel(a.config.ProjectFolder, backupPath)
	return filepath.ToSlash(rel), true
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
		relPath := filepath.ToSlash(filepath.Join("!url", fileName+".txt"))
		os.WriteFile(fullPath+".txt", []byte(finalText), 0644)
		return fmt.Sprintf("Saved text to: `%s`. You can read it with `fileread: %s`", relPath, relPath)
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

	relPath := filepath.ToSlash(filepath.Join("!url", fileName))
	return fmt.Sprintf("Saved to: `%s`. You can read it with `fileread: %s`", relPath, relPath)
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
		return fmt.Sprintf("Build failed: %v. To see errors, use: fileread: build.log", err)
	}

	return "Done. To see errors, use: fileread: build.log"
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

	return "Done. To see errors, use: fileread: run.log"
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
		"fileread":  "Reads a file on disk. Relative path only.\n Usage; fileread: [head/tail] [lines/bytes/all] [file]\n Example; fileread: head 10 pop.go (Reads the top 10 lines of the file).\n Example; fileread: tail 100b hop/pop.go (Reads the last 100 bytes of the file).",
		"filewrite": "modifies or overwrites a file. Relative path only.\n Usage; filewrite: [replace/append] [file] [content]\n Example; filewrite: replace go.json {stuff:ok}\n Example; filewrite: append \"hi lo/go.json\" {stuff:no}",
		"rm":        "remove/delete a file. Relative path only.\n Usage; rm: [file]",
		"ls":        "list files in specified folder. Relative path only.\n Usage; ls: [file]",
		"lines":     "number of lines in a file. Relative path only.\n Usage; lines: [file]\n Example; lines: url/web.htm",
		"filecopy":   "Copy a file. Relative path only.\n Usage; filecopy: [orgfile] [destfile]",
		"splicefile": "Splice/replace lines in a file. 1-indexed. Relative path only.\n Usage; splicefile: [start] [end] [file] [content]\n Example; splicefile: 5 10 \"test.txt\" \"new content for lines 5-10\"",
		"mkdir":      "Make a folder. Relative path only.\n Usage; mkdir: [folder]",
		"memories":  "reads a persistent technical note.\n Usage; memories: [1-999] {note}\n Example; memories: 0 (Read all notes)\n Example; memories: 1 (Read note #1)\n Example; memories: 2 I'd rather be at the beach (saves note to slot #2)",
		"todo":      "Manages a user made to-do list as a checklist\n Usage; todo: [1-999] {done/reset}\n Example; todo: 0 (shows entire list)\n Example; todo: 1 (shows task #1)\n Example; todo: 2 done (appends [done] to the end of line #2)\n Example; todo: 3 reset (removes [done] from the end of line #3)",
		"url":       "downloads full HTML from url.\n Usage; url: [fullurl]",
		"urltxt":    "downloads full HTML and strips html tags.\n Usage; urltxt: [fullurl]",
		"build":     "runs a preset build command. returns log location to check for errors.\n Usage; build:",
		"run":       "runs a preset run command. returns log location to check for errors.\n Usage; run:",
		"kill":      "runs a preset kill command for running program.\n Usage; kill:",
		"help":      "lists info about other tools/commands.\n Usage; help: [tool]\n Example; help: (lists all tool names)\n Example; help: memories (describes how to use memories)",
		"listbots":  "Lists all configured AI bots.\n Usage; listbots:",
		"resume":    "resume code session, user use only\n Usage; resume: [message]",
		"done":      "you are done coding.\n Usage; done: [msg]\n Example; done: I have finished the todo list!",
	}
}

func (a *App) getBuiltInToolSyntax() map[string]string {
	return map[string]string{}
}

func (a *App) toolDone(args string) string {
	if args == "" {
		return "Finished coding."
	}
	return args
}

func (a *App) toolHelp(toolname string, isAI bool) string {
	toolsDir := filepath.Join(a.getExecDir(), "tools")
	builtIns := a.getBuiltInTools()

	toolname = strings.TrimSuffix(strings.TrimSpace(toolname), ":")

	if toolname == "brief_list" {
		// Dynamic Jinja compatible JSON tool listing if requested by prompt
		type ToolDef struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		var toolDefs []ToolDef

		for name, desc := range builtIns {
			if isAI && !a.isToolAllowed(name) {
				continue
			}
			firstLine := strings.Split(desc, "\n")[0]
			summary := strings.TrimSpace(strings.TrimSuffix(firstLine, "."))
			toolDefs = append(toolDefs, ToolDef{Name: name, Description: summary})
		}
		tools, _ := a.ListAvailableTools()
		for _, t := range tools {
			if _, exists := builtIns[t.Name]; exists {
				continue
			}
			if isAI && !a.isToolAllowed(t.Name) {
				continue
			}
			summary := strings.TrimSpace(strings.Split(t.Description, "\n")[0])
			toolDefs = append(toolDefs, ToolDef{Name: t.Name, Description: summary})
		}

		jsonBytes, _ := json.MarshalIndent(toolDefs, "", "  ")
		return string(jsonBytes)
	}

	if toolname != "" {
		if desc, ok := builtIns[toolname]; ok {
			// If AI is asking for help on a tool it's not allowed to use, we should probably tell it it's not found or not allowed.
			if isAI && !a.isToolAllowed(toolname) {
				return fmt.Sprintf("Error: Tool '%s' is not allowed.", toolname)
			}

			// Check for override in files
			if d, err := os.ReadFile(filepath.Join(toolsDir, toolname+".txt")); err == nil {
				desc = string(d)
			} else if d, err := os.ReadFile(filepath.Join(toolsDir, toolname+".json")); err == nil {
				desc = string(d)
			}

			return fmt.Sprintf("[Help for %s:]  %s", strings.Title(toolname), desc)
		}
		tools, _ := a.ListAvailableTools()
		for _, t := range tools {
			if t.Name == toolname {
				if isAI && !a.isToolAllowed(toolname) {
					continue
				}
				return fmt.Sprintf("[Help for %s:]  %s", strings.Title(t.Name), t.Description)
			}
		}
		return fmt.Sprintf("Error: Tool '%s' not found.", toolname)
	}

	tools, _ := a.ListAvailableTools()
	var names []string

	for name := range builtIns {
		if isAI {
			if a.isToolAllowed(name) {
				names = append(names, name)
			}
		} else {
			names = append(names, name)
		}
	}

	for _, t := range tools {
		if _, exists := builtIns[t.Name]; exists {
			continue // Already listed
		}
		if isAI {
			if a.isToolAllowed(t.Name) {
				names = append(names, t.Name)
			}
		} else {
			names = append(names, t.Name)
		}
	}
	return strings.Join(names, ", ") + " (use 'help: toolname' for details)"
}

func (a *App) isToolAllowed(tool string) bool {
	tool = strings.ToLower(tool)
	// help, todo and done cannot be disabled for AI.
	// memories (formerly note) can now be disabled as per user request.
	if tool == "help" || tool == "todo" || tool == "done" {
		return true
	}
	// resume is user only.
	if tool == "resume" {
		return false
	}
	for _, t := range a.config.AllowedTools {
		if strings.ToLower(t) == tool {
			return true
		}
	}
	return false
}

func (a *App) toolExists(tool string) bool {
	tool = strings.ToLower(tool)
	if tool == "resume" {
		return true
	}
	builtIns := a.getBuiltInTools()
	if _, ok := builtIns[tool]; ok {
		return true
	}

	toolsDir := filepath.Join(a.getExecDir(), "tools")
	executable := filepath.Join(toolsDir, tool)
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if _, err := os.Stat(executable); err == nil {
		return true
	}

	return false
}

func (a *App) GetToolDefinitions() []any {
	var tools []any

	// fileread
	if a.isToolAllowed("fileread") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "fileread",
				"description": "Reads a file with optional head/tail constraints.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"filename": map[string]any{"type": "string"},
						"location": map[string]any{
							"type":        "string",
							"enum":        []string{"head", "tail", "all"},
							"description": "Read from start (head), end (tail), or entire file (all).",
						},
						"amount": map[string]any{
							"type":        "string",
							"description": "Amount to read. E.g., '10' for 10 lines, '100b' for 100 bytes. Ignored if location is 'all'.",
						},
					},
					"required": []string{"filename", "location"},
				},
			},
		})
	}

	// filewrite
	if a.isToolAllowed("filewrite") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "filewrite",
				"description": "Creates or modifies a file.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"filename": map[string]any{"type": "string"},
						"operation": map[string]any{
							"type":        "string",
							"enum":        []string{"replace", "append"},
							"description": "Use 'replace' to overwrite or 'append' to add to the end.",
						},
						"content": map[string]any{"type": "string"},
					},
					"required": []string{"filename", "operation", "content"},
				},
			},
		})
	}

	// rm
	if a.isToolAllowed("rm") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "rm",
				"description": "Deletes a file.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"filename": map[string]any{"type": "string"},
					},
					"required": []string{"filename"},
				},
			},
		})
	}

	// ls
	if a.isToolAllowed("ls") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "ls",
				"description": "Lists files in a directory or matches a glob pattern.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string", "description": "Directory path or glob pattern (e.g. 'src/*.go')"},
					},
					"required": []string{"path"},
				},
			},
		})
	}

	// lines
	if a.isToolAllowed("lines") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "lines",
				"description": "Returns the number of lines in a file.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"filename": map[string]any{"type": "string"},
					},
					"required": []string{"filename"},
				},
			},
		})
	}

	// filecopy
	if a.isToolAllowed("filecopy") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "filecopy",
				"description": "Copies a file from source to destination.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"src": map[string]any{"type": "string"},
						"dst": map[string]any{"type": "string"},
					},
					"required": []string{"src", "dst"},
				},
			},
		})
	}

	// splicefile
	if a.isToolAllowed("splicefile") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "splicefile",
				"description": "Replaces a range of lines in a file with new content.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"start_line": map[string]any{"type": "integer", "description": "1-indexed starting line number"},
						"end_line":   map[string]any{"type": "integer", "description": "1-indexed ending line number"},
						"filename":   map[string]any{"type": "string"},
						"content":    map[string]any{"type": "string"},
					},
					"required": []string{"start_line", "end_line", "filename", "content"},
				},
			},
		})
	}

	// mkdir
	if a.isToolAllowed("mkdir") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "mkdir",
				"description": "Creates a new directory.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
					"required": []string{"path"},
				},
			},
		})
	}

	// memories
	if a.isToolAllowed("memories") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "memories",
				"description": "Reads or updates persistent technical notes.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":      map[string]any{"type": "integer", "description": "Note ID (1-1000). Use 0 to read all."},
						"content": map[string]any{"type": "string", "description": "New content to save. Omit to just read."},
					},
					"required": []string{"id"},
				},
			},
		})
	}

	// todo
	if a.isToolAllowed("todo") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "todo",
				"description": "Manages the to-do list checklist.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":     map[string]any{"type": "integer", "description": "Task ID (line number). Use 0 to show all."},
						"action": map[string]any{"type": "string", "enum": []string{"read", "done", "reset"}, "description": "Action to perform: 'read' (default), 'done', or 'reset'."},
					},
					"required": []string{"id"},
				},
			},
		})
	}

	// url
	if a.isToolAllowed("url") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "url",
				"description": "Downloads full HTML from a URL.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{"type": "string"},
					},
					"required": []string{"url"},
				},
			},
		})
	}

	// urltxt
	if a.isToolAllowed("urltxt") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "urltxt",
				"description": "Downloads content from a URL and strips HTML tags.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{"type": "string"},
					},
					"required": []string{"url"},
				},
			},
		})
	}

	// build
	if a.isToolAllowed("build") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "build",
				"description": "Runs the preset build command.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		})
	}

	// run
	if a.isToolAllowed("run") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "run",
				"description": "Runs the preset run command.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		})
	}

	// kill
	if a.isToolAllowed("kill") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "kill",
				"description": "Kills the currently running process.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		})
	}

	// listbots
	if a.isToolAllowed("listbots") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "listbots",
				"description": "Lists all configured AI bots.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		})
	}

	// done
	if a.isToolAllowed("done") {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "done",
				"description": "Signals that you have finished the task.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"message": map[string]any{"type": "string", "description": "Final summary message."},
					},
					"required": []string{"message"},
				},
			},
		})
	}

	return tools
}
