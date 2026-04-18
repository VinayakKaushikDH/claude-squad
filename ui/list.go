package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const readyIcon = "● "
const pausedIcon = "⏸ "

var readyStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var dimReadyStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#2d6b3f", Dark: "#2d6b3f"})

var addedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var pausedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})

var titleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})

var listDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})

var selectedTitleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var selectedDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var mainTitle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230"))

var autoYesStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.Color("#1a1a1a"))

type List struct {
	items         []*session.Instance
	selectedIdx   int // Index into filteredIdxs (visible selection)
	height, width int
	renderer      *InstanceRenderer
	autoyes       bool

	// Workspace filtering: when filterPath is non-empty, only instances with
	// matching Path are visible. filteredIdxs maps visible positions to items indices.
	filterPath    string
	filteredIdxs  []int
	selectionMemo map[string]int // workspace path -> last selectedIdx

	// map of repo name to number of instances using it. Used to display the repo name only if there are
	// multiple repos in play.
	repos map[string]int

	// blinkFrame increments on each spinner tick; used to pulse Ready icons at ~1 Hz.
	blinkFrame int
}

func NewList(spinner *spinner.Model, autoYes bool) *List {
	return &List{
		items:         []*session.Instance{},
		renderer:      &InstanceRenderer{spinner: spinner},
		repos:         make(map[string]int),
		selectionMemo: make(map[string]int),
		autoyes:       autoYes,
	}
}

// IncrementBlinkFrame advances the blink frame counter (called on each spinner tick).
func (l *List) IncrementBlinkFrame() {
	l.blinkFrame++
}

// blinkOn returns whether the Ready icon should be in its bright phase.
// Toggles every 6 spinner frames (~500ms at 12 FPS) for a ~1 Hz pulse.
func (l *List) blinkOn() bool {
	return (l.blinkFrame/6)%2 == 0
}

// SetSize sets the height and width of the list.
func (l *List) SetSize(width, height int) {
	l.width = width
	l.height = height
	l.renderer.setWidth(width)
}

// SetSessionPreviewSize sets the height and width for the tmux sessions. This makes the stdout line have the correct
// width and height.
func (l *List) SetSessionPreviewSize(width, height int) (err error) {
	for i, item := range l.items {
		if !item.Started() || item.Paused() {
			continue
		}

		if innerErr := item.SetPreviewSize(width, height); innerErr != nil {
			err = errors.Join(
				err, fmt.Errorf("could not set preview size for instance %d: %v", i, innerErr))
		}
	}
	return
}

// NumInstances returns the count of visible (filtered) instances.
func (l *List) NumInstances() int {
	return len(l.filteredIdxs)
}

// NumAllInstances returns the total count of all instances regardless of filter.
func (l *List) NumAllInstances() int {
	return len(l.items)
}

// recomputeFilter rebuilds filteredIdxs based on the current filterPath.
func (l *List) recomputeFilter() {
	l.filteredIdxs = l.filteredIdxs[:0]
	for i, inst := range l.items {
		if l.filterPath == "" || inst.Path == l.filterPath {
			l.filteredIdxs = append(l.filteredIdxs, i)
		}
	}
	if l.selectedIdx >= len(l.filteredIdxs) {
		l.selectedIdx = len(l.filteredIdxs) - 1
	}
	if l.selectedIdx < 0 {
		l.selectedIdx = 0
	}
}

// SetFilter sets the workspace filter path. Saves the current selection state
// for the old filter and restores any saved state for the new filter.
func (l *List) SetFilter(path string) {
	// Save current selection for old filter.
	if l.filterPath != "" {
		l.selectionMemo[l.filterPath] = l.selectedIdx
	}
	l.filterPath = path
	l.recomputeFilter()
	// Restore saved selection for new filter.
	if saved, ok := l.selectionMemo[path]; ok && saved < len(l.filteredIdxs) {
		l.selectedIdx = saved
	} else {
		l.selectedIdx = 0
	}
}

// GetVisibleInstances returns only the instances matching the current filter.
func (l *List) GetVisibleInstances() []*session.Instance {
	out := make([]*session.Instance, len(l.filteredIdxs))
	for i, idx := range l.filteredIdxs {
		out[i] = l.items[idx]
	}
	return out
}

// InstanceRenderer handles rendering of session.Instance objects
type InstanceRenderer struct {
	spinner *spinner.Model
	width   int
}

func (r *InstanceRenderer) setWidth(width int) {
	r.width = AdjustPreviewWidth(width)
}

