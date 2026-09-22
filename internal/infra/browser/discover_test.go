package browser

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestFinderPrefersPathLookup(t *testing.T) {
	f := finder{
		goos: "linux",
		lookPath: func(name string) (string, error) {
			if name == "google-chrome" {
				return "/usr/local/bin/google-chrome", nil
			}
			return "", exec.ErrNotFound
		},
		isFile: func(string) bool { return false },
	}
	got, err := f.find()
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got != "/usr/local/bin/google-chrome" {
		t.Errorf("find = %q, want the PATH result", got)
	}
}

func TestFinderFallsBackToKnownPaths(t *testing.T) {
	f := finder{
		goos:     "darwin",
		lookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		isFile: func(p string) bool {
			return p == "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
		},
	}
	got, err := f.find()
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !strings.Contains(got, "Microsoft Edge") {
		t.Errorf("find = %q, want the Edge bundle", got)
	}
}

func TestFinderErrorWhenNoBrowser(t *testing.T) {
	f := finder{
		goos:     "windows",
		lookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		isFile:   func(string) bool { return false },
	}
	_, err := f.find()
	if !errors.Is(err, ErrNoBrowser) {
		t.Fatalf("find error = %v, want ErrNoBrowser", err)
	}
	// The message names what was searched so a user can act on it.
	if !strings.Contains(err.Error(), "chrome") {
		t.Errorf("error should list candidates: %v", err)
	}
}

func TestConsoleRingResetScopesToCurrentPage(t *testing.T) {
	r := &consoleRing{}
	r.add("console.error: page one")
	if len(r.snapshot()) != 1 {
		t.Fatalf("ring should hold the first page's error")
	}
	r.reset() // navigation to a new page
	if got := r.snapshot(); len(got) != 0 {
		t.Errorf("after reset ring = %v, want empty so console reflects the new page", got)
	}
	r.add("console.error: page two")
	if got := r.snapshot(); len(got) != 1 || got[0] != "console.error: page two" {
		t.Errorf("ring = %v, want only the current page's error", got)
	}
}

func TestBrowserCandidatesCoverMajorPlatforms(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		names, paths := browserCandidates(goos)
		if len(names) == 0 || len(paths) == 0 {
			t.Errorf("%s: names=%d paths=%d, want both non-empty", goos, len(names), len(paths))
		}
	}
}
