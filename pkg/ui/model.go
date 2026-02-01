package ui

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sashabaranov/go-openai"
)

type SessionState int

const (
	StateIdle SessionState = iota
	StateThinking
)

type ErrMsg error

type WindowControlMsg struct {
	Action     string
	Target     string
	ToolCallID string
	History    []openai.ChatCompletionMessage
}

type Model struct {
	Client *openai.Client
	State  SessionState

	// UI Components
	Viewport     viewport.Model
	SideViewport viewport.Model // Embedded Terminal / Sidebar
	Input        textarea.Model
	Spinner      spinner.Model

	// Data
	Files    []string // All files in repo
	Filtered []string // For autocomplete

	History      []openai.ChatCompletionMessage // Conversation history
	PendingQueue []string                       // User messages waiting to be sent

	ProcessChan   chan tea.Msg // Channel for live process logs
	ProcessOutput string       // Accumulator for current process output

	// Autocomplete state
	ShowAutocomplete bool
	AutocompleteIdx  int
	AutocompleteList []string

	// Layout dimensions
	Width, Height int
	ShowSidebar   bool // Toggle for Right Sidebar
}

func InitialModel(client *openai.Client, files []string, systemPrompt string) Model {
	// Input area setup
	ta := textarea.New()
	ta.Placeholder = "Ask Trace... (Type @ to tag files)"
	ta.Focus()
	ta.Prompt = "| "
	ta.CharLimit = 0
	ta.SetHeight(5)
	ta.ShowLineNumbers = false

	// Viewport setup
	vp := viewport.New(0, 0)
	vp.SetContent("")

	// Side Viewport setup
	svp := viewport.New(0, 0)
	svp.SetContent("")

	// Spinner setup
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(nordFrost2)

	initialHistory := []openai.ChatCompletionMessage{}
	// Add a trigger message to get the agent to say "Hi"
	if systemPrompt != "" {
		initialHistory = append(initialHistory, openai.ChatCompletionMessage{
			Role:    "system",
			Content: systemPrompt,
		})
		initialHistory = append(initialHistory, openai.ChatCompletionMessage{
			Role:    "user",
			Content: "Hello! Please introduce yourself and your tools briefly.",
		})
	}
	ta.KeyMap.InsertNewline.SetEnabled(false)

	return Model{
		Client:       client,
		State:        StateIdle,
		Viewport:     vp,
		SideViewport: svp,
		Input:        ta,
		Spinner:      s,
		Files:        files,
		Filtered:     []string{},
		History:      initialHistory,
		PendingQueue: []string{},
		ProcessChan:  make(chan tea.Msg),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.Spinner.Tick,
		m.InvokeAI(), // Trigger the API call
	)
}
