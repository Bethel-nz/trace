package ui

import "github.com/charmbracelet/lipgloss"

// Nord Theme Colors
var (
	nordPolarNight1  = lipgloss.Color("#2e3440")
	nordPolarNight4  = lipgloss.Color("#4c566a")
	nordSnowStorm    = lipgloss.Color("#eceff4")
	nordFrost1       = lipgloss.Color("#8fbcbb")
	nordFrost2       = lipgloss.Color("#88c0d0")
	nordFrost3       = lipgloss.Color("#81a1c1")
	nordAuroraGreen  = lipgloss.Color("#a3be8c")
	nordAuroraPurple = lipgloss.Color("#b48ead")
	nordAuroraRed    = lipgloss.Color("#bf616a")
	nordAuroraYellow = lipgloss.Color("#ebcb8b")
)

var (
	focusedStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(nordFrost2)
	blurredStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(nordPolarNight4)

	// Layout styles
	fileSelected = lipgloss.NewStyle().Foreground(nordFrost2).Bold(true)
	fileNormal   = lipgloss.NewStyle().Foreground(nordSnowStorm)

	// Chat styles
	// Chat styles
	userStyle  = lipgloss.NewStyle().Foreground(nordAuroraGreen).Bold(true).MarginLeft(2)
	traceStyle = lipgloss.NewStyle().Foreground(nordFrost2).Bold(true).MarginLeft(2)
	mutedStyle = lipgloss.NewStyle().Foreground(nordPolarNight4).Italic(true).MarginLeft(2)
)
