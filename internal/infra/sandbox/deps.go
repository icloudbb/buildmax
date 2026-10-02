package sandbox

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// DepCheck describes one host-side dependency of the sandbox backend.
type DepCheck struct {
	// Name is the binary/feature being checked (e.g. "bwrap", "socat",
	// "sandbox-exec").
	Name string
	// Required reports whether the absence of this dep blocks the
	// sandbox from running at all. Optional deps are nice-to-have
	// (e.g. seccomp filter on Linux).
	Required bool
	// OK reports whether the dep is present and usable on this host.
	OK bool
	// Path is the resolved binary path when OK is true.
	Path string
	// Hint is a one-line install suggestion when OK is false.
	Hint string
	// Err carries the lookup error when OK is false (typically
	// exec.ErrNotFound).
	Err error
}

// DepsReport summarises the dependency check for a host.
type DepsReport struct {
	Platform string     // "linux", "wsl", "darwin", "windows"
	Backend  string     // "bwrap" | "seatbelt" | "none"
	Checks   []DepCheck // ordered list, required deps first
}

// AllRequiredOK reports whether every Required check passed.
func (r DepsReport) AllRequiredOK() bool {
	for _, c := range r.Checks {
		if c.Required && !c.OK {
			return false
		}
	}
	return true
}

// has reports whether the named dependency was found.
func (r DepsReport) has(name string) bool {
	for _, c := range r.Checks {
		if c.Name == name {
			return c.OK
		}
	}
	return false
}

// FirstMissingRequired returns the first Required dep that failed, or
// the zero value when AllRequiredOK is true.
func (r DepsReport) FirstMissingRequired() DepCheck {
	for _, c := range r.Checks {
		if c.Required && !c.OK {
			return c
		}
	}
	return DepCheck{}
}

// CheckDeps performs the platform-appropriate dependency check. Pure
// function: no global state, callers cache the result themselves.
func CheckDeps() DepsReport {
	switch runtime.GOOS {
	case "darwin":
		return checkDarwinDeps()
	case "linux":
		return checkLinuxDeps()
	default:
		return DepsReport{
			Platform: runtime.GOOS,
			Backend:  "none",
			Checks: []DepCheck{{
				Name:     "platform",
				Required: true,
				OK:       false,
				Hint:     fmt.Sprintf("sandbox is not supported on %s; supported platforms: linux, darwin, wsl2", runtime.GOOS),
				Err:      errors.New("unsupported platform"),
			}},
		}
	}
}

func checkDarwinDeps() DepsReport {
	rep := DepsReport{Platform: "darwin", Backend: "seatbelt"}
	rep.Checks = append(rep.Checks, lookup("sandbox-exec", true,
		"sandbox-exec ships with macOS; missing typically means a stripped CLT install"))
	return rep
}

func checkLinuxDeps() DepsReport {
	rep := DepsReport{Platform: "linux", Backend: "bwrap"}
	rep.Checks = append(rep.Checks,
		lookup("bwrap", true,
			"apt: sudo apt-get install bubblewrap · fedora: sudo dnf install bubblewrap"),
		lookup("socat", false,
			"apt: sudo apt-get install socat · fedora: sudo dnf install socat"),
	)
	return rep
}

// lookup wraps exec.LookPath into a DepCheck.
func lookup(name string, required bool, hint string) DepCheck {
	path, err := exec.LookPath(name)
	if err != nil {
		return DepCheck{Name: name, Required: required, OK: false, Hint: hint, Err: err}
	}
	return DepCheck{Name: name, Required: required, OK: true, Path: path}
}
