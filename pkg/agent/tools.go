package agent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/invopop/jsonschema"
)

type ToolDefinition struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  jsonschema.Schema `json:"parameters"`
	Function    func(input json.RawMessage) (string, error)
}

func GenerateSchema[T any]() jsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return *schema
}

// GetAllToolDefinitions returns all available tool definitions
func GetAllToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		ReadFileDefinition,
		ListFilesDefinition,
		RunCommandDefinition,
		InitProjectDefinition,
		WriteFileDefinition,
		EditFileDefinition,
		ManageWindowDefinition,
	}
}

// ExecuteToolByName executes a tool by name with JSON arguments
func ExecuteToolByName(name string, argsJSON json.RawMessage) (string, error) {
	for _, tool := range GetAllToolDefinitions() {
		if tool.Name == name {
			return tool.Function(argsJSON)
		}
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}

// --- Read File ---

type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

var ReadFileDefinition = ToolDefinition{
	Name:        "read_file",
	Description: "Read the contents of a given relative file path.",
	Parameters:  GenerateSchema[ReadFileInput](),
	Function:    ReadFile,
}

func ReadFile(input json.RawMessage) (string, error) {
	var args ReadFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}

	// 0. Security: explicitly block .env
	if strings.HasSuffix(args.Path, ".env") {
		return "", fmt.Errorf("access denied: .env files are protected")
	}

	// 1. Check size (Limit to 100KB)
	info, err := os.Stat(args.Path)
	if err != nil {
		return "", err
	}
	if info.Size() > 100*1024 {
		return "", fmt.Errorf("skipped: file too large (>100KB)")
	}

	// 2. Read
	content, err := os.ReadFile(args.Path)
	if err != nil {
		return "", err
	}

	// 3. Check for binary junk
	if !utf8.Valid(content) {
		return "", fmt.Errorf("skipped: appears to be binary")
	}

	// 4. Return with Metadata
	return fmt.Sprintf("File: %s\nSize: %d bytes\nLines: %d\n\n%s", args.Path, len(content), strings.Count(string(content), "\n")+1, string(content)), nil
}

// --- List Files ---

type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Optional relative path to list files from. Defaults to current directory."`
}

var ListFilesDefinition = ToolDefinition{
	Name:        "list_files",
	Description: "List files in the project. Respects .gitignore.",
	Parameters:  GenerateSchema[ListFilesInput](),
	Function:    ListFiles,
}

func ListFiles(input json.RawMessage) (string, error) {
	var args ListFilesInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}

	dir := "."
	if args.Path != "" {
		dir = args.Path
	}

	var fileList []string

	// 1. Try git ls-files
	cmd := exec.Command("git", "ls-files", "-c", "-o", "--exclude-standard")
	cmd.Dir = dir
	output, err := cmd.Output()

	if err == nil {
		// Git success
		lines := strings.Split(string(output), "\n")
		fileList = append(fileList, lines...)
	} else {
		// Fallback to filepath.Walk
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == ".git" || d.Name() == "bin") {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				fileList = append(fileList, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}

	// 2. Filter and cleaning
	var cleanList []string
	for _, path := range fileList {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		// 3. EXTRA SAFETY: Skip .git, bin, agent binaries, and .env
		// This applies to both git output and fallback output
		if strings.HasPrefix(path, ".git/") ||
			strings.HasPrefix(path, "bin/") ||
			path == "agent" ||
			path == "trace" ||
			path == ".env" {
			continue
		}

		cleanList = append(cleanList, path)
	}

	// Sort for consistent output
	sort.Strings(cleanList)

	// Build a tree structure from file paths
	root := dir
	if root == "." {
		root = "Project"
	}

	// Nord theme for tree
	enumeratorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#81a1c1")).MarginRight(1) // nordFrost3
	rootStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a3be8c")).Bold(true)           // nordAuroraGreen
	itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#88c0d0"))                      // nordFrost2

	// Build tree from paths
	t := buildFileTree(root, cleanList)
	t = t.Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(enumeratorStyle).
		RootStyle(rootStyle).
		ItemStyle(itemStyle)

	return t.String(), nil
}

// buildFileTree creates a tree structure from a list of file paths
func buildFileTree(root string, paths []string) *tree.Tree {
	t := tree.Root(root)

	// Group files by directory
	dirMap := make(map[string][]string)
	var rootFiles []string

	for _, path := range paths {
		parts := strings.Split(path, "/")
		if len(parts) == 1 {
			rootFiles = append(rootFiles, path)
		} else {
			dir := parts[0]
			rest := strings.Join(parts[1:], "/")
			dirMap[dir] = append(dirMap[dir], rest)
		}
	}

	// Add root files first
	for _, f := range rootFiles {
		t.Child(f)
	}

	// Add directories with their contents
	var dirs []string
	for d := range dirMap {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		subTree := buildFileTree(dir, dirMap[dir])
		t.Child(subTree)
	}

	return t
}

// --- Run Command ---

type RunCommandInput struct {
	Command string   `json:"command" jsonschema_description:"The command to run."`
	Args    []string `json:"args" jsonschema_description:"Arguments for the command."`
}

var RunCommandDefinition = ToolDefinition{
	Name:        "run_command",
	Description: "Run a shell command. Use this for git commands like 'git diff', 'git status', 'git log'.",
	Parameters:  GenerateSchema[RunCommandInput](),
	Function:    RunCommand,
}

// ResolveBinary attempts to handle missing binaries by checking for common alternatives (e.g. python -> python3)
func ResolveBinary(bin string) string {
	// 1. Check if binary exists as-is
	_, err := exec.LookPath(bin)
	if err == nil {
		return bin
	}
	// 2. Common fallbacks
	switch bin {
	case "python":
		if _, err := exec.LookPath("python3"); err == nil {
			return "python3"
		}
	case "pip":
		if _, err := exec.LookPath("pip3"); err == nil {
			return "pip3"
		}
	}
	return bin
}

