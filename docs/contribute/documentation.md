# Documentation Conventions

> **Audience:** contributors · **Status:** current

## Organized By Reader, Not By Artifact

The **end-user manual is [`manual/`](../../manual)** — a task-oriented directory, one
page per capability, that ships inside the Portal image and is served in-app
under **Help**. Everything a person needs to install, run, and drive BuildMax
lives there, from the quickstart and core concepts to every CLI command;
[`manual/manifest.json`](../../manual/manifest.json) is its table of contents.

English is the source. [`manual/zh/`](../../manual/zh) mirrors it as a Simplified
Chinese translation, one page per English page, with its own `manifest.json`; the
Help page offers an EN / 中文 switch. Its files are named in Chinese (`沙箱.md`),
so each manifest entry keeps the English `slug` as its stable URL key and adds a
`file` naming the page on disk. Keep the two languages in step — when you change
an English page, update its `zh/` counterpart in the same pull request.

`docs/` holds everything else, split by the question someone is trying to answer:

| Directory | Reader | Contains |
|---|---|---|
| `deploy/` | Someone running it for a space | Topology, authentication, local cluster |
| `reference/` | Someone looking something up | Configuration, webhook — tables, not prose |
| `contribute/` | Someone changing the code | Layout, architecture, these conventions |
| `design/` | Someone asking "why is it like this" | Semantic design records browsed by domain and marked by lifecycle |
| `proposals/` | Someone evaluating a possible future direction | Exploratory cross-cutting papers that are not committed work |

[`docs/index.md`](../index.md) is the complete, bilingual file directory for
compact and mobile browsing. Keep `docs/README.md` as the task-oriented landing
page rather than duplicating the complete inventory there.

The test for where a document belongs is **who is stuck without it**, not what
kind of document it is.

## Proposals

`proposals/` is for an idea that is consequential enough to need a written
comparison before it becomes a roadmap commitment. It is not a second roadmap,
an issue tracker, or an archive.

A proposal must:

- state its status as `proposal — under discussion`;
- record when discussion opened as `Opened: YYYY-MM-DD`;
- choose the primary domain where a contributor would look for the question;
- link the current roadmap plans and design records it may affect;
- distinguish goals, non-goals, options, open questions, and the evidence
  needed to make a decision;
- avoid documenting behavior as though it has shipped.

A multi-Agent roundtable is a proposal directory rather than one paper. Its
`README.md` owns the question, decision boundary, contribution index, and
evidence standard. Each participant adds one explicitly attributed
`<agent-name>-view.md` and does not rewrite another participant's position.
Only the roundtable README appears in `proposals/README.md`; an accepted
synthesis still retires the directory and moves durable rationale into a
design record.

Keep a proposal while the decision is genuinely open. Once accepted, put the
committed priority in `ROADMAP.md`, move durable rationale into `design/`, and
create implementation issues as appropriate. Then delete the proposal. Delete
rejected or superseded proposals too; git history preserves the discussion.
Only open proposals appear in the live index; accepted rationale belongs in a
design record, and the retired proposal remains available through Git history.

## Design Documents

Use a stable semantic filename such as `sandbox-boundaries.md`. The filename
describes the subject, not chronology, roadmap priority, or lifecycle, so code
comments and other documents can cite it without inheriting planning metadata:

```go
// Mirrors the design in docs/design/sandbox-boundaries.md.
```

`design/README.md` groups records by the primary domain where a contributor
would look for them:

- **Product and Execution Model**;
- **Agent Runtime and Models**;
- **Local Experience**;
- **Space Platform**;
- **Trust and Security**;
- **Operations and Deployment**;
- **Verification**.

The primary domain is a discovery aid, not a statement of exclusive ownership.
A record that crosses boundaries still appears in exactly one domain table and
explains its related domains in its own text. Add a domain only when contributors
have a distinct, durable entry point that the existing domains cannot express;
do not create a directory merely to classify a record.