// ɹ and ɻ are other options.
const branchIcon = "Ꮧ"

func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, hasMultipleRepos bool, blinkOn bool) string {
	prefix := fmt.Sprintf(" %d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	titleS := selectedTitleStyle
	descS := selectedDescStyle
	if !selected {
		titleS = titleStyle
		descS = listDescStyle
	}

	// add spinner next to title if it's running
	var join string
	switch i.Status {
	case session.Running, session.Loading:
		join = fmt.Sprintf("%s ", r.spinner.View())
	case session.Ready:
		if blinkOn {
			join = readyStyle.Render(readyIcon)
		} else {
			join = dimReadyStyle.Render(readyIcon)
		}
	case session.Paused:
		join = pausedStyle.Render(pausedIcon)
	default:
	}

	// Cut the title if it's too long
	titleText := i.Title
	widthAvail := r.width - 3 - runewidth.StringWidth(prefix) - 1
	if widthAvail > 0 && runewidth.StringWidth(titleText) > widthAvail {
		titleText = runewidth.Truncate(titleText, widthAvail-3, "...")
	}
	title := titleS.Render(lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.Place(r.width-3, 1, lipgloss.Left, lipgloss.Center, fmt.Sprintf("%s %s", prefix, titleText)),
		" ",
		join,
	))

	stat := i.GetDiffStats()

	var diff string
	var addedDiff, removedDiff string
	if stat == nil || stat.Error != nil || stat.IsEmpty() {
		// Don't show diff stats if there's an error or if they don't exist
		addedDiff = ""
		removedDiff = ""
		diff = ""
	} else {
		addedDiff = fmt.Sprintf("+%d", stat.Added)
		removedDiff = fmt.Sprintf("-%d ", stat.Removed)
		diff = lipgloss.JoinHorizontal(
			lipgloss.Center,
			addedLinesStyle.Background(descS.GetBackground()).Render(addedDiff),
			lipgloss.Style{}.Background(descS.GetBackground()).Foreground(descS.GetForeground()).Render(","),
			removedLinesStyle.Background(descS.GetBackground()).Render(removedDiff),
		)
	}

	remainingWidth := r.width
	remainingWidth -= runewidth.StringWidth(prefix)
	remainingWidth -= runewidth.StringWidth(branchIcon)
	remainingWidth -= 2 // for the literal " " and "-" in the branchLine format string

	diffWidth := runewidth.StringWidth(addedDiff) + runewidth.StringWidth(removedDiff)
	if diffWidth > 0 {
		diffWidth += 1
	}

	// Use fixed width for diff stats to avoid layout issues
	remainingWidth -= diffWidth

	branch := i.Branch
	if i.Started() && hasMultipleRepos {
		repoName, err := i.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name in instance renderer: %v", err)
		} else {
			branch += fmt.Sprintf(" (%s)", repoName)
		}
	}
	// Don't show branch if there's no space for it. Or show ellipsis if it's too long.
	branchWidth := runewidth.StringWidth(branch)
	if remainingWidth < 0 {
		branch = ""
	} else if remainingWidth < branchWidth {
		if remainingWidth < 3 {
			branch = ""
		} else {
			// We know the remainingWidth is at least 4 and branch is longer than that, so this is safe.
			branch = runewidth.Truncate(branch, remainingWidth-3, "...")
		}
	}
	remainingWidth -= runewidth.StringWidth(branch)

	// Add spaces to fill the remaining width.
	spaces := ""
	if remainingWidth > 0 {
		spaces = strings.Repeat(" ", remainingWidth)
	}

	branchLine := fmt.Sprintf("%s %s-%s%s%s", strings.Repeat(" ", len(prefix)), branchIcon, branch, spaces, diff)

	// join title and subtitle
	text := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		descS.Render(branchLine),
	)

	return text
}

func (l *List) String() string {
	const titleText = " Instances "
	const autoYesText = " auto-yes "

	// Write the title.
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("\n")

	// Write title line
	// add padding of 2 because the border on list items adds some extra characters
	titleWidth := AdjustPreviewWidth(l.width) + 2
	if !l.autoyes {
		b.WriteString(lipgloss.Place(
			titleWidth, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText)))
	} else {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		autoYes := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom, autoYesStyle.Render(autoYesText))
		b.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top, title, autoYes))
	}

	b.WriteString("\n")
	b.WriteString("\n")

	// Render only visible (filtered) instances.
	blink := l.blinkOn()
	for visIdx, actualIdx := range l.filteredIdxs {
		item := l.items[actualIdx]
		b.WriteString(l.renderer.Render(item, visIdx+1, visIdx == l.selectedIdx, len(l.repos) > 1, blink))
		if visIdx != len(l.filteredIdxs)-1 {
			b.WriteString("\n\n")
		}
	}
	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, b.String())
}

