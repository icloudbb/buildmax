# Exploratory Run Records

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/exploratory-runs/README.md)
> **Audience:** contributors and testing agents · **Status:** current

A staging area for [exploratory testing](../exploratory-testing.md) session
reports. Exploratory output is a transient state: a report is committed here so
contributors can read and discuss a finding, not to archive it. Each file is one
session's report, written from that guide's compact report shape.

Commit a report when a session produced something worth discussing — a confirmed
or suspected defect, a usability obstacle, a reusable charter, or evidence
another contributor will want. A session that found nothing of lasting interest
stays in the gitignored `.artifacts/` working directory and need not be
committed; do not manufacture a coverage record.

## Lifecycle

A report is pending work, not a conclusion. Triage each one into a durable
outcome and then remove it, so this directory holds only reports still awaiting
that step and never accumulates:

- **Convert** an actionable finding into a fix pull request, a
  [backlog](../../backlog/README.md) task, or a GitHub issue, following
  [AGENTS.md](../../../AGENTS.md) — one item, one place. Carry the reproduction
  and evidence into that item, then delete the report; the work now lives there,
  not here.
- **Discard** a report once it has been read and yields nothing actionable.

Because every report is converted or discarded, a report is never edited to
track current state, and the directory stays small.

## Writing A Report

Write from the shape in [exploratory-testing.md](../exploratory-testing.md) and
inline the shortest reproduction and the key observations. The raw captures
under `.artifacts/` are ephemeral; the committed report must stand on its own
once they are gone. Redact credentials, login codes, and unrelated private data
before committing.

## Conventions

- File name: `YYYY-MM-DD-<surface>-<slug>.md`, e.g.
  `2026-09-13-cli-first-use-continuity.md`. `<surface>` is `portal`, `desktop`,
  `cli`, `worker`, or similar.
- Reports are bilingual: English is authoritative, with a Simplified Chinese
  mirror at the same relative path under `docs/zh-CN/contribute/exploratory-runs/`,
  per [documentation.md](../documentation.md). A report and its mirror are
  committed, and converted or discarded, together.
- Add a row below when you commit a report; remove it when you convert or discard
  the report.

## Records

| Report | Surface | Summary |
|---|---|---|
| [2026-09-13-cross-surface-task-continuity.md](2026-09-13-cross-surface-task-continuity.md) | Desktop / Portal / server / worker | Local approval and relaunch plus deployed conversation/Task continuity; wrong continued-run input, false-negative kind 404 probe, login console noise, and Desktop card compression |
