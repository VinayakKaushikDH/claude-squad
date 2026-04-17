package notify

import (
	"os/exec"
	"runtime"
)

// Send dispatches an OS notification. Best-effort, non-blocking.
// On macOS uses osascript, on Linux uses notify-send.
func Send(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("osascript", "-e",
			`display notification "`+escapeAppleScript(body)+`" with title "`+escapeAppleScript(title)+`"`).Run()
	case "linux":
		path, err := exec.LookPath("notify-send")
		if err != nil {
			return nil // notify-send not installed, silently skip
		}
		return exec.Command(path, title, body).Run()
	default:
		return nil // unsupported platform, silently skip
	}
}

// escapeAppleScript escapes double quotes and backslashes for AppleScript strings.
func escapeAppleScript(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}
