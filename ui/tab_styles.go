package ui

import "github.com/charmbracelet/lipgloss"

// TabBorderWithBottom creates a rounded border with custom bottom characters.
// Used by both the workspace tab bar and the content tabbed window.
func TabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}

var (
	// InactiveTabBorder has a closed bottom connecting to the window below.
	InactiveTabBorder = TabBorderWithBottom("┴", "─", "┴")
	// ActiveTabBorder has an open bottom, visually merging with the window below.
	ActiveTabBorder = TabBorderWithBottom("┘", " ", "└")

	// HighlightColor is the primary accent color for tab borders.
	HighlightColor = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}

	// InactiveTabStyle is the base style for non-selected tabs.
	InactiveTabStyle = lipgloss.NewStyle().
				Border(InactiveTabBorder, true).
				BorderForeground(HighlightColor).
				AlignHorizontal(lipgloss.Center)

	// ActiveTabStyle is the style for the currently selected tab.
	ActiveTabStyle = InactiveTabStyle.
			Border(ActiveTabBorder, true).
			AlignHorizontal(lipgloss.Center)
)
