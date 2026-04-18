package tmux

import (
	"bytes"
	"claude-squad/cmd"
	"claude-squad/log"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const ProgramClaude = "claude"

const ProgramAider = "aider"
const ProgramGemini = "gemini"
const ProgramOpencode = "env -u CLAUDE_CODE_OAUTH_TOKEN -u GITHUB_TOKEN opencode"
const ProgramPiMono = "env -u CLAUDE_CODE_OAUTH_TOKEN -u GITHUB_TOKEN pi"

// resolveBaseProgram extracts the base program name from a full command string.
// It strips paths (e.g., "/usr/local/bin/claude" → "claude"), flags
// (e.g., "aider --model foo" → "aider"), and env wrappers
// (e.g., "env -u VAR opencode" → "opencode"), returning just the binary name.
func resolveBaseProgram(program string) string {
	parts := strings.Fields(program)
	if len(parts) == 0 {
		return program
	}

	// Skip past "env" and its flags/assignments (e.g., "env -u VAR -i KEY=VAL program")
	i := 0
	if filepath.Base(parts[0]) == "env" {
		i++
		for i < len(parts) {
			if strings.HasPrefix(parts[i], "-") {
				// Flag like -u, -i; if it's -u or -S, skip the next arg too
				if (parts[i] == "-u" || parts[i] == "-S") && i+1 < len(parts) {
					i += 2
				} else {
					i++
				}
			} else if strings.Contains(parts[i], "=") {
				// Environment variable assignment like KEY=VAL
				i++
			} else {
				break
			}
		}
	}

	if i >= len(parts) {
		return filepath.Base(parts[0])
	}
	return filepath.Base(parts[i])
}

// TmuxSession represents a managed tmux session
type TmuxSession struct {
	// Initialized by NewTmuxSession
	//
	// The name of the tmux session and the sanitized name used for tmux commands.
	sanitizedName string
	program       string
	// ptyFactory is used to create a PTY for the tmux session.
	ptyFactory PtyFactory
	// cmdExec is used to execute commands in the tmux session.
	cmdExec cmd.Executor

	// Initialized by Start or Restore
	//
	// ptmx is a PTY running the tmux attach command. Only non-nil while attached
	// (between Attach and Detach). When detached, all interaction with the tmux
	// session goes through tmux CLI commands (send-keys, resize-window, etc.).
	ptmx *os.File
	// monitor monitors the tmux pane content and sends signals to the UI when it's status changes
	monitor *statusMonitor

	// Initialized by Attach
	// Deinitilaized by Detach
	//
	// Channel to be closed at the very end of detaching. Used to signal callers.
	attachCh chan struct{}
	// While attached, we use some goroutines to manage the window size and stdin/stdout. This stuff
	// is used to terminate them on Detach. We don't want them to outlive the attached window.
	ctx    context.Context
	cancel func()
	wg     *sync.WaitGroup

	// lastCols/lastRows cache the last-applied window size so redundant
	// resize calls (from the periodic poll) can be skipped.
	// sizeMu protects lastCols/lastRows: SetDetachedSize is called from the
	// Bubbletea main loop while monitorWindowSize goroutines call updateWindowSize
	// concurrently during attach.
	sizeMu   sync.Mutex
	lastCols int
	lastRows int

	// savedTermState holds the terminal state captured just before Attach() so
	// that Detach() can restore it. Attached programs (pi, opencode) may enable
	// extended key reporting or mouse modes whose escape sequences get forwarded
	// to the outer terminal; without an explicit restore those modes persist
	// after detach and corrupt the TUI.
	savedTermState *term.State

	// suppressNextUpdate, when true, causes the next HasUpdated() call to
	// consume the current pane content into the hash without reporting a change.
	// Set by Detach() to absorb the resize-triggered redraw that happens when
	// SetDetachedSize restores the session to preview dimensions after detach.
	// Without this, the resize causes a spurious updated=true → Running transition
	// that clears a freshly-acknowledged ReadyAcknowledged flag.
	suppressNextUpdate atomic.Bool
}

const TmuxPrefix = "claudesquad_"

var whiteSpaceRegex = regexp.MustCompile(`\s+`)

func toClaudeSquadTmuxName(str string) string {
	str = whiteSpaceRegex.ReplaceAllString(str, "")
	str = strings.ReplaceAll(str, ".", "_") // tmux replaces all . with _
	return fmt.Sprintf("%s%s", TmuxPrefix, str)
}

// NewTmuxSession creates a new TmuxSession with the given name and program.
func NewTmuxSession(name string, program string) *TmuxSession {
	return newTmuxSession(name, program, MakePtyFactory(), cmd.MakeExecutor())
}

// NewTmuxSessionWithDeps creates a new TmuxSession with provided dependencies for testing.
func NewTmuxSessionWithDeps(name string, program string, ptyFactory PtyFactory, cmdExec cmd.Executor) *TmuxSession {
	return newTmuxSession(name, program, ptyFactory, cmdExec)
}

func newTmuxSession(name string, program string, ptyFactory PtyFactory, cmdExec cmd.Executor) *TmuxSession {
	return &TmuxSession{
		sanitizedName: toClaudeSquadTmuxName(name),
		program:       program,
		ptyFactory:    ptyFactory,
		cmdExec:       cmdExec,
	}
}

// Start creates and starts a new tmux session, then attaches to it. Program is the command to run in
// the session (ex. claude). workdir is the git worktree directory.
func (t *TmuxSession) Start(workDir string) error {
	// Check if the session already exists
	if t.DoesSessionExist() {
		return fmt.Errorf("tmux session already exists: %s", t.sanitizedName)
	}

	// Create a new detached tmux session and start claude in it
	cmd := exec.Command("tmux", "new-session", "-d", "-s", t.sanitizedName, "-c", workDir, t.program)

	ptmx, err := t.ptyFactory.Start(cmd)
	if err != nil {
		// Cleanup any partially created session if any exists.
		if t.DoesSessionExist() {
			cleanupCmd := exec.Command("tmux", "kill-session", "-t", t.sanitizedName)
			if cleanupErr := t.cmdExec.Run(cleanupCmd); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
		}
		return fmt.Errorf("error starting tmux session: %w", err)
	}

	// Poll for session existence with exponential backoff
	timeout := time.After(2 * time.Second)
	sleepDuration := 5 * time.Millisecond
	for !t.DoesSessionExist() {
		select {
		case <-timeout:
			if cleanupErr := t.Close(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
			return fmt.Errorf("timed out waiting for tmux session %s: %v", t.sanitizedName, err)
		default:
			time.Sleep(sleepDuration)
			// Exponential backoff up to 50ms max
			if sleepDuration < 50*time.Millisecond {
				sleepDuration *= 2
			}
		}
	}
	ptmx.Close()

	// Set history limit to enable scrollback (default is 2000, we'll use 10000 for more history)
	historyCmd := exec.Command("tmux", "set-option", "-t", t.sanitizedName, "history-limit", "10000")
	if err := t.cmdExec.Run(historyCmd); err != nil {
		log.InfoLog.Printf("Warning: failed to set history-limit for session %s: %v", t.sanitizedName, err)
	}

	// Enable mouse scrolling for the session
	mouseCmd := exec.Command("tmux", "set-option", "-t", t.sanitizedName, "mouse", "on")
	if err := t.cmdExec.Run(mouseCmd); err != nil {
		log.InfoLog.Printf("Warning: failed to enable mouse scrolling for session %s: %v", t.sanitizedName, err)
	}

	err = t.Restore()
	if err != nil {
		if cleanupErr := t.Close(); cleanupErr != nil {
			err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
		}
		return fmt.Errorf("error restoring tmux session: %w", err)
	}

	return nil
}

// CheckAndHandleTrustPrompt checks the pane content once for a trust prompt and dismisses it if found.
// Returns true if the prompt was found and handled.
func (t *TmuxSession) CheckAndHandleTrustPrompt() bool {
	content, err := t.CapturePaneContent()
	if err != nil {
		return false
	}

	base := resolveBaseProgram(t.program)
	switch base {
	case ProgramClaude:
		if strings.Contains(content, "Do you trust the files in this folder?") ||
			strings.Contains(content, "new MCP server") {
			if err := t.TapEnter(); err != nil {
				log.ErrorLog.Printf("could not tap enter on trust/MCP screen: %v", err)
			}
			return true
		}
	default:
		if strings.Contains(content, "Open documentation url for more info") {
			if err := t.TapDAndEnter(); err != nil {
				log.ErrorLog.Printf("could not tap enter on trust screen: %v", err)
			}
			return true
		}
	}
	return false
}

// Restore verifies that an existing tmux session is alive and initializes
// the status monitor. It does NOT attach a PTY — multiple cs processes can
// safely call Restore on the same tmux session without interfering with
// each other. A PTY is only created when Attach() is called for interactive use.
func (t *TmuxSession) Restore() error {
	if !t.DoesSessionExist() {
		return fmt.Errorf("tmux session does not exist: %s", t.sanitizedName)
	}
	t.monitor = newStatusMonitor()
	return nil
}

type statusMonitor struct {
	// Store hashes to save memory.
	prevOutputHash []byte
}

func newStatusMonitor() *statusMonitor {
	return &statusMonitor{}
}

// hash hashes the string.
func (m *statusMonitor) hash(s string) []byte {
	h := sha256.New()
	// TODO: this allocation sucks since the string is probably large. Ideally, we hash the string directly.
	h.Write([]byte(s))
	return h.Sum(nil)
}

// TapEnter sends an enter keystroke to the tmux pane via tmux send-keys.
func (t *TmuxSession) TapEnter() error {
	cmd := exec.Command("tmux", "send-keys", "-t", t.sanitizedName, "Enter")
	if err := t.cmdExec.Run(cmd); err != nil {
		return fmt.Errorf("error sending enter keystroke: %w", err)
	}
	return nil
}

// TapDAndEnter sends 'D' followed by an enter keystroke to the tmux pane.
func (t *TmuxSession) TapDAndEnter() error {
	cmd := exec.Command("tmux", "send-keys", "-t", t.sanitizedName, "D", "Enter")
	if err := t.cmdExec.Run(cmd); err != nil {
		return fmt.Errorf("error sending D+Enter keystrokes: %w", err)
	}
	return nil
}

// SendKeys sends literal text to the tmux pane via tmux send-keys.
func (t *TmuxSession) SendKeys(keys string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", t.sanitizedName, "-l", keys)
	return t.cmdExec.Run(cmd)
}

// HasUpdated checks if the tmux pane content has changed since the last tick. It also returns true if
// the tmux pane has a prompt for aider or claude code.
func (t *TmuxSession) HasUpdated() (updated bool, hasPrompt bool) {
	content, err := t.CapturePaneContent()
	if err != nil {
		log.ErrorLog.Printf("error capturing pane content in status monitor: %v", err)
		return false, false
	}

	// Detect idle prompts per program using the normalized base name.
	base := resolveBaseProgram(t.program)
	switch base {
	case ProgramClaude:
		hasPrompt = strings.Contains(content, "No, and tell Claude what to do differently")
	case ProgramAider:
		hasPrompt = strings.Contains(content, "(Y)es/(N)o/(D)on't ask again")
	case ProgramGemini:
		hasPrompt = strings.Contains(content, "Yes, allow once")
	case ProgramOpencode:
		// TODO: discover exact idle prompt string by running opencode
		hasPrompt = strings.Contains(content, ">")
	case ProgramPiMono:
		// TODO: discover exact idle prompt string by running pi
		hasPrompt = strings.Contains(content, ">")
	}

	newHash := t.monitor.hash(content)
	if t.suppressNextUpdate.Swap(false) {
		// Absorb the resize-triggered redraw that follows a Detach(). Update the
		// hash so future checks compare from the post-resize content, but do not
		// report this as an agent update.
		t.monitor.prevOutputHash = newHash
		return false, hasPrompt
	}
	if !bytes.Equal(newHash, t.monitor.prevOutputHash) {
		t.monitor.prevOutputHash = newHash
		return true, hasPrompt
	}
	return false, hasPrompt
}

func (t *TmuxSession) Attach() (chan struct{}, error) {
	// Save terminal state so Detach() can restore it. Attached programs may
	// enable extended key reporting or mouse modes; those ANSI sequences are
	// forwarded to the outer terminal via io.Copy and persist after detach
	// unless we explicitly undo them.
	if state, err := term.GetState(int(os.Stdin.Fd())); err == nil {
		t.savedTermState = state
	}

	// Create the PTY attachment to the tmux session. This is the ONLY place a
	// PTY is created — Restore() deliberately does not create one so that
	// multiple cs processes can coexist without disrupting each other's PTYs.
	ptmx, err := t.ptyFactory.Start(exec.Command("tmux", "attach-session", "-t", t.sanitizedName))
	if err != nil {
		return nil, fmt.Errorf("error attaching to tmux session: %w", err)
	}
	t.ptmx = makeNonBlockingFile(ptmx, "pty-master")

	t.attachCh = make(chan struct{})

	t.wg = &sync.WaitGroup{}
	t.wg.Add(1)
	t.ctx, t.cancel = context.WithCancel(context.Background())

	// The first goroutine should terminate when the ptmx is closed. We use the
	// waitgroup to wait for it to finish.
	// The 2nd one returns when you press escape to Detach. It doesn't need to be
	// in the waitgroup because is the goroutine doing the Detaching; it waits for
	// all the other ones.
	go func() {
		defer t.wg.Done()
		_, _ = io.Copy(os.Stdout, t.ptmx)
		// When io.Copy returns, it means the connection was closed
		// This could be due to normal detach or Ctrl-D
		// Check if the context is done to determine if it was a normal detach
		select {
		case <-t.ctx.Done():
			// Normal detach, do nothing
		default:
			// If context is not done, it was likely an abnormal termination (Ctrl-D)
			// Print warning message
			fmt.Fprintf(os.Stderr, "\n\033[31mError: Session terminated without detaching. Use Ctrl-Q to properly detach from tmux sessions.\033[0m\n")
		}
	}()

	go func() {
		// Close the channel after 50ms
		timeoutCh := make(chan struct{})
		go func() {
			time.Sleep(50 * time.Millisecond)
			close(timeoutCh)
		}()

		// Read input from stdin and check for Ctrl+q
		buf := make([]byte, 32)
		for {
			nr, err := os.Stdin.Read(buf)
			if err != nil {
				if err == io.EOF {
					break
				}
				continue
			}

			// Nuke the first bytes of stdin, up to 64, to prevent tmux from reading it.
			// When we attach, there tends to be terminal control sequences like ?[?62c0;95;0c or
			// ]10;rgb:f8f8f8. The control sequences depend on the terminal (warp vs iterm). We should use regex ideally
			// but this works well for now. Log this for debugging.
			//
			// There seems to always be control characters, but I think it's possible for there not to be. The heuristic
			// here can be: if there's characters within 50ms, then assume they are control characters and nuke them.
			select {
			case <-timeoutCh:
			default:
				log.InfoLog.Printf("nuked first stdin: %s", buf[:nr])
				continue
			}

			// Check for Ctrl+q (ASCII 17) anywhere in the read buffer.
			// We scan the full buffer rather than requiring nr==1 because TUI programs
			// (pi, opencode) enable mouse reporting, causing terminal sequences to arrive
			// on stdin interleaved with keystrokes. When Ctrl+Q lands alongside other bytes,
			// nr>1 and a single-byte check never fires — making detach feel frozen.
			for i := 0; i < nr; i++ {
				if buf[i] == 17 {
					t.Detach()
					return
				}
			}

			// Forward other input to tmux
			_, _ = t.ptmx.Write(buf[:nr])
		}
	}()

	t.monitorWindowSize()
	return t.attachCh, nil
}

// DetachSafely disconnects from the current tmux session without panicking
func (t *TmuxSession) DetachSafely() error {
	// Only detach if we're actually attached
	if t.attachCh == nil {
		return nil // Already detached
	}

	var errs []error

	// Close the attached pty session.
	if t.ptmx != nil {
		if err := t.ptmx.Close(); err != nil {
			errs = append(errs, fmt.Errorf("error closing attach pty session: %w", err))
		}
		t.ptmx = nil
	}

	// Clean up attach state
	if t.attachCh != nil {
		close(t.attachCh)
		t.attachCh = nil
	}

	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}

	if t.wg != nil {
		t.wg.Wait()
		t.wg = nil
	}

	t.ctx = nil

	if len(errs) > 0 {
		return fmt.Errorf("errors during detach: %v", errs)
	}
	return nil
}

// Detach disconnects from the current tmux session. It panics if detaching fails. At the moment, there's no
// way to recover from a failed detach.
func (t *TmuxSession) Detach() {
	defer func() {
		close(t.attachCh)
		t.attachCh = nil
		t.cancel = nil
		t.ctx = nil
		t.wg = nil
	}()

	// Close the attached PTY. This causes the io.Copy goroutine in Attach to
	// receive EOF and exit. No new PTY is created — when detached, all tmux
	// interaction goes through CLI commands (send-keys, resize-window, etc.).
	if t.ptmx != nil {
		err := t.ptmx.Close()
		if err != nil {
			msg := fmt.Sprintf("error closing attach pty session: %v", err)
			log.ErrorLog.Println(msg)
			panic(msg)
		}
		t.ptmx = nil
	}

	// Cancel goroutines created by Attach.
	t.cancel()
	t.wg.Wait()

	// Reset cached dimensions so the next SetDetachedSize call (from the
	// preview pane) always issues a resize instead of being skipped because
	// lastCols/lastRows still hold the full-terminal values set during attach.
	t.sizeMu.Lock()
	t.lastCols = 0
	t.lastRows = 0
	t.sizeMu.Unlock()

	// Absorb the resize-triggered redraw. SetDetachedSize (called from the
	// Bubbletea loop momentarily after this) will resize the tmux window back
	// to preview dimensions, causing the program inside to redraw. The next
	// HasUpdated() call will see a changed hash and falsely report updated=true,
	// which would set ReadyAcknowledged=false and re-show the blink even though
	// the user just acknowledged it. Suppress exactly that one check.
	t.suppressNextUpdate.Store(true)

	// Restore the terminal state saved at Attach time. This undoes any tty-level
	// changes (raw mode, echo flags, etc.) that the attached program may have made.
	if t.savedTermState != nil {
		_ = term.Restore(int(os.Stdin.Fd()), t.savedTermState)
		t.savedTermState = nil
	}

	// Send explicit ANSI sequences to disable application-level terminal modes
	// that term.Restore (tcsetattr) cannot undo. Programs like pi/opencode enable
	// extended key reporting and mouse tracking; without these resets those modes
	// persist in the terminal emulator after detach and corrupt the TUI.
	_, _ = os.Stdout.Write([]byte(
		"\033[>4;0m" +   // XTerm modifyOtherKeys off
		"\033[=0u" +     // kitty keyboard protocol off
		"\033[?1000l" +  // mouse reporting off
		"\033[?1002l" +  // mouse cell-motion off
		"\033[?1003l" +  // all-motion mouse off
		"\033[?1006l" +  // SGR mouse extension off
		"\033[?2004l",   // bracketed paste off
	))
}

// Close terminates the tmux session and cleans up resources
func (t *TmuxSession) Close() error {
	var errs []error

	if t.ptmx != nil {
		if err := t.ptmx.Close(); err != nil {
			errs = append(errs, fmt.Errorf("error closing PTY: %w", err))
		}
		t.ptmx = nil
	}

	cmd := exec.Command("tmux", "kill-session", "-t", t.sanitizedName)
	if err := t.cmdExec.Run(cmd); err != nil {
		errs = append(errs, fmt.Errorf("error killing tmux session: %w", err))
	}

	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple errors occurred during cleanup:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return errors.New(errMsg)
}

// SetDetachedSize set the width and height of the session while detached. This makes the
// tmux output conform to the specified shape.
func (t *TmuxSession) SetDetachedSize(width, height int) error {
	t.sizeMu.Lock()
	if width == t.lastCols && height == t.lastRows {
		t.sizeMu.Unlock()
		return nil
	}
	t.lastCols = width
	t.lastRows = height
	t.sizeMu.Unlock()
	return t.resizeViaTmux(width, height)
}

// resizeViaTmux resizes the tmux window using the tmux CLI. Works whether
// or not a PTY is attached.
func (t *TmuxSession) resizeViaTmux(cols, rows int) error {
	cmd := exec.Command("tmux", "resize-window", "-t", t.sanitizedName,
		"-x", fmt.Sprintf("%d", cols), "-y", fmt.Sprintf("%d", rows))
	return t.cmdExec.Run(cmd)
}

// updateWindowSize updates the window size. When a PTY is attached, it resizes
// both the tmux window (to undo any forced size from SetDetachedSize) and the
// PTY. Otherwise falls back to tmux CLI only.
func (t *TmuxSession) updateWindowSize(cols, rows int) error {
	t.sizeMu.Lock()
	if cols == t.lastCols && rows == t.lastRows {
		t.sizeMu.Unlock()
		return nil
	}
	t.lastCols = cols
	t.lastRows = rows
	t.sizeMu.Unlock()

	if t.ptmx != nil {
		// Resize the tmux window first to undo any forced size from SetDetachedSize.
		_ = t.resizeViaTmux(cols, rows)
		return pty.Setsize(t.ptmx, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
			X:    0,
			Y:    0,
		})
	}
	return t.resizeViaTmux(cols, rows)
}