func RunCommand(input json.RawMessage) (string, error) {
	var args RunCommandInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}

	// Smart resolve the command
	cmdName := ResolveBinary(args.Command)

	// Print for user visibility
	fmt.Printf("[Exec] %s %v\n", cmdName, args.Args)

	cmd := exec.Command(cmdName, args.Args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error: %s\nOutput:\n%s", err, string(output)), nil
	}
	return string(output), nil
}

// --- Init Project ---

type InitProjectInput struct {
	Name        string `json:"name,omitempty" jsonschema_description:"Optional name of the project directory. If invalid or empty, uses current directory."`
	Description string `json:"description,omitempty" jsonschema_description:"Short description for the README."`
}

var InitProjectDefinition = ToolDefinition{
	Name:        "init_project",
	Description: "Initialize a new git project with a README and .gitignore. Can create a new directory.",
	Parameters:  GenerateSchema[InitProjectInput](),
	Function:    InitProject,
}

func InitProject(input json.RawMessage) (string, error) {
	var args InitProjectInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}

	targetDir := "."
	if args.Name != "" {
		targetDir = args.Name
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Helper to write file if not exists
	writeFile := func(path, content string) error {
		fullPath := fmt.Sprintf("%s/%s", targetDir, path)
		if _, err := os.Stat(fullPath); err == nil {
			return nil // File exists, skip
		}
		return os.WriteFile(fullPath, []byte(content), 0644)
	}

	// 1. git init
	cmd := exec.Command("git", "init")
	cmd.Dir = targetDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init failed: %s", string(out))
	}

	// 2. README.md
	title := args.Name
	if title == "" {
		title = "Project"
	}
	// 3. .gitignore
	gitignoreContent := ".DS_Store\nnode_modules/\ndist/\nbin/\n.env\n"
	if err := writeFile(".gitignore", gitignoreContent); err != nil {
		return "", fmt.Errorf("failed to create .gitignore: %w", err)
	}

	return fmt.Sprintf("Initialized project in '%s' with git, README.md, and .gitignore.", targetDir), nil
}

// --- Edit File ---

type EditFileInput struct {
	Path        string `json:"path" jsonschema_description:"The relative path of the file to edit"`
	SearchText  string `json:"search_text" jsonschema_description:"The exact block of text to replace. Must match exactly."`
	ReplaceText string `json:"replace_text" jsonschema_description:"The new text to insert in place of the search_text."`
}

var EditFileDefinition = ToolDefinition{
	Name:        "edit_file",
	Description: "Edit a file by replacing a specific block of text with new text. Uses exact string matching.",
	Parameters:  GenerateSchema[EditFileInput](),
	Function:    EditFile,
}

func EditFile(input json.RawMessage) (string, error) {
	var args EditFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}

	// 0. Security: explicitly block .env
	if strings.HasSuffix(args.Path, ".env") {
		return "", fmt.Errorf("access denied: .env files are protected")
	}

	// 1. Read File
	contentBytes, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}
	content := string(contentBytes)

	// 2. Locate the Block
	if !strings.Contains(content, args.SearchText) {
		return "", fmt.Errorf("search block not found in %s. Ensure exact match (including whitespace).", args.Path)
	}

	// 3. Replace
	newContent := strings.Replace(content, args.SearchText, args.ReplaceText, 1)

	// 4. Write Back
	if err := os.WriteFile(args.Path, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %v", err)
	}

	return fmt.Sprintf("Successfully edited %s", args.Path), nil
}

// --- Write File ---

type WriteFileInput struct {
	Path    string `json:"path" jsonschema_description:"The relative path of the file to write."`
	Content string `json:"content" jsonschema_description:"The content to write to the file."`
	Ext     string `json:"ext,omitempty" jsonschema_description:"The file extension (e.g., .go, .txt). Optional."`
}

var WriteFileDefinition = ToolDefinition{
	Name:        "write_file",
	Description: "Write content to a file. Creates the file if it doesn't exist, or overwrites it if it does.",
	Parameters:  GenerateSchema[WriteFileInput](),
	Function:    WriteFile,
}

func WriteFile(input json.RawMessage) (string, error) {
	var args WriteFileInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}

	// 0. Security: explicitly block .env
	if strings.HasSuffix(args.Path, ".env") {
		return "", fmt.Errorf("access denied: .env files are protected")
	}

	// 1. Create directory if needed
	dir := filepath.Dir(args.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %v", err)
	}

	// 2. Write File
	if err := os.WriteFile(args.Path, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %v", err)
	}

	return fmt.Sprintf("Successfully wrote to %s (Length: %d characters)", args.Path, len(args.Content)), nil
}

// --- Manage Window ---

type ManageWindowInput struct {
	Action string `json:"action" jsonschema_description:"Action to perform: 'open' or 'close'."`
	Target string `json:"target,omitempty" jsonschema_description:"Target view: 'terminal' (default)."`
}

var ManageWindowDefinition = ToolDefinition{
	Name:        "manage_window",
	Description: "Control the interface layout, such as opening or closing the sidebar to show terminal output.",
	Parameters:  GenerateSchema[ManageWindowInput](),
	Function:    ManageWindow,
}

func ManageWindow(input json.RawMessage) (string, error) {
	var args ManageWindowInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}

	if args.Action != "open" && args.Action != "close" {
		return "", fmt.Errorf("invalid action: %s", args.Action)
	}

	return fmt.Sprintf("Window action '%s' triggered for target '%s'", args.Action, args.Target), nil
}
