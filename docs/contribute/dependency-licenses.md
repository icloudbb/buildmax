# Dependency Licenses

> **简体中文：** [阅读中文镜像](../zh-CN/contribute/dependency-licenses.md)
> **Audience:** contributors · **Status:** current

BuildMax ships under [Apache-2.0](../../LICENSE). This page records what its
dependencies are licensed under, and how to re-check.

Last audited 2026-08-15.

## Go

129 modules reported as reachable from the three binaries in `cmd/`:

| License | Count |
|---|---|
| MIT | 59 |
| Apache-2.0 | 42 |
| BSD-3-Clause | 24 |
| BSD-2-Clause | 2 |
| MPL-2.0 | 1 |
| ISC | 1 |

All are compatible with distributing BuildMax under Apache-2.0. All except the
single MPL-2.0 module use permissive licenses.

The single MPL-2.0 dependency is `github.com/go-sql-driver/mysql`. MPL-2.0 is
file-level copyleft: it obliges you to publish modifications **to that
library's own files**, and it places no condition on the rest of the project.
BuildMax uses it unmodified, so nothing further is required. If you ever fork
or patch it in-tree, that fork's sources have to stay available under MPL-2.0.

## npm

The frontend packages (`gui/`, `portal/`, `desktop/frontend/`) resolve to MIT
and ISC only across their production dependencies.

License tools report `@buildmax/gui`, `buildmax-portal`, and
`buildmax-desktop-frontend` as `UNLICENSED`. That is an artifact of their
`"private": true` field, which suppresses the `"license": "Apache-2.0"` they
declare. They are this repository's own packages, not third-party code.

The Portal image carries the attribution these licenses require. Its Docker
build runs `portal/scripts/collect-notices.mjs`, which concatenates the
license text of every production dependency in the `gui` and `portal`
lockfiles into `third-party-notices.txt`, served at the site root next to the
bundle it attributes. That script is the npm counterpart of
`./make release notices`; it lives in node rather than `tools/mk` because it
runs inside the image build stage, which has `node_modules` and no Go
toolchain. Regenerate locally with:

```bash
node portal/scripts/collect-notices.mjs --out /tmp/notices.txt gui portal
```

## Re-running the audit

Go, including a gate that fails on copyleft that would conflict with
redistribution:

```bash
go install github.com/google/go-licenses@v1.6.0
go-licenses report ./cmd/...
go-licenses check ./cmd/... --disallowed_types=forbidden,restricted
```

The `check` command runs in CI on every pull request, so a dependency carrying
a forbidden or restricted license fails the build rather than being noticed
later.

npm, across all three lockfiles:

```bash
./make release licenses
```

The npm check reads all three committed lockfiles, ignores development-only and
local linked packages, and fails when a production dependency has missing or
unapproved license metadata. Both Go and npm checks run in CI on every pull
request.

`go-licenses` may print warnings for modules that contain assembly it cannot
follow while discovering further transitive dependencies. Those inspection
warnings are not license findings; the modules' declared license files are
still checked and collected.

## Attribution in releases

Apache-2.0 §4(d) requires carrying forward the attribution notices of
Apache-2.0 dependencies when you redistribute them, and compiled BuildMax
binaries contain that dependency code.

`./make release notices` collects the full license text of every module linked into the
binaries into a single `NOTICE-THIRD-PARTY` file — 132 modules at the last run.
GoReleaser runs it as a pre-build hook, so every release archive and the
container image ship it. The file is generated, not committed.

To produce it locally:

```bash
go install github.com/google/go-licenses@v1.6.0
./make release notices
```

The document must be reproducible: the same dependency set has to produce the
same bytes, so modules are concatenated in byte order and the header is fixed.
`TestWriteNoticesBytes` and `TestLicenseFilesSortedByByteOrder` in `tools/mk` hold
that contract.

`go-licenses` v1.6.0 fails to resolve the standard library when `GOROOT` points
inside the module cache, which is where Go puts a toolchain it downloaded
itself. If `go env GOROOT` shows a `golang.org/toolchain@...` path, install the
matching Go release normally, or generate the file in a container:

```bash
docker run --rm -v "$PWD:/repo" -w /repo golang:1.26.9 bash -c \
  'go install github.com/google/go-licenses@v1.6.0 && PATH=$PATH:$(go env GOPATH)/bin go run ./tools/mk release notices'
```
