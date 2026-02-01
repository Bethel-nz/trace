package ui

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sashabaranov/go-openai"
)

// --- Update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
		cmds  []tea.Cmd
	)

	switch msg := msg.(type) {

	// Window Control: Toggle sidebar and resume AI
	case WindowControlMsg:
		switch msg.Action {
		case "open":
			m.ShowSidebar = true
		case "close":
			m.ShowSidebar = false
		}

		// 1. Update History with tool result
		result := fmt.Sprintf("Window action '%s' triggered.", msg.Action)
		m.History = msg.History
		m.History = append(m.History, openai.ChatCompletionMessage{
			Role:       openai.ChatMessageRoleTool,
			Content:    result,
			ToolCallID: msg.ToolCallID,
		})

		// 2. Resume AI
		m.State = StateThinking

		// 3. Trigger Resize (to update component widths) + Resume AI
		// We use a batch cmd
		resizeCmd := func() tea.Msg {
			return tea.WindowSizeMsg{Width: m.Width, Height: m.Height}
		}
		return m, tea.Batch(resizeCmd, m.InvokeAI())

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height

		// 1. Calculate Pane Sizes
		inputHeight := int(float64(msg.Height) * 0.10)
		if inputHeight < 3 {
			inputHeight = 3
		}

		// Calculate available height for viewports
		chatHeight := msg.Height - inputHeight - 2 // border allowance

		// 2. Resize Components
		chatWidth := msg.Width - 4
		sidebarWidth := 0

		if m.ShowSidebar {
			sidebarWidth = msg.Width / 3 // 33% width
			if sidebarWidth < 40 {
				sidebarWidth = 40 // Min width
			}
			chatWidth = msg.Width - sidebarWidth - 4

			// Resize side viewport
			m.SideViewport.Width = sidebarWidth - 2 // Padding/Border
			m.SideViewport.Height = chatHeight
		}

		m.Viewport.Width = chatWidth
		m.Viewport.Height = chatHeight

		m.Input.SetWidth(msg.Width - 4)

		// Re-render chat with new width
		m.RenderChat()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if m.ShowAutocomplete {
				m.ShowAutocomplete = false
				return m, nil
			}
			m.SaveSession()
			return m, tea.Quit

		case "up":
			if m.ShowAutocomplete && m.AutocompleteIdx > 0 {
				m.AutocompleteIdx--
				return m, nil
			}
		case "down":
			if m.ShowAutocomplete && m.AutocompleteIdx < len(m.AutocompleteList)-1 {
				m.AutocompleteIdx++
				return m, nil
			}
		case "tab":
			if m.ShowAutocomplete && len(m.AutocompleteList) > 0 {
				// Insert selected file
				selected := m.AutocompleteList[m.AutocompleteIdx]
				currentVal := m.Input.Value()
				// Replace the @partial with @fullpath
				words := strings.Fields(currentVal)
				if len(words) > 0 {
					words[len(words)-1] = "@" + selected
					m.Input.SetValue(strings.Join(words, " ") + " ")
				}
				m.ShowAutocomplete = false
				return m, nil
			}

		case "enter":
			// If autocomplete is showing, select item
			if m.ShowAutocomplete && len(m.AutocompleteList) > 0 {
				selected := m.AutocompleteList[m.AutocompleteIdx]
				currentVal := m.Input.Value()
				words := strings.Fields(currentVal)
				if len(words) > 0 {
					words[len(words)-1] = "@" + selected
					m.Input.SetValue(strings.Join(words, " ") + " ")
				}
				m.ShowAutocomplete = false
				return m, nil
			}
			if !msg.Alt && m.Input.Value() != "" {
				// 1. Parse for @tags and read files
				userMsg := m.Input.Value()
				finalContent := m.resolveFileTags(userMsg)

				// 2. Add to History
				m.History = append(m.History, openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleUser,
					Content: finalContent,
				})

				// 3. Clear Input
				m.Input.Reset()

				// 4. Handle State
				if m.State == StateIdle {
					// Start AI immediately
					m.State = StateThinking
					cmds = append(cmds, m.InvokeAI()) // Initial call logic
				} else {
					// Queue it
					m.PendingQueue = append(m.PendingQueue, finalContent)
				}
				m.RenderChat()
				// Don't process the enter in textarea
				return m, tea.Batch(cmds...)
			}
		}

	// AI Response (with full history update)
	case AiResponseMsg:
		m.History = msg.History
		m.RenderChat()
		m.Viewport.GotoBottom()
		cmds = append(cmds, func() tea.Msg { return AiCompleteMsg{} })

	case AiCompleteMsg:
		m.State = StateIdle
		// If we have queued messages, fire the next one!
		if len(m.PendingQueue) > 0 {
			nextContent := m.PendingQueue[0]
			m.PendingQueue = m.PendingQueue[1:]

			m.History = append(m.History, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: nextContent,
			})
			m.RenderChat()

			m.State = StateThinking
			cmds = append(cmds, m.InvokeAI())
		}

	case ErrMsg:
		slog.Error("Error received in UI", "error", msg)
		m.History = append(m.History, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: fmt.Sprintf("**Error:** %v", msg),
		})
		m.State = StateIdle
		m.RenderChat()
		m.Viewport.GotoBottom()

	// --- Process Streaming Handlers ---

	case RunCommandMsg:
		// 1. Update history with what happened inside the AI loop (including the Assistant's tool call)
		m.History = msg.History
		m.ProcessOutput = "" // Reset output buffer
		m.RenderChat()

		// 2. Start the process AND start the subscriber
		return m, tea.Batch(
			RunProcessCmd(msg.Command, msg.Args, msg.ToolCallID, m.ProcessChan),
			WaitForProcessOutput(m.ProcessChan),
		)

	case ProcessOutputMsg:
		// Accumulate output
		if m.ShowSidebar {
			// Redirect to SideViewport
			// Currently SideViewport content is not persistent in a string buffer in Model (unlike ProcessOutput)
			// So we need to append to what's there? Viewport doesn't support Append easily.
			// Let's store sidebar content in m.ProcessOutput as well or a specific buffer?
			// Actually, let's just append to m.ProcessOutput and let SideViewport render IT.
			m.ProcessOutput += string(msg) + "\n"

			// Update SideViewport content
			m.SideViewport.SetContent(m.ProcessOutput)
			m.SideViewport.GotoBottom()
		} else {
			m.ProcessOutput += string(msg) + "\n"
			m.RenderChat()
			m.Viewport.GotoBottom()
		}

		// Listen for next line
		return m, WaitForProcessOutput(m.ProcessChan)

	case ProcessDoneMsg:
		result := "Process finished successfully."
		if msg.Err != nil {
			result = fmt.Sprintf("Process exited with error: %v", msg.Err)
		}
		// Add result as Tool Output message to history so model sees it
		m.History = append(m.History, openai.ChatCompletionMessage{
			Role:       openai.ChatMessageRoleTool,
			Content:    result,
			ToolCallID: msg.ToolCallID,
		})

		// Optionally append the full process output to the history tool message?
		// Or keep it separate. The model usually doesn't need to see the full log if it's huge.
		// For now, let's just clear the visual buffer since it's "done".
		// BUT the user might want to keep the logs visible?
		// If we clear m.ProcessOutput, the logs disappear from the screen.
		// That is bad UX.
		// We should probably convert m.ProcessOutput into a user-visible block in history if we want to persist it.
		// Or, leaving it in m.ProcessOutput until the NEXT command runs?
		// But if the user types something else, m.ProcessOutput is still there.

		// If Sidebar was open, we assume the user saw the output there and doesn't want it cluttering history.
		if !m.ShowSidebar {
			fullLog := "Process Output:\n```\n" + m.ProcessOutput + "```\n" + result
			m.History[len(m.History)-1].Content = fullLog
		}

		m.ProcessOutput = "" // Now we can clear it

		m.RenderChat()
		m.Viewport.GotoBottom()
		// Trigger AI to see the result
		m.State = StateThinking
		return m, m.InvokeAI()
	}

	m.Input, tiCmd = m.Input.Update(msg)

	// Check if we should show autocomplete
	inputVal := m.Input.Value()
	words := strings.Fields(inputVal)
	if len(words) > 0 {
		lastWord := words[len(words)-1]
		if strings.HasPrefix(lastWord, "@") {
			search := strings.TrimPrefix(lastWord, "@")

			// Check if this is an exact match (file already selected)
			isExactMatch := false
			for _, f := range m.Files {
				if f == search {
					isExactMatch = true
					break
				}
			}

			// Don't show autocomplete if file is already fully selected
			if isExactMatch {
				m.ShowAutocomplete = false
			} else {
				m.AutocompleteList = []string{}
				for _, f := range m.Files {
					if search == "" || strings.Contains(f, search) {
						m.AutocompleteList = append(m.AutocompleteList, f)
						if len(m.AutocompleteList) >= 10 {
							break // Limit to 10 items
						}
					}
				}
				if len(m.AutocompleteList) > 0 {
					m.ShowAutocomplete = true
					if m.AutocompleteIdx >= len(m.AutocompleteList) {
						m.AutocompleteIdx = 0
					}
				} else {
					m.ShowAutocomplete = false
				}
			}
		} else {
			m.ShowAutocomplete = false
		}
	} else {
		m.ShowAutocomplete = false
	}

	m.Viewport, vpCmd = m.Viewport.Update(msg)

	// Always update spinner if thinking
	if m.State == StateThinking {
		m.Spinner, spCmd = m.Spinner.Update(msg)
	}

	cmds = append(cmds, tiCmd, vpCmd, spCmd)
	return m, tea.Batch(cmds...)
}