Each record also has one lifecycle:

- **Direction** — durable decisions spanning roadmap phases.
- **Active plan** — planned or partly implemented work under a
  `ROADMAP.md` priority. These expire when the work lands or changes direction.
- **Specification** — durable records of how an implemented or
  partly implemented subsystem is designed. These stay current.

Keep roadmap priority and detailed implementation status in `ROADMAP.md` and
the individual record. The index carries the lifecycle, a concise progress
label, and a scope description so a reader can distinguish shipped, partial,
unstarted, and decision-only records without turning the index into a second
roadmap. Do not put percentages or slice-by-slice detail in the index; link to
the owning record instead.

A design document is **rationale, not user documentation**. When a design ships
a user-configurable feature, the user-facing half belongs in the `manual/` manual
(or in `reference/` when it is a lookup table), and the design document links to
it and keeps the trade-offs and open gaps.

## Languages And Translations

English is the authoritative language for repository documentation and the
only source of truth for product and architecture decisions. Simplified Chinese
design records are maintained as a derived mirror so Chinese-speaking readers
can review the same rationale without creating a second decision stream.

The mirror has one fixed shape:

- every Markdown file under `docs/design/`, including its index and any future
  subdirectories, has exactly one counterpart at the same relative path under
  `docs/zh-CN/design/`;
- every English design record links to its Chinese counterpart immediately
  after the title;
- every Chinese record links back to its English source;
- the Chinese notice says that the translation is derived and makes the
  English text controlling when the two differ.

There is no automated freshness check: keeping a mirror current is a review
responsibility, not a gate. Do not resolve a disagreement by editing only the
Chinese record. Correct the English source first, then synchronize the
translation in the same change.

Translate prose, headings, tables, link labels, and contents lists completely.
Preserve code, commands, identifiers, paths, URLs, schema and configuration
keys, route patterns, and BuildMax domain names whose capitalization identifies
a product concept. In particular, keep `Agent`, `Task`, `TaskRun`, `Space`,
`Issue`, `Workflow`, `Run`, `CLI`, `TUI`, `Portal`, `Desktop`, `Project`, and
`Artifact` in English. Chinese prose may explain a concept around those names,
but must not replace one with a new domain term. Links between Chinese design
records stay inside the Chinese mirror; links to documentation outside the
mirrored tree continue to point to the authoritative English page.

The author of any English design change owns synchronization of its Chinese
counterpart. Reviewers verify both coverage and semantic fidelity, in
proportion to the decision's risk. Other documentation remains English unless
its directory receives an explicit mirror policy here.

