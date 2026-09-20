package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coreconnect "github.com/icloudbb/buildmax/internal/core/appconnect"
	"github.com/icloudbb/buildmax/internal/interface/appconnect"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/term"
)

func connectorFor(name string) (coreconnect.Manifest, error) {
	d := config.DiscoverPlugins()
	p, ok := findPlugin(d, name)
	if !ok || !p.Loadable() {
		return coreconnect.Manifest{}, fmt.Errorf("enabled plugin %q not found", name)
	}
	return appconnect.Load(p.Path)
}

func newConnectCommand() *cobra.Command {
	c := &cobra.Command{
		Use: "connect <plugin>", Short: "Authorize one installed app connector",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := connectorFor(args[0])
			if err != nil {
				return err
			}
			return connectOAuth(cmd.Context(), cmd.OutOrStdout(), args[0], m)
		},
	}
	c.AddCommand(newConnectMCPCommand())
	return c
}

func newAppCommand() *cobra.Command {
	c := &cobra.Command{Use: "app <plugin> <operation>", Short: "Call a declared app operation", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := connectorFor(args[0])
			if err != nil {
				return err
			}
			op, ok := m.Operations[args[1]]
			if !ok {
				return fmt.Errorf("operation %q is not declared", args[1])
			}
			body, _ := cmd.Flags().GetString("json")
			approved := false
			if op.Effect == "write" {
				if len(body) > 1<<20 || !json.Valid([]byte(body)) {
					return errors.New("write body must be valid JSON no larger than 1 MiB")
				}
				approved, err = confirmAppWrite(cmd.ErrOrStderr(), args[0], args[1], body)
				if err != nil {
					return err
				}
			}
			result, err := appconnect.Call(cmd.Context(), args[0], m, args[1], []byte(body), approved)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(result))
			return err
		},
	}
	c.Flags().String("json", "", "JSON body for a declared write operation")
	return c
}

func confirmAppWrite(w io.Writer, plugin, operation, body string) (bool, error) {
	// Agent tool calls do not have an interactive terminal. A model-supplied
	// --yes flag or piped stdin must not turn this prototype into silent writes.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, errors.New("write requires an interactive terminal")
	}
	want := plugin + "/" + operation
	fmt.Fprintf(w, "Write to %s with this JSON body:\n%s\nType %s to confirm: ", plugin, body, want)
	var got string
	if _, err := fmt.Fscanln(os.Stdin, &got); err != nil {
		return false, err
	}
	if got != want {
		return false, errors.New("write cancelled")
	}
	return true, nil
}

func connectOAuth(parent context.Context, w io.Writer, name string, m coreconnect.Manifest) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	redirect := "http://" + listener.Addr().String() + "/callback"
	cfg, err := appconnect.OAuthConfig(m, redirect)
	if err != nil {
		return err
	}
	state, err := randomURLToken()
	if err != nil {
		return err
	}
	verifier := oauth2.GenerateVerifier()
	type callbackResult struct {
		code string
		err  error
	}
	callbackCh := make(chan callbackResult, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/callback" || req.URL.Query().Get("state") != state {
			http.Error(rw, "OAuth response rejected", http.StatusBadRequest)
			return
		}
		result := callbackResult{code: req.URL.Query().Get("code")}
		if req.URL.Query().Get("error") != "" {
			result.err = errors.New("OAuth provider denied authorization")
		}
		if result.code == "" && result.err == nil {
			result.err = errors.New("OAuth callback contained no authorization code")
		}
		select {
		case callbackCh <- result:
			if result.err != nil {
				http.Error(rw, result.err.Error(), http.StatusBadRequest)
			} else {
				fmt.Fprintln(rw, "BuildMax connection received. You can close this tab.")
			}
		default:
			http.Error(rw, "Already received", http.StatusConflict)
		}
	})
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- server.Serve(listener) }()
	defer func() { _ = server.Shutdown(context.Background()) }()
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.AccessTypeOffline)
	fmt.Fprintln(w, "Open this URL to authorize the connection:", authURL)
	_ = openExternalURL(authURL) // Printed URL remains usable when no browser opener exists.
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	var callback callbackResult
	select {
	case callback = <-callbackCh:
	case err := <-serveErrCh:
		return fmt.Errorf("OAuth callback listener stopped: %w", err)
	case <-ctx.Done():
		return fmt.Errorf("OAuth callback: %w", ctx.Err())
	}
	if callback.err != nil {
		return callback.err
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	token, err := cfg.Exchange(context.WithValue(ctx, oauth2.HTTPClient, client), callback.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("OAuth token exchange: %w", err)
	}
	if token.RefreshToken == "" {
		return errors.New("OAuth provider did not issue a refresh token; reconnect with offline access enabled")
	}
	if err := appconnect.SaveToken(name, token); err != nil {
		return fmt.Errorf("store connection: %w", err)
	}
	fmt.Fprintf(w, "%s connected.\n", name)
	return nil
}

func randomURLToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

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
