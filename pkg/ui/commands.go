package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"agent/pkg/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sashabaranov/go-openai"
)

// --- Process Streaming Support ---

type RunCommandMsg struct {
	Command    string
	Args       []string
	ToolCallID string
	History    []openai.ChatCompletionMessage
}

type ProcessOutputMsg string
type ProcessDoneMsg struct {
	Err        error
	ToolCallID string
}

// RunProcessCmd executes a command and streams output to a channel
func RunProcessCmd(command string, args []string, toolCallID string, sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		// Smart resolve command (e.g. python -> python3)
		resolvedCmd := agent.ResolveBinary(command)
		cmd := exec.Command(resolvedCmd, args...)

		// 1. Pipe both Stdout and Stderr
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()

		if err := cmd.Start(); err != nil {
			return ProcessDoneMsg{Err: err, ToolCallID: toolCallID}
		}

		// 2. Stream Reader (Stdout)
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				// Send line to the channel (thread-safe)
				sub <- ProcessOutputMsg(scanner.Text())
			}
		}()

		// 3. Stream Reader (Stderr)
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				sub <- ProcessOutputMsg(scanner.Text())
			}
		}()

		// 4. Wait for exit
		err := cmd.Wait()
		return ProcessDoneMsg{Err: err, ToolCallID: toolCallID}
	}
}

// WaitForProcessOutput listens for the next log line
func WaitForProcessOutput(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

// --- AI Commands ---

type AiCompleteMsg struct{}

// AiResponseMsg carries content and updated history
type AiResponseMsg struct {
	Content string
	History []openai.ChatCompletionMessage
}

func (m Model) InvokeAI() tea.Cmd {
	return func() tea.Msg {
		modelName := os.Getenv("PROVIDER_MODEL")
		if modelName == "" {
			slog.Error("PROVIDER_MODEL not set")
			return ErrMsg(errors.New("PROVIDER_MODEL not set in .env"))
		}

		// Copy history for the loop
		messages := make([]openai.ChatCompletionMessage, len(m.History))
		copy(messages, m.History)

		tools := convertToolsToOpenAI(agent.GetAllToolDefinitions())

		// Agentic loop - keep calling until we get a final response
		for iteration := 0; iteration < 10; iteration++ { // Max 10 iterations to prevent infinite loops
			slog.Info("Calling AI", "model", modelName, "messageCount", len(messages), "iteration", iteration)

			req := openai.ChatCompletionRequest{
				Model:    modelName,
				Messages: messages,
				Tools:    tools,
			}

			resp, err := m.Client.CreateChatCompletion(context.Background(), req)
			if err != nil {
				slog.Error("API call failed", "error", err)
				return ErrMsg(fmt.Errorf("API error: %v", err))
			}

			if len(resp.Choices) == 0 {
				slog.Warn("No choices in response")
				return ErrMsg(errors.New("no response from model"))
			}

			choice := resp.Choices[0]
			slog.Info("AI response", "finishReason", choice.FinishReason, "toolCallCount", len(choice.Message.ToolCalls), "contentLength", len(choice.Message.Content))

			// Check if the model wants to call tools
			if len(choice.Message.ToolCalls) > 0 {
				slog.Info("Model requested tool calls", "count", len(choice.Message.ToolCalls))

				// Add assistant message with tool calls to history
				assistantMsg := openai.ChatCompletionMessage{
					Role:      openai.ChatMessageRoleAssistant,
					Content:   choice.Message.Content,
					ToolCalls: choice.Message.ToolCalls,
				}
				messages = append(messages, assistantMsg)

				// Execute each tool and add results
				for _, toolCall := range choice.Message.ToolCalls {
					slog.Info("Executing tool", "name", toolCall.Function.Name, "id", toolCall.ID, "args", toolCall.Function.Arguments)

					if toolCall.Function.Name == "run_command" {
						var args struct {
							Command string   `json:"command"`
							Args    []string `json:"args"`
						}
						// Use map[string]interface and define struct locally or inside logic
						if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
							// Trigger part 2 of the "Pulse" pattern
							return RunCommandMsg{
								Command:    args.Command,
								Args:       args.Args,
								ToolCallID: toolCall.ID,
								History:    messages,
							}
						}
					}

					// Check if it's the specific "manage_window" tool
					if toolCall.Function.Name == "manage_window" {
						var args struct {
							Action string `json:"action"`
							Target string `json:"target"`
						}
						if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
							return WindowControlMsg{
								Action:     args.Action,
								Target:     args.Target,
								ToolCallID: toolCall.ID,
								History:    messages,
							}
						}
					}

					// Execute other tools normally
					result, err := agent.ExecuteToolByName(toolCall.Function.Name, json.RawMessage(toolCall.Function.Arguments))
					if err != nil {
						result = fmt.Sprintf("Error executing tool: %v", err)
						slog.Error("Tool execution failed", "name", toolCall.Function.Name, "error", err)
					} else {
						slog.Info("Tool executed successfully", "name", toolCall.Function.Name, "resultLength", len(result))
					}

					// Add tool result to messages
					toolMsg := openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						Content:    result,
						ToolCallID: toolCall.ID,
					}
					messages = append(messages, toolMsg)
				}

				// Continue the loop to send results back to model
				continue
			}

			// No tool calls - this is the final response
			content := choice.Message.Content
			slog.Info("Final response received", "contentLength", len(content))

			// Add final assistant response to history
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: content,
			})

			return AiResponseMsg{
				Content: content,
				History: messages,
			}
		}

		slog.Warn("Max iterations reached in agentic loop")
		return ErrMsg(errors.New("max iterations reached - possible infinite loop"))
	}
}

// convertToolsToOpenAI converts our ToolDefinition format to OpenAI's Tool format
func convertToolsToOpenAI(defs []agent.ToolDefinition) []openai.Tool {
	var tools []openai.Tool
	for _, def := range defs {
		paramsBytes, _ := json.Marshal(def.Parameters)
		var paramsMap map[string]interface{}
		json.Unmarshal(paramsBytes, &paramsMap)

		tools = append(tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  paramsMap,
			},
		})
	}
	return tools
}
