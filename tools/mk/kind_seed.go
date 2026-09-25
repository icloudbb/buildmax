package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// `kind seed` puts a contributor's own models into the local cluster's catalog
// so the CLI and Desktop can exercise the managed transport against real
// inference, instead of needing a hosted deployment to point at.
//
// The cluster's own inference is left alone: conversation.model and the worker
// keep answering from the in-cluster mock, so `kind smoke` stays deterministic
// and costs nothing after a seed.
//
// A seeded row is callable as soon as it exists — every catalog model is
// available to every user, and a client names one by the row's own name — so
// this touches no configuration and needs no server restart.
//
// Rows are added through `buildmax-server model add` rather than by writing the
// table. The command already owns public ID generation, the default capability
// set, and field validation; a second copy of those in mk would be a copy that
// nothing keeps in step with the schema.

const (
	// kindHostAddress is how a pod reaches a daemon on this machine. Docker
	// Desktop resolves it inside the cluster, which is what makes a local Ollama
	// usable from the deployment; on Linux the equivalent is the bridge gateway,
	// so a rewrite says what it did rather than doing it silently.
	kindHostAddress = "host.docker.internal"
)

// addedModelPattern reads the ID back out of `model add`. Nothing else prints
// it, so a change to that line is a change to this.
var addedModelPattern = regexp.MustCompile(`(?m)^Added model (\S+) \((.*)\)$`)

// publicIDPattern is the text form of a public ID: 20 lowercase base32
// characters. Catalog output is parsed by column, and this is what tells a data
// row from a log line that landed in the same stream.
var publicIDPattern = regexp.MustCompile(`^[a-z2-7]{20}$`)

// kindSeedEntry is one local settings model as the cluster will hold it.
type kindSeedEntry struct {
	// name is the catalog row's operator-facing name, unique in the deployment.
	// It is what a managed client selects after login.
	name string
	// id is the catalog ID the row was created with.
	id string
	// added is a new row; refreshed is an existing one whose key was replaced.
	added, refreshed bool
}

func kindSeed() error {
	if err := requireCommands("kubectl"); err != nil {
		return err
	}
	cluster := kindClusterName()
	exists, err := kindClusterExists(cluster)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("kind cluster %q does not exist; run %s kind up", cluster, mk())
	}

	configured, err := readLocalModels()
	if err != nil {
		return err
	}
	direct := directSettingsModels(configured)
	if len(direct) == 0 {
		return fmt.Errorf("%s configures no provider model to seed; a managed entry names a catalog model, which is what this command creates", localSettingsPath)
	}

	target := kindSmokeTarget()
	existing, err := kindCatalogIDs(target)
	if err != nil {
		return err
	}

	entries, err := seedKindCatalog(direct, existing)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("no model could be seeded")
	}

	return printKindSeedUsage(entries)
}

// directSettingsModels drops obsolete managed entries that already call a
// gateway. Such an entry names a catalog row rather than an upstream, so there
// is nothing in it for a catalog to hold.
func directSettingsModels(models []settingsModel) []settingsModel {
	out := make([]settingsModel, 0, len(models))
	for _, m := range models {
		if m.isManaged() {
			fmt.Printf("Skipping %s: it is already a managed entry.\n", m.id)
			continue
		}
		out = append(out, m)
	}
	return out
}

// seedKindCatalog adds every model that is not already there, and refreshes the
// key of every one that is.
//
// A name already in the catalog keeps its row and ID, because that name is what
// signed-in clients select; its key is replaced from settings.yaml and the
// output says so. Without that, a seed run with a wrong key could never be
// corrected short of rebuilding the cluster.
func seedKindCatalog(models []settingsModel, existing map[string]string) ([]kindSeedEntry, error) {
	entries := make([]kindSeedEntry, 0, len(models))
	claimed := make(map[string]string, len(models))
	for _, m := range models {
		name := m.name
		if name == "" {
			name = m.id
		}
		// The catalog's name column is unique, and a name is how a client
		// addresses a model, so two local entries sharing a display name would
		// make the second unreachable rather than merely duplicated.
		if other, taken := claimed[name]; taken {
			fmt.Printf("Skipping %q: %q already claims that name.\n", m.id, other)
			continue
		}
		// A copied example file still holds a placeholder; seeding it would put
		// a model in the catalog whose every call fails upstream.
		if placeholderAPIKey(m.apiKey) {
			fmt.Printf("Skipping %s: its api_key in %s is still the example placeholder.\n", name, localSettingsPath)
			continue
		}

		entry := kindSeedEntry{name: name}
		if id, known := existing[name]; known {
			entry.id = id
			if m.apiKey != "" {
				if _, err := kindServerCommand(m.apiKey+"\n", "model", "set-key", "--id", id); err != nil {
					return nil, fmt.Errorf("refresh the key of %q: %w", name, err)
				}
				entry.refreshed = true
				fmt.Printf("  %s is already in the catalog as %s; its key was refreshed from %s\n", name, id, localSettingsPath)
			} else {
				fmt.Printf("  %s is already in the catalog as %s\n", name, id)
			}
		} else {
			added, err := addKindCatalogModel(m, name)
			if err != nil {
				return nil, err
			}
			entry.id, entry.added = added, true
			fmt.Printf("  %s added as %s\n", name, added)
		}
		claimed[name] = m.id
		entries = append(entries, entry)
	}
	return entries, nil
}