// Down selects the next visible item in the list.
func (l *List) Down() {
	if len(l.filteredIdxs) == 0 {
		return
	}
	if l.selectedIdx < len(l.filteredIdxs)-1 {
		l.selectedIdx++
	}
}

// Kill removes the currently selected visible instance.
func (l *List) Kill() {
	if len(l.filteredIdxs) == 0 {
		return
	}
	actualIdx := l.filteredIdxs[l.selectedIdx]
	targetInstance := l.items[actualIdx]

	// Kill the tmux session
	if err := targetInstance.Kill(); err != nil {
		log.ErrorLog.Printf("could not kill instance: %v", err)
	}

	// Unregister the reponame.
	repoName, err := targetInstance.RepoName()
	if err != nil {
		log.ErrorLog.Printf("could not get repo name: %v", err)
	} else {
		l.rmRepo(repoName)
	}

	// Remove from master list and recompute filter.
	// recomputeFilter() handles clamping selectedIdx.
	l.items = append(l.items[:actualIdx], l.items[actualIdx+1:]...)
	l.recomputeFilter()
}

func (l *List) Attach() (chan struct{}, error) {
	inst := l.GetSelectedInstance()
	if inst == nil {
		return nil, fmt.Errorf("no instance selected")
	}
	return inst.Attach()
}

// Up selects the prev visible item in the list.
func (l *List) Up() {
	if len(l.filteredIdxs) == 0 {
		return
	}
	if l.selectedIdx > 0 {
		l.selectedIdx--
	}
}

func (l *List) addRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		l.repos[repo] = 0
	}
	l.repos[repo]++
}

func (l *List) rmRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		log.ErrorLog.Printf("repo %s not found", repo)
		return
	}
	l.repos[repo]--
	if l.repos[repo] == 0 {
		delete(l.repos, repo)
	}
}

// AddInstance adds a new instance to the list and recomputes the filter.
// It returns a finalizer function that should be called when the instance
// is started. If the instance was restored from storage or is paused, you can call the finalizer immediately.
// When creating a new one and entering the name, you want to call the finalizer once the name is done.
func (l *List) AddInstance(instance *session.Instance) (finalize func()) {
	l.items = append(l.items, instance)
	l.recomputeFilter()
	// The finalizer registers the repo name once the instance is started.
	return func() {
		repoName, err := instance.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name: %v", err)
			return
		}

		l.addRepo(repoName)
	}
}

// RemoveInstanceByTitle removes an instance by title without calling Kill().
// Used for cross-process merge when an instance was deleted by another process.
func (l *List) RemoveInstanceByTitle(title string) {
	for i, inst := range l.items {
		if inst.Title == title {
			// Unregister the repo name if the instance was started.
			if inst.Started() {
				repoName, err := inst.RepoName()
				if err == nil {
					l.rmRepo(repoName)
				}
			}
			l.items = append(l.items[:i], l.items[i+1:]...)
			l.recomputeFilter()
			return
		}
	}
}

// GetInstanceByID returns the instance with the given ID, or nil if not found.
func (l *List) GetInstanceByID(id string) *session.Instance {
	if id == "" {
		return nil
	}
	for _, inst := range l.items {
		if inst.ID == id {
			return inst
		}
	}
	return nil
}

// GetSelectedInstance returns the currently selected visible instance.
func (l *List) GetSelectedInstance() *session.Instance {
	if len(l.filteredIdxs) == 0 {
		return nil
	}
	if l.selectedIdx >= len(l.filteredIdxs) {
		return nil
	}
	return l.items[l.filteredIdxs[l.selectedIdx]]
}

// SetSelectedInstance sets the selected index within the visible list.
// Noop if the index is out of bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.filteredIdxs) {
		return
	}
	l.selectedIdx = idx
}

// SelectInstance finds and selects the given instance in the visible list.
func (l *List) SelectInstance(target *session.Instance) {
	for visIdx, actualIdx := range l.filteredIdxs {
		if l.items[actualIdx] == target {
			l.selectedIdx = visIdx
			return
		}
	}
}

// GetInstances returns all instances in the list
func (l *List) GetInstances() []*session.Instance {
	return l.items
}
