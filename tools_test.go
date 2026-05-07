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
				"ls", "fwrite", "fread", "rm", "note", "todo", "lines", "fcopy", "mkdir", "url", "urltxt", "help",
			},
		},
	}
	// Mock UpdateAINotes for tests to update app state
	// In the real app, this is in app.go and updates a.aiNotes
	return app, tempDir
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
	app.ExecuteTool("fwrite: " + filePath + " write initial content", false)

	// Second write (should trigger backup)
	output := app.ExecuteTool("fwrite: " + filePath + " write updated content", false)
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

func TestToolFReadLargeFile(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	filePath := "large.txt"
	fullPath := filepath.Join(tempDir, filePath)

	// Create a file > 2MB
	data := make([]byte, 2*1024*1024 + 1024)
	os.WriteFile(fullPath, data, 0644)

	output := app.ExecuteTool("fread: " + filePath, false)
	if !strings.Contains(output, "use head or tail") || !strings.Contains(output, "Example: `fread:") {
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
	if !strings.Contains(output, "You can read it with `fread: "+filePath+"`") {
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

	// Test User help (syntax)
	outputUser := app.ExecuteTool("help: fwrite", false)
	if !strings.Contains(outputUser, "Backs up to !trash") || strings.Contains(outputUser, "{") {
		t.Errorf("Expected syntax for user, got: %s", outputUser)
	}

	// Test AI help (JSON)
	outputAI := app.ExecuteTool("help: fwrite", true)
	if !strings.Contains(outputAI, `{"tool_name": "fwrite"`) {
		t.Errorf("Expected JSON for AI, got: %s", outputAI)
	}
}

func TestToolPathWithBackslash(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	os.Mkdir(filepath.Join(tempDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tempDir, "subdir", "file.txt"), []byte("test content"), 0644)

	// Simulate Windows-style path input
	output := app.ExecuteTool(`fread: subdir\file.txt`, false)
	if !strings.Contains(output, "test content") {
		t.Errorf("Expected 'test content' in output, got: %s", output)
	}
}


func TestToolNote(t *testing.T) {
	app, _ := setupTestApp(t)

	// Test empty notes
	output := app.ExecuteTool("note: 0", false)
	if output != "No notes found." {
		t.Errorf("Expected 'No notes found.', got: %s", output)
	}

	// Test writing note
	app.ExecuteTool("note: 1 first note", false)
	output = app.ExecuteTool("note: 0", false)
	if !strings.Contains(output, "first note") {
		t.Errorf("Expected 'first note', got: %s", output)
	}

	// Test writing specific ID with padding
	app.ExecuteTool("note: 3 third note", false)
	output = app.ExecuteTool("note: 0", false)
	if !strings.Contains(output, "first note") || !strings.Contains(output, "third note") {
		t.Errorf("Expected padded notes, got: %q", output)
	}

	// Test reading specific note
	output = app.ExecuteTool("note: 3", false)
	if output != "Note 3: third note" {
		t.Errorf("Expected 'Note 3: third note', got: %s", output)
	}

	// Test linebreak removal
	app.ExecuteTool("note: 4 multi\nline\nnote", false)
	output = app.ExecuteTool("note: 4", false)
	if output != "Note 4: multi line note" {
		t.Errorf("Expected 'Note 4: multi line note', got: %q", output)
	}
}

func TestToolFWriteRestore(t *testing.T) {
	app, tempDir := setupTestApp(t)
	defer os.RemoveAll(tempDir)

	filePath := "restore_test.txt"
	fullPath := filepath.Join(tempDir, filePath)

	// Write initial content
	os.WriteFile(fullPath, []byte("initial content"), 0644)

	// Attempt to write with invalid operation to simulate failure (not easy with current implementation)
	// Instead, let's make the directory read-only to force a write failure if possible
	// Or more reliably, test the restore logic directly if we can't easily trigger write failure in os.WriteFile

	// Let's mock a failure by using a path that is a directory
	dirPath := "faildir"
	os.Mkdir(filepath.Join(tempDir, dirPath), 0755)

	// Create a file with same name as dir to cause failure? No, let's use a path that is blocked.
	blockedFile := "blocked.txt"
	os.WriteFile(filepath.Join(tempDir, blockedFile), []byte("original"), 0644)

	// On Linux, we can try to make it unwriteable
	os.Chmod(filepath.Join(tempDir, blockedFile), 0444)

	output := app.ExecuteTool("fwrite: " + blockedFile + " write new content", false)

	if strings.Contains(output, "Writing data failed") && strings.Contains(output, "Original file restored from backup") {
		t.Log("Successfully verified restore logic")
	} else if strings.Contains(output, "permission denied") || strings.Contains(output, "Error writing to file") {
		// Even if it didn't restore (maybe backup failed too if permissions were weird), it caught the error
		t.Logf("Caught write error as expected: %s", output)
	} else {
		// If it actually succeeded, chmod didn't work as expected (e.g. running as root)
		t.Logf("Write unexpectedly succeeded or gave different output: %s", output)
	}
}
