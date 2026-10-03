package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// cmdInstall copies the built binaries into ~/.local/bin. The CLI is required;
// the others are copied when present, so `build cli` followed by `install` is a
// valid shortcut.
func cmdInstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	localBin := filepath.Join(home, ".local", "bin")

	cliSrc := filepath.Join(binDir, exe(cliBinary))
	if !exists(cliSrc) {
		return fmt.Errorf("%s not found. Run %s build first", cliSrc, mk())
	}
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		return err
	}
	fmt.Printf("Installing BuildMax binaries to %s...\n", localBin)
	for _, binary := range []string{cliBinary, serverBinary, workerBinary, desktopBinary} {
		src := filepath.Join(binDir, exe(binary))
		if !exists(src) {
			fmt.Printf("Note: %s not found, skip. Run %s build to build it.\n", exe(binary), mk())
			continue
		}
		dst := filepath.Join(localBin, exe(binary))
		fmt.Printf("Copying %s to %s\n", exe(binary), dst)
		if err := copyFile(src, dst, 0o755); err != nil {
			return err
		}
	}

	fmt.Println()
	if onPath(localBin) {
		fmt.Printf("%s is already in your PATH.\n", localBin)
	} else {
		printPathHint(localBin)
	}
	fmt.Println()
	fmt.Println("Installation complete!")
	return nil
}

func onPath(dir string) bool {
	want := filepath.Clean(dir)
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == "" {
			continue
		}
		got := filepath.Clean(entry)
		if runtime.GOOS == "windows" {
			if strings.EqualFold(got, want) {
				return true
			}
			continue
		}
		if got == want {
			return true
		}
	}
	return false
}

func printPathHint(dir string) {
	fmt.Printf("%s is not in your PATH.\n", dir)
	fmt.Println("To run the binaries from any directory, add it:")
	if runtime.GOOS == "windows" {
		fmt.Printf("  setx PATH \"%%PATH%%;%s\"\n", dir)
		fmt.Println()
		fmt.Println("Then open a new terminal for the change to take effect.")
		return
	}
	fmt.Printf("  export PATH=\"%s:$PATH\"\n", dir)
	fmt.Println()
	fmt.Println("To make it permanent, add that line to your shell config:")
	fmt.Printf("  echo 'export PATH=\"%s:$PATH\"' >> ~/.zshrc   # or ~/.bashrc\n", dir)
	fmt.Println()
	fmt.Println("Then start a new terminal session, or source the file you edited.")
}

// kindImage is one image ./make kind builds locally and loads into the kind
// cluster, which has no registry to pull from.
type kindImage struct {
	tag        string
	dockerfile string
}

var (
	kindImageServer = kindImage{"buildmax:local", filepath.Join("deployment", "docker", "Dockerfile.buildmax")}
	kindImagePortal = kindImage{"buildmax-portal:local", filepath.Join("deployment", "docker", "Dockerfile.portal")}
	kindImageSmoke  = kindImage{"buildmax-smoke-llm:local", "deployment/smoke/mock-llm/Dockerfile"}

	kindImageSmokeOIDC = kindImage{"buildmax-smoke-oidc:local", "deployment/smoke/mock-oidc/Dockerfile"}
)

// kindReloadService maps a `kind reload <service>` name to the deployment it
// restarts and the image that deployment runs.
type kindReloadService struct {
	name       string
	deployment string
	image      kindImage
}

// kindReloadServices only lists the two services this cluster runs as
// Deployments; the worker still runs as an ad-hoc Job, so there is no
// deployment for `reload` to restart.
var kindReloadServices = []kindReloadService{
	{"server", "buildmax-server", kindImageServer},
	{"portal", "buildmax-portal", kindImagePortal},
}

func kindReloadServiceNames() []string {
	names := make([]string, len(kindReloadServices))
	for i, svc := range kindReloadServices {
		names[i] = svc.name
	}
	return names
}

// cmdKindReload builds and loads the image for one service, or both when none
// is named, then restarts the matching deployment so a code change takes
// effect. Loading an image under the fixed :local tag does not by itself
// replace a running pod, so without the restart the new code would sit on the
// node unused; that gap made a bare image load the wrong default for the
// local development loop.
func cmdKindReload(args []string) error {
	if len(args) > 1 {
		return usageErrorf("kind", "reload takes at most one service name (%s)", strings.Join(kindReloadServiceNames(), " or "))
	}
	services := kindReloadServices
	if len(args) == 1 {
		found := false
		for _, svc := range kindReloadServices {
			if svc.name == args[0] {
				services = []kindReloadService{svc}
				found = true
				break
			}
		}
		if !found {
			return usageErrorf("kind", "unknown service %q for reload; want %s", args[0], strings.Join(kindReloadServiceNames(), " or "))
		}
	}

	cluster := kindClusterName()
	images := make([]kindImage, len(services))
	for i, svc := range services {
		images[i] = svc.image
	}
	if err := buildAndLoadKindImages(cluster, images); err != nil {
		return err
	}
	// A restart on a deployment that does not exist yet is an error, not a
	// no-op, so skip the missing ones: a cluster that is up but not yet
	// deployed (a manual non-smoke apply is still pending) will pick up the
	// loaded image when its pods are first created, and needs no restart.
	for _, svc := range services {
		if !succeeds("kubectl", "--context", kindContext(), "get", "deployment/"+svc.deployment, "-n", "buildmax") {
			fmt.Printf("Deployment %s not found, skipping restart.\n", svc.deployment)
			continue
		}
		fmt.Printf("Restarting deployment %s...\n", svc.deployment)
		if err := kindKubectl("rollout", "restart", "deployment/"+svc.deployment, "-n", "buildmax"); err != nil {
			return err
		}
		if err := kindKubectl("rollout", "status", "deployment/"+svc.deployment, "-n", "buildmax", "--timeout=180s"); err != nil {
			return err
		}
		// Re-stamp the source: this rollout is exactly what makes the deployment
		// match the tree again, which is what `./make e2e kind` checks for.
		if err := stampKindDeploymentIdentity(svc.deployment); err != nil {
			return err
		}
	}
	return nil
}

func buildAndLoadKindImages(cluster string, images []kindImage) error {
	var platform []string
	if p := os.Getenv("BUILDMAX_IMAGE_PLATFORM"); p != "" {
		platform = []string{"--platform", p}
	}

	for _, image := range images {
		fmt.Printf("Building image %s...\n", image.tag)
		args := append([]string{"build", "-f", image.dockerfile}, platform...)
		args = append(args, "-t", image.tag, ".")
		if err := runCmd("docker", args...); err != nil {
			return fmt.Errorf("docker build of %s failed: %w", image.tag, err)
		}
	}
	fmt.Printf("Loading images into kind cluster %q...\n", cluster)
	for _, image := range images {
		if err := runKind("load", "docker-image", image.tag, "--name", cluster); err != nil {
			return fmt.Errorf("kind load of %s failed: %w", image.tag, err)
		}
	}
	fmt.Println("Kind images are loaded.")
	return nil
}
