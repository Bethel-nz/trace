package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"agent/pkg/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
)

// --- Main ---

func main() {
	_ = godotenv.Load()

	// Setup file logger
	logFile, err := os.OpenFile("trace.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Println("Failed to open log file:", err)
		os.Exit(1)
	}
	defer logFile.Close()

	logger := slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	slog.Info("Trace starting up")

	apiKey := os.Getenv("PROVIDER_API_KEY")
	authToken := os.Getenv("PROVIDER_AUTH_TOKEN")
	if apiKey == "" && authToken != "" {
		apiKey = authToken
	}

	slog.Debug("Config loaded", "baseURL", os.Getenv("PROVIDER_BASE_URL"), "model", os.Getenv("PROVIDER_MODEL"))

	config := openai.DefaultConfig(apiKey)
	if baseURL := os.Getenv("PROVIDER_BASE_URL"); baseURL != "" {
		config.BaseURL = baseURL
	}

	client := openai.NewClientWithConfig(config)

	// PREVENT TERMINAL ARTIFACTS: formatting queries
	lipgloss.SetHasDarkBackground(true)

	// Initialize the File List (Respecting gitignore)
	files, _ := listProjectFiles()

	// Load System Prompt
	var sysPrompt string
	if promptBytes, err := os.ReadFile("system_prompt.md"); err == nil {
		sysPrompt = string(promptBytes)
	} else {
		sysPrompt = "You are Trace, a helpful AI coding assistant."
	}

	// DISABLE MOUSE temporarily to fix artifacts reported by user
	p := tea.NewProgram(ui.InitialModel(client, files, sysPrompt), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

// --- File System ---

func listProjectFiles() ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-c", "-o", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		// Fallback for non-git
		return []string{}, err
	}

	lines := strings.Split(string(out), "\n")
	var clean []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, ".git") && l != "agent" && l != "trace" && !strings.HasPrefix(l, "bin/") && l != ".env" {
			clean = append(clean, l)
		}
	}
	return clean, nil
}
