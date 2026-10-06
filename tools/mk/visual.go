package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// visualSuites are the screenshot suites `e2e visual` runs, in order. Each is
// a Playwright config beside the package it renders; the baselines live under
// that package's visual/__screenshots__/.
var visualSuites = []struct {
	dir    string
	config string
}{
	{"portal", "playwright.visual.config.ts"},
	{filepath.Join("desktop", "frontend"), "playwright.visual.config.js"},
}

// e2eVisual compares Portal's and Desktop's production builds against the
// committed screenshot baselines, and with update rewrites those baselines.
//
// Fonts and antialiasing differ between macOS and Linux, and even between
// Linux distributions, so one renderer has to own the baselines: the official
// Playwright image at the exact version the lockfiles pin. The browsers run
// inside it here and in CI alike, and nowhere else, so a local pass and a CI
// pass are the same claim. The builds and dependencies come from the host; the
// container only needs @playwright/test, which is plain JavaScript.
//
// Neither suite needs a deployment: each serves its build and answers the API
// or the Wails bridge from fixtures, which is what lets CI gate pull requests
// on it.
func e2eVisual(args []string) error {
	update := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--update":
		update = true
	default:
		return usageErrorf("e2e", "visual takes at most `--update`")
	}
	if err := requireCommands("node", "npm", "docker"); err != nil {
		return err
	}
	if !succeeds("docker", "info") {
		return errors.New("docker is installed but not answering: start Docker, then rerun")
	}

	version, err := visualPlaywrightVersion()
	if err != nil {
		return err
	}
	image := "mcr.microsoft.com/playwright:v" + version + "-noble"
	if update {
		fmt.Printf("[e2e] visual suite: rewriting the screenshot baselines in %s\n", image)
	} else {
		fmt.Printf("[e2e] visual suite: production builds against the committed baselines, rendered in %s\n", image)
	}

	// The builds are what get rendered, so they are always rebuilt from this
	// tree: a stale dist/ would compare the wrong code.
	if err := buildGUI(); err != nil {
		return err
	}
	for _, suite := range visualSuites {
		if err := ensureNPMDeps(suite.dir); err != nil {
			return err
		}
		if err := runIn(suite.dir, "npm", "run", "build"); err != nil {
			return fmt.Errorf("build %s: %w", suite.dir, err)
		}
	}

	root, err := os.Getwd()
	if err != nil {
		return err
	}
	artifacts, _, err := prepareArtifacts("visual", "visual", "fixtures, no deployment")
	if err != nil {
		return err
	}
	fmt.Printf("[e2e] artifacts from this run (diffs of any mismatch): %s\n", artifacts)

	var failed []string
	for _, suite := range visualSuites {
		if update {
			// Regenerated from nothing, so a baseline whose test is gone goes too.
			if err := os.RemoveAll(filepath.Join(suite.dir, "visual", "__screenshots__")); err != nil {
				return err
			}
		}
		results := filepath.ToSlash(filepath.Join("/work", artifactDir, "visual", filepath.Base(suite.dir)))
		docker := []string{"run", "--rm", "--ipc=host",
			"-v", root + ":/work",
			"-w", "/work/" + filepath.ToSlash(suite.dir),
			"-e", "BUILDMAX_E2E_ARTIFACTS=" + results,
		}
		if os.Getenv("CI") != "" {
			docker = append(docker, "-e", "CI="+os.Getenv("CI"))
		}
		// On a Linux host the bind mount keeps numeric owners, so the
		// container writes as the caller rather than leaving root-owned
		// baselines behind. Docker Desktop maps ownership itself.
		if runtime.GOOS == "linux" {
			docker = append(docker, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "-e", "HOME=/tmp")
		}
		docker = append(docker, image, "node", "node_modules/@playwright/test/cli.js", "test", "-c", suite.config)
		if update {
			docker = append(docker, "--update-snapshots=all")
		}
		// A busy machine can ask for fewer browsers; the result does not change.
		if workers := os.Getenv("BUILDMAX_E2E_WORKERS"); workers != "" {
			docker = append(docker, "--workers="+workers)
		}
		fmt.Printf("[e2e] %s\n", suite.dir)
		if err := runCmd("docker", docker...); err != nil {
			failed = append(failed, suite.dir)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("visual suite failed in %v: open the diffs under %s, then either fix the change or, if it is intended, run `%s e2e visual --update` and commit the new baselines with it", failed, artifacts, mk())
	}
	if update {
		fmt.Println("[e2e] baselines rewritten: review the changed PNGs in `git diff` before committing them")
	}
	return nil
}

// visualPlaywrightVersion is the @playwright/test version both suites have
// installed. The image tag must match it exactly: a different Chromium is a
// different renderer, and the baselines would drift with no code change.
func visualPlaywrightVersion() (string, error) {
	version := ""
	for _, suite := range visualSuites {
		path := filepath.Join(suite.dir, "node_modules", "@playwright", "test", "package.json")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			if err := ensureNPMDeps(suite.dir); err != nil {
				return "", err
			}
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return "", fmt.Errorf("read the installed Playwright version: %w", err)
		}
		var pkg struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil || pkg.Version == "" {
			return "", fmt.Errorf("no version in %s", path)
		}
		if version != "" && pkg.Version != version {
			return "", fmt.Errorf("portal and desktop/frontend install different @playwright/test versions (%s, %s); the screenshot baselines need one renderer, so align the lockfiles", version, pkg.Version)
		}
		version = pkg.Version
	}
	return version, nil
}
