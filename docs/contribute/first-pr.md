# Your First Pull Request

> **简体中文：** [阅读中文镜像](../zh-CN/contribute/first-pr.md)
> **Audience:** new contributors · **Status:** current

The shortest complete path from a clone to an open pull request. It needs Go and
git, and **does not need a model API key**. The first build downloads roughly
700 MB of Go dependencies; after that every command here finishes in under a
minute, including the full test suite and the pre-pull-request gate.

## 1. Clone And Build

```bash
git clone https://github.com/icloudbb/buildmax.git
cd buildmax
./make doctor
./make setup local
./make build cli
```

On Windows, use `make.bat build cli`. `./make` is not GNU make — it is a
one-line shim around the Go task runner in `tools/mk`, so every platform runs the
same task code. `./make help` lists every command, from the daily ones to the
deployment and release tasks, and `./make help <command>` explains one command
in full.

`./make setup local` creates `.local/`, the one gitignored directory holding
configuration that is yours rather than the repository's, and prints what is
left to fill in. Nothing on this page needs any of it filled in — a first
change needs no credentials — but running it once now is why `./make doctor`
stops mentioning it, and it is where a model API key goes when you do want one.

The binary lands in `bin/buildmax`. `./make build cli` skips the server, worker,
and frontends, which is all you need for a first change.

## 2. Run The Tests

```bash
./make test
```

This runs `go test ./...` with `BUILDMAX_HOME=./testing-sandbox`, so it writes
to a gitignored directory in the repo instead of your real `~/.buildmax`. A
clean run here means your toolchain is set up correctly.

If you plan to touch Go code, run the Go gate before you open the pull request:

```bash
./make check go
```

That is one command for every Go step CI runs: formatting, `go mod tidy`
cleanliness, build, vet, the race suite, and lint. It reports unformatted files
rather than fixing them — `./make fmt` is the fix. Running `./make test` alone
leaves those out, and formatting is the easiest way to arrive at a red CI on an
otherwise good change.

## 3. Pick Something Small

In order of how easy they are to get merged:

- An issue labeled `good first issue`, `help wanted`, or `documentation`.
- A documentation fix. Every claim in `docs/` is supposed to match the code — if
  you find one that does not, that is a real bug and a welcome pull request.
  `./make check docs` verifies it, and its Markdown lint is the one contributor
  gate that needs Node; without it, open the pull request and let CI run that
  half.
- A missing test for behavior that already works.
- A CLI or TUI rough edge you hit while trying the quickstart.

To claim an issue so others know you are on it, comment `/assign` on it — a bot
assigns you, no repository permission required. Comment `/unassign` to release it.
You can hold two open issues at a time, and a claim that goes quiet for a few
weeks is released automatically so the work does not stay blocked.

Before starting anything larger, read [manual/support.md](../../manual/support.md).
It says which surfaces and deployment paths the project supports today and which
things are deliberately out of scope for the alpha, so your work does not land
outside what maintainers can accept.

Where code lives: [repo-layout.md](repo-layout.md). How a subsystem works:
[architecture/](architecture/README.md).

## 4. Make The Change

```bash
git switch -c short-topic-name
# edit, then:
./make test ./internal/tool   # one package while iterating
./make test                   # the whole suite before you push
```

Package patterns come before `go test` flags — write
`./make test ./internal/tool -run TestX`, not the other way round. A package
after a flag is refused rather than quietly widening the run to `./...`.

Match the surrounding code. The rules that are not visible in the diff —
`snake_case` persisted JSON, singular table names, prefixed entity IDs,
LLM-facing tool output — are in [conventions.md](conventions.md).

Documentation is part of the change, not a follow-up: if you changed behavior or
configuration, update `manual/`, `docs/reference/`, and `config-examples/`
in the same pull request. [documentation.md](documentation.md) says what to
update when.

## 5. Commit And Open The Pull Request

```bash
./make check ci
git commit -m "Fix the workspace path in the sandbox guide"
git push -u origin short-topic-name
```

`./make check ci` is the required pull-request suite plus the path-scoped
release check — the Go gate above, both frontend suites, the
documentation checks, and the repository-wide scans. It needs the pinned Node;
without it, run `./make check go` and let CI cover the other half.

A commit subject is a single imperative line. `Co-authored-by` may credit
substantive human collaboration; do not add assistant, model, or tooling
trailers or a "Generated with …" footer. Add a changelog entry — a new file under
[`docs/changelog/`](../changelog/README.md) — if a user or operator would notice
the change.

Open the pull request against `main` and fill in the template: the problem, the
approach, how you verified it, and anything still missing. Small and verifiable
beats large and thorough — a pull request that a maintainer can read in one
sitting gets reviewed sooner.

Spend a moment on the **title**, and on the commit subjects under it. Pull
requests here are merged with a merge commit, so both land on `main` and both
are what someone reads in `git log` a year from now — one imperative line each,
specific enough to stand alone.

## What CI Will Run

Every pull request runs the three required jobs described in
[CONTRIBUTING.md § Pull Requests](../../CONTRIBUTING.md#pull-requests):
formatting, `go mod tidy` cleanliness, build, vet, golangci-lint, govulncheck,
the test suite with `-race`, the three frontend builds and both frontend test
suites, a secret scan over git history, dependency license checks, and Markdown
lint. None of it needs credentials, so it runs the same way on a fork.

Relevant changes add GoReleaser configuration validation or a Portal image
build; `./make check ci` always runs the first. The Go job cross-compiles for
Windows; the native Windows suite runs after merge, because it needs a Windows
machine.

## If You Get Stuck

- [manual/troubleshooting.md](../../manual/troubleshooting.md) for runtime problems
- [GitHub Discussions](https://github.com/icloudbb/buildmax/discussions) for
  questions — an unfinished pull request with a question in it is also fine
- [../../.github/SUPPORT.md](../../.github/SUPPORT.md) for which channel to use
