package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestApp(t *testing.T) (*App, string) {
	tempDir, err := os.MkdirTemp("", "aicoder_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	app := &App{
		config: Config{
			ProjectFolder: tempDir,
			AllowedTools: []string{
				"ls", "filewrite", "fileread", "rm", "memories", "todo", "lines", "filecopy", "mkdir", "url", "urltxt", "help",
			},
		},
	}
	return app, tempDir
}

func TestToolExists(t *testing.T) {
	app, _ := setupTestApp(t)
	tools := []string{"ls", "fileread", "filewrite", "rm", "memories", "todo", "help", "resume", "done"}
	for _, tool := range tools {
		if !app.toolExists(tool) {
			t.Errorf("Tool %s should exist", tool)
		}
	}
}

func TestToolLS(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("content"), 0644)
	os.Mkdir(filepath.Join(tempDir, "dir1"), 0755)

	output := app.ExecuteTool("ls: .", false)
	if !strings.Contains(output, "file1.txt") {
		t.Errorf("Expected file1.txt in output, got: %s", output)
	}
	if !strings.Contains(output, "[DIR]  dir1") {
		t.Errorf("Expected [DIR]  dir1 in output, got: %s", output)
	}
}

func TestToolFWriteAndBackup(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	filePath := "test.txt"
	fullPath := filepath.Join(tempDir, filePath)

	// First write
	app.ExecuteTool("filewrite: replace " + filePath + " initial content", false)

	// Second write (should trigger backup)
	output := app.ExecuteTool("filewrite: replace " + filePath + " updated content", false)
	if !strings.Contains(output, "(Backup saved to `!trash/") {
		t.Errorf("Expected backup message with backticks, got: %s", output)
	}

	// Verify backup exists
	trashDir := filepath.Join(tempDir, "!trash")
	files, _ := os.ReadDir(trashDir)
	if len(files) != 1 {
		t.Errorf("Expected 1 backup file, found %d", len(files))
	}

	// Verify content
	content, _ := os.ReadFile(fullPath)
	if string(content) != "updated content" {
		t.Errorf("Expected 'updated content', got '%s'", string(content))
	}
}

func TestToolFRead(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	filePath := "test.txt"
	os.WriteFile(filepath.Join(tempDir, filePath), []byte("line1\nline2\nline3\nline4\nline5"), 0644)

	// Test head lines
	output := app.ExecuteTool("fileread: head 2 " + filePath, false)
	if output != "line1\nline2" {
		t.Errorf("Expected 'line1\nline2', got: %q", output)
	}

	// Test tail lines
	output = app.ExecuteTool("fileread: tail 2 " + filePath, false)
	if output != "line4\nline5" {
		t.Errorf("Expected 'line4\nline5', got: %q", output)
	}

	// Test head bytes
	output = app.ExecuteTool("fileread: head 5b " + filePath, false)
	if output != "line1" {
		t.Errorf("Expected 'line1', got: %q", output)
	}

	// Test tail bytes
	output = app.ExecuteTool("fileread: tail 5b " + filePath, false)
	if output != "line5" {
		t.Errorf("Expected 'line5', got: %q", output)
	}

	// Test all
	output = app.ExecuteTool("fileread: all " + filePath, false)
	if !strings.Contains(output, "line1\nline2\nline3\nline4\nline5") {
		t.Errorf("Expected all content, got: %s", output)
	}
}

func TestToolFReadLargeFile(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	filePath := "large.txt"
	fullPath := filepath.Join(tempDir, filePath)

	// Create a file > 2MB
	data := make([]byte, 2*1024*1024 + 1024)
	os.WriteFile(fullPath, data, 0644)

	output := app.ExecuteTool("fileread: all " + filePath, false)
	if !strings.Contains(output, "use head or tail") || !strings.Contains(output, "Example: `fileread:") {
		t.Errorf("Expected large file warning with backticked example, got: %s", output)
	}
}

func TestToolMkdirAndSuggestion(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	output := app.ExecuteTool("mkdir: newdir", false)
	if !strings.Contains(output, "Use `ls: newdir` to see it.") {
		t.Errorf("Expected backticked suggestion in output, got: %s", output)
	}
}

func TestToolLinesAndSuggestion(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	filePath := "lines.txt"
	os.WriteFile(filepath.Join(tempDir, filePath), []byte("line1\nline2\n"), 0644)

	output := app.ExecuteTool("lines: " + filePath, false)
	if !strings.Contains(output, "You can read it with `fileread: "+filePath+"`") {
		t.Errorf("Expected backticked suggestion in output, got: %s", output)
	}
}

