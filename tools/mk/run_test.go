package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerDatabaseAddress(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantHost string
		wantPort string
	}{
		{
			name:     "no database block falls back to the server's own defaults",
			body:     "port: 5678\n",
			wantHost: "localhost",
			wantPort: "3306",
		},
		{
			name:     "reads the block and stops at the next top-level key",
			body:     "database:\n  host: 127.0.0.1\n  port: 13306\n  name: buildmax_local\n\nworker:\n  port: 9999\n",
			wantHost: "127.0.0.1",
			wantPort: "13306",
		},
		{
			name:     "quoted values",
			body:     "database:\n  host: \"db.internal\"\n  port: \"3307\"\n",
			wantHost: "db.internal",
			wantPort: "3307",
		},
		{
			name:     "partial block keeps the default for what it omits",
			body:     "database:\n  name: buildmax\n",
			wantHost: "localhost",
			wantPort: "3306",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, port := serverDatabaseAddress(tc.body)
			if host != tc.wantHost || port != tc.wantPort {
				t.Errorf("serverDatabaseAddress() = %s:%s, want %s:%s", host, port, tc.wantHost, tc.wantPort)
			}
		})
	}
}

// A database somewhere else is not something `mk` can start, so it must not
// claim the deployment is broken when it cannot reach it from here.
func TestCheckServerDatabaseIgnoresRemoteHosts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.yaml"), []byte("database:\n  host: db.internal\n  port: 3306\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkServerDatabase(dir); err != nil {
		t.Errorf("checkServerDatabase() = %v, want nil for a remote database", err)
	}
}

// The case a contributor actually hits: a local database in server.yaml that
// nothing is listening for. The message has to name the two commands that start
// it, because the server's own dial error names neither.
func TestCheckServerDatabaseNamesTheCommandsThatStartIt(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	config := fmt.Sprintf("database:\n  host: 127.0.0.1\n  port: %d\n", port)
	if err := os.WriteFile(filepath.Join(dir, "server.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	err = checkServerDatabase(dir)
	if err == nil {
		t.Fatal("checkServerDatabase() = nil, want an error naming how to start the database")
	}
	for _, want := range []string{"kind up", "kind forward", fmt.Sprint(port)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestCheckServerDatabaseWithoutConfig(t *testing.T) {
	if err := checkServerDatabase(t.TempDir()); err != nil {
		t.Errorf("checkServerDatabase() = %v, want nil when there is no server.yaml", err)
	}
}

// `mk run cli -- <args>` documents "--" as the boundary between mk's own
// arguments and the CLI's. mk is not cobra and gives "--" no meaning, so it must
// strip a single leading separator before forwarding, or the literal token
// reaches the binary and cobra starts the TUI instead of running the subcommand.
func TestStripArgSeparator(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"leading separator dropped", []string{"--", "init"}, []string{"init"}},
		{"no separator untouched", []string{"init"}, []string{"init"}},
		{"only the first separator dropped", []string{"--", "--", "help"}, []string{"--", "help"}},
		{"empty stays empty", nil, nil},
		{"separator inside is kept", []string{"info", "--", "x"}, []string{"info", "--", "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripArgSeparator(tc.in)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("stripArgSeparator(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
