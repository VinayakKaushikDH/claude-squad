//go:build !windows

package tmux

import (
	"claude-squad/log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

// makeNonBlockingFile converts a blocking *os.File (e.g., a PTY master from
// creack/pty) into a non-blocking *os.File backed by Go's runtime netpoller.
//
// Why this matters: for blocking (non-netpoller) files, Go's os.File.Close()
// waits via runtime_Semacquire for any in-progress Read to return before
// actually closing the underlying fd. When a goroutine is blocked inside
// io.Copy(..., ptmx) waiting for PTY output and the agent is idle, that Read
// never returns — so Close() deadlocks indefinitely.
//
// By dup-ing the fd, setting O_NONBLOCK on the dup, and wrapping it with
// os.NewFile, Go 1.23+ detects the non-blocking flag and registers the fd with
// kqueue/epoll (the netpoller). Close() then calls pd.evict() which immediately
// unparks any goroutine blocked in Read, eliminating the deadlock.
//
// Falls back to returning the original file unchanged if any step fails.
func makeNonBlockingFile(f *os.File, name string) *os.File {
	// f.Fd() reverts f's underlying fd to blocking mode as a side effect,
	// but f still owns the fd — we dup it so we can manage the copy ourselves.
	rawFd := int(f.Fd())
	newFd, err := syscall.Dup(rawFd)
	if err != nil {
		return f
	}
	if err := syscall.SetNonblock(newFd, true); err != nil {
		syscall.Close(newFd)
		return f
	}
	// Close the original blocking wrapper; the dup keeps the PTY master alive.
	f.Close()
	// os.NewFile in Go 1.12+ detects O_NONBLOCK and uses the netpoller,
	// so Close() on the returned file will interrupt blocked reads immediately.
	return os.NewFile(uintptr(newFd), name)
}

// monitorWindowSize monitors and handles window resize events while attached.
// In addition to SIGWINCH, it polls every 2 seconds to reassert the correct
// window size. This handles the case where another cs process calls
// resize-window (for its preview pane) on the same tmux session — SIGWINCH
// is not delivered for tmux-initiated resizes, so polling is the only way
// to recover.
func (t *TmuxSession) monitorWindowSize() {
	winchChan := make(chan os.Signal, 1)
	signal.Notify(winchChan, syscall.SIGWINCH)
	// Send initial SIGWINCH to trigger the first resize
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)

	everyN := log.NewEvery(60 * time.Second)

	doUpdate := func() {
		// Use the current terminal height and width.
		cols, rows, err := term.GetSize(int(os.Stdin.Fd()))
		if err != nil {
			if everyN.ShouldLog() {
				log.ErrorLog.Printf("failed to update window size: %v", err)
			}
		} else {
			if err := t.updateWindowSize(cols, rows); err != nil {
				if everyN.ShouldLog() {
					log.ErrorLog.Printf("failed to update window size: %v", err)
				}
			}
		}
	}
	// Do one at the end of the function to set the initial size.
	defer doUpdate()

	// Capture context by value so goroutines don't access t.ctx after
	// Detach() sets it to nil in its deferred cleanup.
	ctx := t.ctx

	// These goroutines are NOT added to t.wg. They exit via context
	// cancellation independently and do not block Detach()'s wg.Wait().
	// Previously t.wg.Add(3) here meant wg.Wait() had to wait for any
	// in-progress doUpdate() (a tmux resize-window subprocess) to finish
	// before Detach() could return — adding up to ~50ms of stall on top of
	// the normal teardown path. The goroutines are safe to run past Detach()
	// because doUpdate() only calls tmux resize-window (no stdout writes).
	debouncedWinch := make(chan os.Signal, 1)

	// Cleanup goroutine: stops SIGWINCH delivery once the context is done.
	go func() {
		<-ctx.Done()
		signal.Stop(winchChan)
	}()

	// Debounce goroutine: coalesces rapid SIGWINCH events with a 50ms quiet window.
	go func() {
		var resizeTimer *time.Timer
		for {
			select {
			case <-ctx.Done():
				return
			case <-winchChan:
				if resizeTimer != nil {
					resizeTimer.Stop()
				}
				resizeTimer = time.AfterFunc(50*time.Millisecond, func() {
					select {
					case debouncedWinch <- syscall.SIGWINCH:
					case <-ctx.Done():
					}
				})
			}
		}
	}()

	// Resize handler goroutine: applies the actual resize on each debounced event.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-debouncedWinch:
				doUpdate()
			}
		}
	}()

	// Periodic poll goroutine: reasserts window size every 2s. Another cs
	// process may call resize-window for its preview pane, shrinking this
	// attached session. SIGWINCH is not delivered for tmux-initiated resizes,
	// so polling is the only recovery mechanism.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				doUpdate()
			}
		}
	}()
}