func (t *TmuxSession) DoesSessionExist() bool {
	// Using "-t name" does a prefix match, which is wrong. `-t=` does an exact match.
	existsCmd := exec.Command("tmux", "has-session", fmt.Sprintf("-t=%s", t.sanitizedName))
	return t.cmdExec.Run(existsCmd) == nil
}

// CapturePaneContent captures the content of the tmux pane
func (t *TmuxSession) CapturePaneContent() (string, error) {
	// Add -e flag to preserve escape sequences (ANSI color codes)
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-t", t.sanitizedName)
	output, err := t.cmdExec.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("error capturing pane content: %v", err)
	}
	return string(output), nil
}

// CapturePaneContentWithOptions captures the pane content with additional options
// start and end specify the starting and ending line numbers (use "-" for the start/end of history)
func (t *TmuxSession) CapturePaneContentWithOptions(start, end string) (string, error) {
	// Add -e flag to preserve escape sequences (ANSI color codes)
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-S", start, "-E", end, "-t", t.sanitizedName)
	output, err := t.cmdExec.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to capture tmux pane content with options: %v", err)
	}
	return string(output), nil
}

// CleanupSessions kills all tmux sessions that start with "session-"
func CleanupSessions(cmdExec cmd.Executor) error {
	// First try to list sessions
	cmd := exec.Command("tmux", "ls")
	output, err := cmdExec.Output(cmd)

	// If there's an error and it's because no server is running, that's fine
	// Exit code 1 typically means no sessions exist
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil // No sessions to clean up
		}
		return fmt.Errorf("failed to list tmux sessions: %v", err)
	}

	re := regexp.MustCompile(fmt.Sprintf(`%s.*:`, TmuxPrefix))
	matches := re.FindAllString(string(output), -1)
	for i, match := range matches {
		matches[i] = match[:strings.Index(match, ":")]
	}

	for _, match := range matches {
		log.InfoLog.Printf("cleaning up session: %s", match)
		if err := cmdExec.Run(exec.Command("tmux", "kill-session", "-t", match)); err != nil {
			return fmt.Errorf("failed to kill tmux session %s: %v", match, err)
		}
	}
	return nil
}