// placeholderAPIKey reports a key an example or starter file wrote rather than
// a person.
func placeholderAPIKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(k, "your_api_key") ||
		(strings.HasPrefix(k, "your-") && strings.HasSuffix(k, "-key"))
}

func addKindCatalogModel(m settingsModel, name string) (string, error) {
	input := ""
	if m.apiKey != "" {
		input = m.apiKey + "\n"
	}
	output, err := kindServerCommand(input, kindCatalogModelArgs(m, name)...)
	if err != nil {
		return "", fmt.Errorf("add %q to the catalog: %w", name, err)
	}
	match := addedModelPattern.FindStringSubmatch(output)
	if match == nil {
		return "", fmt.Errorf("add %q to the catalog: the command printed no model ID: %s", name, output)
	}
	return match[1], nil
}

// kindServerCommand runs buildmax-server in the cluster with input on its
// standard input. Keys travel that way rather than as arguments, which the
// process listing inside the pod and on this machine would both show. A
// variable so a test can stand in for the cluster.
var kindServerCommand = func(input string, args ...string) (string, error) {
	cmdArgs := append([]string{"--context", kindContext(), "exec", "-i", "-n", "buildmax", "deployment/buildmax-server", "--", "buildmax-server"}, args...)
	cmd := exec.Command("kubectl", cmdArgs...)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("buildmax-server %s: %w: %s", strings.Join(args[:min(2, len(args))], " "), err, text)
	}
	return text, nil
}

// kindCatalogModelArgs is the `model add` command line for one settings entry.
// It is separate from running it so a test can hold the flags against the ones
// the command actually defines: an entry field that stops reaching the catalog
// is silent, and a flag that no longer exists fails the whole seed.
func kindCatalogModelArgs(m settingsModel, name string) []string {
	args := []string{"model", "add", "--name", name, "--model", m.id, "--api-url", kindReachableURL(m.apiURL, name)}
	// An empty flag is not the same as an absent one here: the add command
	// defaults --provider to openai_compatible and rejects an empty value, so an
	// entry that names no protocol must not pass the flag at all.
	if m.provider != "" {
		args = append(args, "--provider", m.provider)
	}
	if m.apiKey != "" {
		// The key itself goes on standard input; see kindServerCommand.
		args = append(args, "--api-key", "-")
	}
	if m.contextWindow > 0 {
		args = append(args, "--context-window", strconv.Itoa(m.contextWindow))
	}
	if m.callTimeout > 0 {
		args = append(args, "--call-timeout", strconv.Itoa(m.callTimeout))
	}
	if m.maxTokens > 0 {
		args = append(args, "--max-tokens", strconv.Itoa(m.maxTokens))
	}
	if m.reasoning != "" {
		args = append(args, "--reasoning", m.reasoning)
	}
	if m.cacheMode != "" {
		args = append(args, "--cache-mode", m.cacheMode)
	}
	if m.cacheTTL != "" {
		args = append(args, "--cache-ttl", m.cacheTTL)
	}
	// Prices are forwarded whole or not at all. The add command validates them
	// as a set — rates without a currency price nothing — so half a price list
	// would be rejected rather than partially applied.
	if m.pricing.currency != "" {
		args = append(args, "--currency", m.pricing.currency)
		for _, p := range []struct{ flag, price string }{
			{"--input-price", m.pricing.inputPerMTok},
			{"--cache-read-price", m.pricing.cacheReadPerMTok},
			{"--cache-write-price", m.pricing.cacheWritePerMTok},
			{"--output-price", m.pricing.outputPerMTok},
		} {
			if p.price != "" {
				args = append(args, p.flag, p.price)
			}
		}
	}
	if m.vision {
		args = append(args, "--vision")
	}
	return args
}

