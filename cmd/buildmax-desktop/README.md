# BuildMax Desktop App

Native desktop application for BuildMax (Wails + Go). Provides a local, first-hand agent experience similar to the CLI, without requiring a backend server.

## Prerequisites

- **Go** — the version in `go.mod`
- **Node 24** and **npm 11** — pinned by `.node-version` and `package.json`
- **Wails CLI** is needed only for direct development commands. Install the
  same version used by `go.mod`:

  ```bash
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
  ```

  Ensure `$HOME/go/bin` (or `$GOPATH/bin`) is in your `PATH`.

- **macOS**: Xcode Command Line Tools (or full Xcode) for building the native app.

## Build

**Production build** (strictly builds the shared GUI, Portal, Desktop frontend,
and native app with the pinned Wails version):

From the **repo root**:

```bash
./make build
```

Or from this directory, after building `gui`:

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build -tags desktop
```

Output: `build/bin/buildmax-desktop` (or platform-specific path under `build/`).

To check the React frontend only: `./make check desktop` from the repository
root. The manual equivalent is `cd desktop/frontend && npm ci && npm run lint &&
npm test && npm run build`.

**Note:** `-tags desktop` is required. The frontend bundle in
`desktop/dist/` is embedded only under that tag, so that plain
`go build ./...` keeps working on a checkout where the bundle has not been built
yet. `wails build -tags desktop` produces the bundle first, and `./make build`
does the same. A binary built without the tag refuses to start and prints how to
rebuild it — except during Wails' binding generation, which strips the `desktop`
tag by design; see [bindings_on.go](bindings_on.go).

## Run

From the **repo root**:

```bash
./make run desktop
```

This starts the already-built binary from `bin/`. Run `./make build` first.

For Wails/Vite hot reload, run from this directory:

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 dev -tags desktop
```

This starts the Vite dev server and opens the app window with hot reload.

## Configuration

The desktop app uses the same application data directory as the CLI:
**`BUILDMAX_HOME`** (default `~/.buildmax`). Local projects, sessions, traces,
and logs use that path today.

## Project layout

- **`cmd/buildmax-desktop/`** — Entrypoint only: `main.go`, `wails.json`.
- **`desktop/`** (repo root) — React + Vite app in `desktop/frontend/` (src/, index.html, package.json). Wails runs `npm run build`, which writes the bundle to `desktop/dist/`; `desktop/assets_embed.go` embeds it under the `desktop` build tag, and `desktop/assets_stub.go` is compiled without the tag and embeds nothing. The bundle sits beside the Go package rather than inside `frontend/` because `frontend/` is a separate Go module and `//go:embed` cannot cross a module boundary.
- **`internal/interface/desktop`** — App logic (App, Run, Startup/Shutdown).
