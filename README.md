# Trace

Trace is an AI-powered coding assistant that lives in your terminal. It uses a TUI (Text User Interface) to interact with LLMs (like Claude or OpenAI models) to help you write code, manage files, and execute commands safely.
(ps: i simply wanted to build an ai agent that would help me commit my files , turns out i wrote something more than that)

## Features

- **TUI Interface**: A clean, keyboard-centric interface built with BubbleTea.
- **Agentic Capabilities**: Can read files, list directories, run shell commands, and edit code.
- **Dynamic Sidebar**: A split-pane view that opens automatically to show long-running command output or terminal logs.
- **Smart Command Resolution**: Automatically resolves common missing binaries (e.g., uses `python3` if `python` is missing).
- **Context Awareness**: Can reference files in chat using `@filename` syntax.

## Setup

1. **Prerequisites**:
   - Go 1.21+
   - An API Key (Anthropic, OpenAI, or OpenRouter)

2. **Configuration**:
   Create a `.env` file in the root directory:

   ```bash
   PROVIDER_API_KEY=your_api_key_here
   PROVIDER_BASE_URL=https://api.openai.com/v1 # or https://openrouter.ai/api/v1
   PROVIDER_MODEL=gpt-4o # or anthropic/claude-3.5-sonnet, etc.
   PROVIDER_AUTH_TOKEN=your_auth_token_here # if your provider requires it
   ```

   # side note: you can get a model on groq for free, 1k free request which should be enough for most use cases

_(Note: The system supports OpenAI-compatible APIs)_

3. **Running**:
   ```bash
   go run .
   ```

## Usage

- **Chat**: Type your request in the input box at the bottom.
- **File References**: Type `@` to trigger autocomplete for filenames. Mentioning a file gives the AI read access to it contextually.
- **Tools**: The AI works by calling tools:
  - `read_file`: Read file contents.
  - `write_file`: Create or overwrite files.
  - `edit_file`: Find and replace text blocks.
  - `list_files`: View project structure.
  - `run_command`: Execute shell commands (output streams to the sidebar).
  - `manage_window`: Open/close the sidebar.

## Key Controls

- `Enter`: Send message
- `Ctrl+C` / `Esc`: Quit (or cancel autocomplete)
