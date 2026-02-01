package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/bethel-nz/trace/pkg/agent"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/sashabaranov/go-openai"
)

// --- View ---

func (m Model) View() string {
	if m.Width == 0 {
		return "Initializing..."
	}

	// Single column layout
	chatBox := blurredStyle.Width(m.Width - 2).Height(m.Viewport.Height).Render(m.Viewport.View())
	inputBox := focusedStyle.Width(m.Width - 2).Render(m.Input.View())

	// Autocomplete overlay
	if m.ShowAutocomplete && len(m.AutocompleteList) > 0 {
		var autocompleteContent strings.Builder
		autocompleteContent.WriteString("Files:\n")
		for i, file := range m.AutocompleteList {
			if i == m.AutocompleteIdx {
				autocompleteContent.WriteString(fileSelected.Render("> "+file) + "\n")
			} else {
				autocompleteContent.WriteString(fileNormal.Render("  "+file) + "\n")
			}
		}
		autocompleteContent.WriteString("\n↑↓: Navigate | Tab/Enter: Select | Esc: Cancel")

		autocompleteBox := focusedStyle.
			Width(m.Width - 6).
			BorderForeground(lipgloss.Color("205")).
			Render(autocompleteContent.String())

		// Position above input
		return lipgloss.JoinVertical(lipgloss.Left, chatBox, autocompleteBox, inputBox)
	}

	// Status Bar
	statusContent := fmt.Sprintf(" Model: %s │ Tools: %d │ Messages: %d ",
		os.Getenv("PROVIDER_MODEL"),
		len(agent.GetAllToolDefinitions()),
		len(m.History),
	)
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("235")).
		Padding(0, 1).
		Width(m.Width)

	statusBar := statusStyle.Render(statusContent)

	// Determine middle content (Spinner or nothing)
	var midContent string
	if m.State == StateThinking {
		midContent = fmt.Sprintf("\n %s Thinking...", m.Spinner.View())
	}

	var mainView string
	if m.ShowSidebar {
		// Re-render chatBox with constrained width
		// m.Viewport.Width was already updated in Update() to be chatWidth
		// So we just need to respect it here instead of using m.Width
		chatBox = blurredStyle.Width(m.Viewport.Width).Height(m.Viewport.Height).Render(m.Viewport.View())

		sideBox := blurredStyle.Width(m.SideViewport.Width).Height(m.SideViewport.Height).Render(m.SideViewport.View())
		mainView = lipgloss.JoinHorizontal(lipgloss.Top, chatBox, sideBox)
	} else {
		mainView = chatBox
	}

	return lipgloss.JoinVertical(lipgloss.Left, mainView, midContent, inputBox, statusBar)
}

// --- Helpers ---

// Render the Markdown Chat
func (m *Model) RenderChat() {
	buf := new(strings.Builder)

	// Regex to strip filecontext and file reference hints
	reContext := regexp.MustCompile(`(?s)<file_context.*?>.*?</file_context>`)
	reHint := regexp.MustCompile(`\n\n\[User has referenced these files:.*?\]`)

	// Track visible messages to manage separators
	visibleCount := 0

	// Create a single renderer instance
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(m.Viewport.Width-4),
	)

	// Helper to render and append a message block
	renderBlock := func(role, content string) {
		if visibleCount > 0 {
			fmt.Fprint(buf, "\n\n___\n\n")
		}

		// Render title with proper style
		var title string
		if role == "user" {
			title = userStyle.Render("You")
		} else {
			title = traceStyle.Render("Trace")
		}
		fmt.Fprintf(buf, "%s\n\n", title)

		// Clean content
		displayContent := reContext.ReplaceAllString(content, "")
		displayContent = reHint.ReplaceAllString(displayContent, "")
		displayContent = strings.TrimSpace(displayContent)

		// Highlight @tags with bold
		// We use a regex to replace "@filename" with "**@filename**"
		reTags := regexp.MustCompile(`@[\w\.\-/]+`)
		displayContent = reTags.ReplaceAllStringFunc(displayContent, func(match string) string {
			return "**" + match + "**"
		})

		// Render body with Glamour
		rendered, err := renderer.Render(displayContent)
		if err != nil {
			rendered = displayContent
		}
		// Trim excessive newlines from glamour output
		fmt.Fprint(buf, strings.TrimSpace(rendered))
		visibleCount++
	}

	// Render history
	for _, msg := range m.History {
		// Skip the internal auto-trigger message
		if msg.Role == openai.ChatMessageRoleUser && msg.Content == "Hello! Please introduce yourself and your tools briefly." {
			continue
		}
		// Skip system messages
		if msg.Role == "system" {
			continue
		}
		// Skip tool messages (internal tool results)
		if msg.Role == openai.ChatMessageRoleTool {
			continue
		}
		// Skip assistant messages that are just tool calls (no content to show)
		if msg.Role == openai.ChatMessageRoleAssistant && msg.Content == "" && len(msg.ToolCalls) > 0 {
			continue
		}

		switch msg.Role {
		case openai.ChatMessageRoleUser:
			renderBlock("user", msg.Content)

		case openai.ChatMessageRoleAssistant:
			// Check if this message has tool calls
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					if visibleCount > 0 {
						fmt.Fprint(buf, "\n\n___\n\n")
					}
					fmt.Fprintf(buf, "**Calling tool:** `%s`\n", tc.Function.Name)
					visibleCount++
				}
			}
			if msg.Content != "" {
				renderBlock("assistant", msg.Content)
			}
		}
	}

	// Render Queue (Grayed out)
	for i, q := range m.PendingQueue {
		cleanQ := reContext.ReplaceAllString(q, "")
		cleanQ = strings.TrimSpace(cleanQ)
		if len(cleanQ) > 50 {
			cleanQ = cleanQ[:47] + "..."
		}
		if visibleCount > 0 || i > 0 {
			fmt.Fprint(buf, "\n\n___\n\n")
		}
		fmt.Fprintf(buf, "_(Queued #%d): %s_\n", i+1, cleanQ)
		visibleCount++
	}

	if m.ProcessOutput != "" && !m.ShowSidebar {
		if visibleCount > 0 {
			fmt.Fprint(buf, "\n\n___\n\n")
		}
		fmt.Fprintf(buf, "**Process Output:**\n%s", m.ProcessOutput)
		visibleCount++
	}

	m.Viewport.SetContent(buf.String())
}