// kindReachableURL rewrites an address that means "this machine" into one a pod
// can reach. A local runtime is the whole point of seeding for some
// contributors, and its local settings entry necessarily points at
// loopback, which inside a pod is the pod itself.
func kindReachableURL(rawURL, name string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return rawURL
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
	default:
		return rawURL
	}
	host := kindHostAddress
	if port := parsed.Port(); port != "" {
		host = host + ":" + port
	}
	parsed.Host = host
	rewritten := parsed.String()
	fmt.Printf("  %s points at this machine; the cluster will use %s\n", name, rewritten)
	fmt.Printf("    On Linux that name does not resolve — use the bridge gateway from `docker network inspect kind`\n" +
		"    and start the daemon with OLLAMA_HOST=0.0.0.0.\n")
	return rewritten
}

// kindCatalogIDs reads the catalog as a name-to-ID map.
//
// `model list` writes an aligned table, so the columns are found from the
// header rather than by splitting on whitespace: a model name may contain
// spaces. Anything that is not a data row — a log line sharing the stream, the
// empty-catalog notice — is skipped rather than guessed at.
func kindCatalogIDs(target smokeTarget) (map[string]string, error) {
	output, err := target.admin("model", "list")
	if err != nil {
		return nil, fmt.Errorf("read the catalog: %w", err)
	}
	return parseCatalogIDs(output), nil
}

func parseCatalogIDs(output string) map[string]string {
	ids := make(map[string]string)
	nameStart, nameEnd := -1, -1
	for _, raw := range strings.Split(output, "\n") {
		line := []rune(strings.TrimRight(raw, " \r"))
		if nameStart < 0 {
			nameStart, nameEnd = catalogNameColumn(string(line))
			continue
		}
		if len(line) <= nameStart {
			continue
		}
		id := strings.TrimSpace(string(line[:min(nameStart, len(line))]))
		if !publicIDPattern.MatchString(id) {
			continue
		}
		name := strings.TrimSpace(string(line[nameStart:min(nameEnd, len(line))]))
		if name == "" {
			continue
		}
		ids[name] = id
	}
	return ids
}

// catalogNameColumn locates the NAME column in the table header, returning
// (-1, -1) for any other line.
func catalogNameColumn(line string) (int, int) {
	if !strings.HasPrefix(line, "ID") || !strings.Contains(line, "ENABLED") {
		return -1, -1
	}
	start := strings.Index(line, "NAME")
	provider := strings.Index(line, "PROVIDER")
	if start < 0 || provider < 0 || provider <= start {
		return -1, -1
	}
	// Index counts bytes and the data rows are sliced as runes, so both ends are
	// measured the same way. The header is ASCII, but a name above it need not be.
	return len([]rune(line[:start])), len([]rune(line[:provider]))
}

// printKindSeedUsage explains how a managed client sees the new catalog rows.
// Login decides the mode; managed model entries do not belong in settings.yaml.
func printKindSeedUsage(entries []kindSeedEntry) error {
	target := kindSmokeTarget()
	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()
	if err := waitForHTTP(ctx, client, target.apiBase+"/healthz", 90*time.Second); err != nil {
		return err
	}

	added, refreshed := 0, 0
	for _, e := range entries {
		if e.added {
			added++
		}
		if e.refreshed {
			refreshed++
		}
	}
	fmt.Printf("\nCluster %s offers %d seeded model(s): %d added, %d already there with the key refreshed, %d left as they were.\n",
		kindClusterName(), len(entries), added, refreshed, len(entries)-added-refreshed)
	fmt.Printf("The cluster's own inference is untouched: Portal conversations and task runs\n"+
		"still answer from the mock, so `%s kind smoke` stays free and deterministic.\n", mk())
	fmt.Printf("\nSign in to use the deployment catalog from the CLI or Desktop:\n")
	fmt.Printf("  buildmax login   (enter server %s and sign in as %s)\n", target.apiBase, smokeEmail)
	fmt.Printf("Run `%s kind info` for a single-use code, then `buildmax models` to check.\n", mk())
	return nil
}