func (m Model) SaveSession() {
	if len(m.History) == 0 {
		return
	}

	// Regex to strip hints about file references
	reHint := regexp.MustCompile(`\n\n\[User has referenced these files:.*?\]`)

	fName := fmt.Sprintf("trace_session_%d.md", time.Now().Unix())
	f, err := os.Create(fName)
	if err != nil {
		return
	}
	defer f.Close()

	for _, msg := range m.History {
		// Skip system and tool messages
		if msg.Role == "system" || msg.Role == openai.ChatMessageRoleTool {
			continue
		}
		// Skip assistant messages that are just tool calls
		if msg.Role == openai.ChatMessageRoleAssistant && msg.Content == "" && len(msg.ToolCalls) > 0 {
			continue
		}

		role := "User"
		if msg.Role == openai.ChatMessageRoleAssistant {
			role = "Trace"
		}

		content := msg.Content
		if msg.Role == openai.ChatMessageRoleUser {
			// Strip the hint we added
			content = reHint.ReplaceAllString(content, "")
			content = strings.TrimSpace(content)

			// Convert @tags to markdown links with relative paths
			words := strings.Fields(content)
			for i, w := range words {
				if strings.HasPrefix(w, "@") {
					filename := strings.TrimPrefix(w, "@")
					// Use relative path so markdown is portable
					words[i] = fmt.Sprintf("[%s](./%s)", w, filename)
				}
			}
			content = strings.Join(words, " ")
		}

		fmt.Fprintf(f, "## %s\n\n%s\n\n---\n\n", role, content)
	}
}

// Detect @filename and append hints for the model to read them
func (m *Model) resolveFileTags(input string) string {
	words := strings.Fields(input)
	var referencedFiles []string

	for _, word := range words {
		if strings.HasPrefix(word, "@") {
			path := strings.TrimPrefix(word, "@")

			// Check if we know this file
			for _, f := range m.Files {
				if f == path {
					referencedFiles = append(referencedFiles, path)
					break
				}
			}
		}
	}

	if len(referencedFiles) > 0 {
		hint := "\n\n[User has referenced these files: " + strings.Join(referencedFiles, ", ") + ". Use the read_file tool to view their contents.]"
		return input + hint
	}
	return input
}
