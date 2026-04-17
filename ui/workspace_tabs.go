package ui

import "github.com/charmbracelet/lipgloss"

const maxVisibleTabs = 7

var (
	// notifyInactiveTabStyle is for inactive tabs with a Ready agent — green glow.
	notifyInactiveTabStyle = lipgloss.NewStyle().
				Border(InactiveTabBorder, true).
				BorderForeground(HighlightColor).
				Background(lipgloss.Color("#51bd73")).
				Foreground(lipgloss.Color("#ffffff")).
				AlignHorizontal(lipgloss.Center)

	// notifyActiveTabStyle is for the active tab with a Ready agent.
	notifyActiveTabStyle = lipgloss.NewStyle().
				Border(ActiveTabBorder, true).
				BorderForeground(HighlightColor).
				Background(lipgloss.Color("#51bd73")).
				Foreground(lipgloss.Color("#ffffff")).
				AlignHorizontal(lipgloss.Center)
)

// WorkspaceTab holds the display data for a workspace tab.
type WorkspaceTab struct {
	Name     string
	Path     string
	HasReady bool
}

// WorkspaceTabBar renders a horizontal tab bar for switching between workspaces.
type WorkspaceTabBar struct {
	workspaces []WorkspaceTab
	activeIdx  int
	width      int
}

func NewWorkspaceTabBar() *WorkspaceTabBar {
	return &WorkspaceTabBar{}
}

func (w *WorkspaceTabBar) SetWorkspaces(workspaces []WorkspaceTab) {
	w.workspaces = workspaces
	if w.activeIdx >= len(workspaces) {
		w.activeIdx = 0
	}
}

func (w *WorkspaceTabBar) SetActiveIdx(idx int) {
	if idx >= 0 && idx < len(w.workspaces) {
		w.activeIdx = idx
	}
}

func (w *WorkspaceTabBar) SetWidth(width int) {
	w.width = width
}

// Height returns the rendered height of the tab bar (border + 1 char + leading newline).
func (w *WorkspaceTabBar) Height() int {
	return ActiveTabStyle.GetVerticalFrameSize() + 2
}

// visibleRange returns the start and end indices for the sliding window of tabs.
func (w *WorkspaceTabBar) visibleRange() (start, end int) {
	n := len(w.workspaces)
	if n <= maxVisibleTabs {
		return 0, n
	}

	half := maxVisibleTabs / 2
	start = w.activeIdx - half
	end = start + maxVisibleTabs

	if start < 0 {
		start = 0
		end = maxVisibleTabs
	}
	if end > n {
		end = n
		start = end - maxVisibleTabs
	}
	return start, end
}

func (w *WorkspaceTabBar) View() string {
	if len(w.workspaces) == 0 || w.width == 0 {
		return ""
	}

	visStart, visEnd := w.visibleRange()
	visibleWs := w.workspaces[visStart:visEnd]
	numVisible := len(visibleWs)

	// Reserve space for overflow indicators.
	hasLeftArrow := visStart > 0
	hasRightArrow := visEnd < len(w.workspaces)
	arrowWidth := 0
	if hasLeftArrow {
		arrowWidth += 2
	}
	if hasRightArrow {
		arrowWidth += 2
	}

	totalTabWidth := w.width - arrowWidth
	if totalTabWidth < numVisible {
		totalTabWidth = numVisible
	}
	tabWidth := totalTabWidth / numVisible
	lastTabWidth := totalTabWidth - tabWidth*(numVisible-1)

	arrowStyle := lipgloss.NewStyle().Foreground(HighlightColor).PaddingTop(1)
	var renderedTabs []string

	if hasLeftArrow {
		renderedTabs = append(renderedTabs, arrowStyle.Render("◀ "))
	}

	for i, ws := range visibleWs {
		globalIdx := visStart + i
		isActive := globalIdx == w.activeIdx
		isFirst := i == 0
		isLast := i == numVisible-1

		width := tabWidth
		if isLast {
			width = lastTabWidth
		}

		var style lipgloss.Style
		if isActive && ws.HasReady {
			style = notifyActiveTabStyle
		} else if isActive {
			style = ActiveTabStyle
		} else if ws.HasReady {
			style = notifyInactiveTabStyle
		} else {
			style = InactiveTabStyle
		}

		border, _, _, _, _ := style.GetBorder()
		if isFirst && isActive {
			border.BottomLeft = "│"
		} else if isFirst {
			border.BottomLeft = "├"
		}
		if isLast && isActive {
			border.BottomRight = "│"
		} else if isLast {
			border.BottomRight = "┤"
		}
		style = style.Border(border)

		// Truncate name to fit within tab width.
		name := ws.Name
		innerWidth := width - style.GetHorizontalFrameSize()
		if innerWidth < 1 {
			innerWidth = 1
		}
		if len(name) > innerWidth {
			if innerWidth > 3 {
				name = name[:innerWidth-3] + "..."
			} else {
				name = name[:innerWidth]
			}
		}

		style = style.Width(innerWidth)
		renderedTabs = append(renderedTabs, style.Render(name))
	}

	if hasRightArrow {
		renderedTabs = append(renderedTabs, arrowStyle.Render(" ▶"))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	if lipgloss.Width(row) < w.width {
		gap := w.width - lipgloss.Width(row)
		filler := lipgloss.NewStyle().
			Border(InactiveTabBorder, false, false, true, false).
			BorderForeground(HighlightColor).
			Width(gap).
			Render("")
		row = lipgloss.JoinHorizontal(lipgloss.Top, row, filler)
	}

	return "\n" + row
}