func TestToolRMAndBackup(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	filePath := "todelete.txt"
	os.WriteFile(filepath.Join(tempDir, filePath), []byte("to be deleted"), 0644)

	output := app.ExecuteTool("rm: " + filePath, false)
	if !strings.Contains(output, "removed successfully") || !strings.Contains(output, "(Backup saved to `!trash/") {
		t.Errorf("Expected success message with backticked backup info, got: %s", output)
	}

	if _, err := os.Stat(filepath.Join(tempDir, filePath)); !os.IsNotExist(err) {
		t.Errorf("File still exists after rm")
	}
}

func TestToolHelp(t *testing.T) {
	app, _ := setupTestApp(t)

	output := app.ExecuteTool("help: filewrite", false)
	if !strings.Contains(output, "replace/append") {
		t.Errorf("Expected new help format, got: %s", output)
	}
}

func TestToolPathWithBackslash(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	os.Mkdir(filepath.Join(tempDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tempDir, "subdir", "file.txt"), []byte("test content"), 0644)

	// Simulate Windows-style path input
	output := app.ExecuteTool(`fileread: all subdir\file.txt`, false)
	if !strings.Contains(output, "test content") {
		t.Errorf("Expected 'test content' in output, got: %s", output)
	}
}

func TestToolTodo(t *testing.T) {
	app, _ := setupTestApp(t)
	app.todoList = "Task 1\nTask 2"

	// Mark done
	app.ExecuteTool("todo: 1 done", false)
	if app.todoList != "Task 1 [done]\nTask 2" {
		t.Errorf("Expected 'Task 1 [done]\nTask 2', got: %q", app.todoList)
	}

	// Reset
	app.ExecuteTool("todo: 1 reset", false)
	if app.todoList != "Task 1\nTask 2" {
		t.Errorf("Expected 'Task 1\nTask 2', got: %q", app.todoList)
	}
}

func TestToolFWriteQuotedPath(t *testing.T) {
    app, tempDir := setupTestApp(t)
    defer os.RemoveAll(tempDir)

    path := "path with spaces.txt"
    app.ExecuteTool(`filewrite: replace "` + path + `" some content`, false)

    content, _ := os.ReadFile(filepath.Join(tempDir, path))
    if string(content) != "some content" {
        t.Errorf("Expected 'some content', got %q", string(content))
    }
}

func TestSplitArgs(t *testing.T) {
	app := &App{}

	tests := []struct {
		input    string
		expected []string
	}{
		{`arg1 arg2`, []string{"arg1", "arg2"}},
		{`"arg with spaces" arg2`, []string{"arg with spaces", "arg2"}},
		{`'arg with spaces' arg2`, []string{"arg with spaces", "arg2"}},
		{`path\with\backslashes arg2`, []string{`path\with\backslashes`, "arg2"}},
		{`"quoted path\with\backslashes" arg2`, []string{`quoted path\with\backslashes`, "arg2"}},
		{`replace "quoted path" some content`, []string{"replace", "quoted path", "some", "content"}},
	}

	for _, tc := range tests {
		result := app.splitArgs(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("For input %q, expected %d parts, got %d: %v", tc.input, len(tc.expected), len(result), result)
			continue
		}
		for i := range result {
			if result[i] != tc.expected[i] {
				t.Errorf("For input %q, part %d: expected %q, got %q", tc.input, i, tc.expected[i], result[i])
			}
		}
	}
}

func TestSanitizeOutput(t *testing.T) {
	app := &App{
		config: Config{
			ProjectFolder: "/home/user/project",
		},
	}

	input := "/home/user/project/file.txt"
	expected := "./file.txt"
	output := app.sanitizeOutput(input)
	if output != expected {
		t.Errorf("Expected %s, got %s", expected, output)
	}
}

func TestToolMemories(t *testing.T) {
	app, _ := setupTestApp(t)

	// Test empty memories
	output := app.ExecuteTool("memories: 0", false)
	if output != "No notes found." {
		t.Errorf("Expected 'No notes found.', got: %s", output)
	}

	// Test writing memory
	app.ExecuteTool("memories: 1 first memory", false)
	output = app.ExecuteTool("memories: 0", false)
	if !strings.Contains(output, "first memory") {
		t.Errorf("Expected 'first memory', got: %s", output)
	}
}
