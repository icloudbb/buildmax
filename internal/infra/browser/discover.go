// Package browser owns the Go-side control of a real, headless browser driven
// over the Chrome DevTools Protocol. It discovers a system Chrome/Edge, launches
// it with an isolated profile, and maps each BuildMax session to its own page,
// implementing the tool.BrowserController port. See
// docs/design/agent-browser-capability.md.
package browser

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// runtimeGOOS and lookPath are indirections over the standard library so the
// finder can be exercised for every platform from any host.
func runtimeGOOS() string                  { return runtime.GOOS }
func lookPath(name string) (string, error) { return exec.LookPath(name) }

// ErrNoBrowser is returned when no supported browser executable can be found.
var ErrNoBrowser = errors.New("no supported browser found")

// browserCandidates returns, per GOOS, the ordered PATH names and absolute
// paths to try, most-preferred first. Chrome/Chromium come before Edge because
// the tools target a Chromium engine and Edge is the fallback.
func browserCandidates(goos string) (names, paths []string) {
	switch goos {
	case "darwin":
		return []string{"google-chrome", "chromium", "microsoft-edge"},
			[]string{
				"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
				"/Applications/Chromium.app/Contents/MacOS/Chromium",
				"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			}
	case "windows":
		return []string{"chrome", "msedge"},
			[]string{
				`C:\Program Files\Google\Chrome\Application\chrome.exe`,
				`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
				`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
				`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			}
	default: // linux and other unix
		return []string{
				"google-chrome", "google-chrome-stable", "chromium",
				"chromium-browser", "microsoft-edge", "microsoft-edge-stable",
			},
			[]string{
				"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable",
				"/usr/bin/chromium", "/usr/bin/chromium-browser",
				"/usr/bin/microsoft-edge", "/snap/bin/chromium",
			}
	}
}

// finder locates a browser executable. The lookup functions are injected so the
// search is testable without a browser installed on the host.
type finder struct {
	goos     string
	lookPath func(string) (string, error) // resolves a PATH name to a full path
	isFile   func(string) bool            // reports whether an absolute path is an existing file
}

func (f finder) find() (string, error) {
	names, paths := browserCandidates(f.goos)
	for _, name := range names {
		if p, err := f.lookPath(name); err == nil && p != "" {
			return p, nil
		}
	}
	for _, p := range paths {
		if f.isFile(p) {
			return p, nil
		}
	}
	searched := append(append([]string{}, names...), paths...)
	return "", fmt.Errorf(
		"%w: install Google Chrome, Chromium, or Microsoft Edge; searched %s",
		ErrNoBrowser, strings.Join(searched, ", "),
	)
}

// isRegularFile reports whether path exists and is a regular file (not a dir).
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