`docs/contribute/exploratory-runs/` receives that policy: every report and the
index has a zh-CN counterpart at the same relative path, English authoritative,
carrying the links and derived-translation notice above. Its reports are
short-lived staging artifacts (see that directory's README), so a report and its
mirror are committed, then converted or discarded, together.

## Retiring A Document

There is no archive directory. A document that no longer describes the current
direction is **deleted** — git history keeps it, and a stale document in the
tree costs more than the history is worth. Recover one with:

```bash
git log --diff-filter=D --oneline -- docs/
git show <commit>^:docs/path/to/file.md
```

If a retired document contains something still true and still needed, move that
content to the `manual/` manual or `reference/` first, verified against the code,
then delete the original.

## Document Header

Every document opens with its audience and status:

```markdown
> **Audience:** operators · **Status:** current
```

`Status` is one of `current`, `planned`, or a specific caveat such as
`current — this describes a known gap`. A reader must be able to tell within one
line whether a document can be trusted.

## Contents Lists

Every document in `proposals/` and `design/` opens with a `## Contents` list,
placed after the header and any related-document links and before the first
section:

```markdown
## Contents

- [1. Decision](#1-decision)
- [2. Why These Concepts Must Stay Separate](#2-why-these-concepts-must-stay-separate)
```

One line per `##` section, in document order. Subsections are left out: a list
long enough to need scrolling has stopped being an overview.

These documents run to several hundred lines and are usually read by someone
deciding whether a section concerns them at all. Without a contents list that
decision costs a scroll through the whole file, which is why the list is
required here and not merely encouraged.

The two `README.md` index files are exempt — they are already lists of links.
The `manual/` manual pages and `reference/` pages are exempt too: they are
task-oriented, and a contents list competes with the task rather than serving
it.

Maintain the list with the document. A section renamed or added without its
entry is worse than no list at all, because a reader trusts one that exists.

## Single Sources Of Truth

Repeating a fact in two documents guarantees that one of them becomes wrong.

| Fact | Lives in | Everything else |
|---|---|---|
| Repository tree | [repo-layout.md](repo-layout.md) | links |
| Environment variables | `internal/config/env_spec.go` → [reference/configuration.md](../reference/configuration.md) | links |
| Config file fields | `config-examples/*.example.yaml` → [reference/configuration.md](../reference/configuration.md) | links |
| HTTP routes | each handler subpackage's `Register` method → `/openapi.json` | links |
| Roadmap priorities | [ROADMAP.md](../ROADMAP.md) | links |

## What Is Enforced

`internal/architecture/docs_test.go` runs with the normal test suite and fails
the build on the ways documentation rots silently:

| Test | Fails when |
|---|---|
| `TestDocsLinksResolve` | A relative markdown link points at a file that does not exist |
| `TestDocsIndexCoversEveryDocument` | A Markdown file under `docs/` is missing from the [single-page index](../index.md) |
| `TestEnvVarsDocumented` | `config.EnvVars()` gains a variable missing from [reference/configuration.md](../reference/configuration.md) |
| `TestToolNamesDocumented` | A tool name constant is missing from [manual/tools.md](../../manual/tools.md) |
| `TestArchitectureToolInventoryCoversEveryToolNameConstant` | A tool declared in `internal/tool/names.go` is missing from the contributor [tool inventory](architecture/tools.md) |
| `TestAgentsMDPathsExist` / `TestAgentsMDRoutesExist` | [AGENTS.md](../../AGENTS.md) cites a path or route that does not exist |
| `TestDocumentedFilePathsExist` | Any document cites a repository file that does not exist |
| `TestDocumentedMakeCommandsExist` | Any document names a `./make` command the task runner does not dispatch |
| `TestCLIReferenceCoversEveryCommand` | A command reaches the binary without reaching [manual/cli.md](../../manual/cli.md) |

The tool-name checks exist because those strings are user-visible contract —
they appear in hook `matcher` regexes and subagent `tools:` fields, so renaming
a tool without updating the docs breaks working configuration silently. The
architecture check reads the declarations from `names.go` so a newly added
surface-scoped tool cannot be omitted by a hand-maintained test list.

The last three carry a short list of the drift they find today, each keyed to an
open issue. Fixing one means deleting its entry, and an entry nothing reports
fails the test too — so the list shrinks as the documentation is repaired
instead of outliving it. Design records are exempt: they record the plan of the
day, and current code wins when the two disagree.

Everything else is convention, upheld in review.

## Updating Docs With Code

| Change | Update |
|---|---|
| Package boundary or runtime contract | The matching document in [architecture/](architecture/README.md), same pull request |
| User-visible behavior or configuration | The `manual/` manual, `reference/`, and `config-examples/` |
| Direction | Add or update a semantic record in [../design/](../design/README.md) |
| A package moves | [repo-layout.md](repo-layout.md) — and nowhere else |

## Style

- Write authoritative documentation in English; follow the mirror policy above
  for Simplified Chinese design records.
- Cite documents by repository-relative path so links survive being moved.
- Prefer a table to a bulleted list when the content is a lookup.
- State the gap. A document that quietly omits what does not work yet is worse
  than no document — say "off by default", "not wired yet", "development only".
