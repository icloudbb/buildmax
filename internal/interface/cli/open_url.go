package cli

import (
	"os/exec"
	"runtime"
)

// openExternalURL opens url in the system browser. It is a variable so tests
// can stand in for the browser; nothing under test may open a real one.
// Callers always print the URL as well, because no opener may exist.
var openExternalURL = func(url string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{url}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		command, args = "xdg-open", []string{url}
	}
	return exec.Command(command, args...).Start()
}

// browserOnThisScreen reports whether a browser opened from this process would
// appear in front of the person running it. Over SSH it would open on the
// remote host, if anywhere; a Linux or BSD host with no display server has no
// screen to open it on. getenv is os.Getenv outside tests.
func browserOnThisScreen(getenv func(string) string, goos string) bool {
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return false
	}
	switch goos {
	case "darwin", "windows":
		return true
	default:
		return getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != ""
	}
}
