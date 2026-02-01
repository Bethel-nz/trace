package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestEditFile(t *testing.T) {
	// Setup
	tmpFile, err := os.CreateTemp("", "test_edit_file_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	originalContent := "Line 1\nLine 2\nLine 3\n"
	if err := os.WriteFile(tmpFile.Name(), []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Test Case 1: Successful Edit
	args := EditFileInput{
		Path:        tmpFile.Name(),
		SearchText:  "Line 2\n",
		ReplaceText: "Line 2 Modified\n",
	}
	argsBytes, _ := json.Marshal(args)

	result, err := EditFile(argsBytes)
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}
	if !strings.Contains(result, "Successfully edited") {
		t.Errorf("Unexpected result: %s", result)
	}

	// Verify Content
	content, _ := os.ReadFile(tmpFile.Name())
	expected := "Line 1\nLine 2 Modified\nLine 3\n"
	if string(content) != expected {
		t.Errorf("Expected content:\n%q\nGot:\n%q", expected, string(content))
	}

	// Test Case 2: Block Not Found
	args.SearchText = "NonExistent"
	argsBytes, _ = json.Marshal(args)
	_, err = EditFile(argsBytes)
	if err == nil {
		t.Error("Expected error for non-existent block, got nil")
	}
}
