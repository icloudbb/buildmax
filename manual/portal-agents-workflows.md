# Agents & workflows

Agents and workflows are the reusable building blocks you assign work to. An agent
is a saved definition of *how* an agent should behave; a workflow is a graph
of Agent steps connected by dependencies. Both live in the current space and
keep a numbered history.

## Create an agent

Open **Agents** in the sidebar and create a new agent. An agent definition has:

- **Name** and **description** — how it shows up when you assign it.
- **Instructions** — the standing prompt that tells the agent how to work. This is
  the heart of the definition. (In a background run, the space's shared Agent
  instructions from **Space → Overview** are sent first, then these.)
- **Model** — which of the deployment's models it runs on.
- **Plugins** — optional [plugins](plugins.md) the agent may use.
- **Sandbox tiers** — the filesystem and network confinement for its `Bash` tool.
  See [Sandbox](sandbox.md).

Save the definition to make it assignable. You set an agent as an issue's
**Executor** from the issue's Overview tab — see
[Conversations & issues](portal-issues.md).
On the agent page, **Run agent** starts a task. The **Configuration** tab has
**Save changes** and **Delete agent**; the tab row also works with Left and
Right Arrow keys. On narrow screens, scroll the tab row sideways to reach
later sections.

### Versions

Every time you save an agent, BuildMax records a new numbered version along with
who wrote it. You can **Restore** an earlier version, which records a *new* version
rather than erasing the ones since — so history stays intact. A workflow run notes
the exact agent version each step ran under, so past runs stay readable even after
the definition moves on.

Deleting an agent removes it from the space but keeps the record behind it, so runs
and history that already name it stay readable. An agent that a published workflow
still uses can't be deleted until that workflow is changed or archived.

## Create a workflow

Open **Workflows** in the sidebar and choose **New Workflow**. A workflow's
detail page is organized into tabs, the same layout the agent page uses:
**Overview** (a read-only view of the plan), **Definition** (the editor and the
lifecycle actions), **Runs**, **Schedules**, and **Revisions**. A draft opens on
its Definition tab; a published workflow opens on Overview. **Run Workflow** sits
in the header and is enabled once the workflow is published.

A workflow is a reusable execution plan you can run manually or assign to an
issue. Build it on the **Definition** tab in the visual graph editor:

- Use **Add step** to create an Agent step. Select its node to edit its id,
  target Agent, prompt, Issue access, and input bindings in the inspector.
  `agent_task` is the only node type the runtime executes today.
- Drag from one node's right edge to another's left edge to make the second
  depend on the first. A node runs after all its dependencies succeed;
  independent nodes may run together. **Max parallel** sets a per-run ceiling.
  **Re-layout** arranges the graph without changing its execution rules.
- **Edit raw JSON** shows the same definition for exact inspection and for
  fields the visual editor does not yet author, including `input_schema`,
  `result`, and a node's `output_schema`. **Visual editor** returns to the
  graph. Both views use the same validation when you save.

### Draft, publish, archive

A workflow has a status:

- **Draft** — still being edited.
- **Published** — ready to use. A workflow must be published before you can run it
  manually or assign it to an issue.
- **Archived** — retired from use.

Set the status from the **Definition** tab, whose actions name the state they
reach: **Publish** (the primary action) makes the workflow runnable, **Save as
draft** keeps your changes without publishing, **Archive** retires it, and
**Discard changes** drops unsaved edits. Editing a published workflow and saving
writes a new revision. The **Revisions** tab holds earlier versions. Step removal
and input removal use destructive controls in the editor.

### Run a workflow

Once a workflow is published, use **Run Workflow** to run it. You'll be taken to
the run's detail view, where each step shows its own status as it executes. You can
also assign the workflow to an issue so it runs as that issue's work — see
[Conversations & issues](portal-issues.md).

Like agents, workflows keep a numbered history, and a run records the workflow
version it expanded so the record of a past run stays accurate.

## Schedule an agent

An agent can run on a timetable with nobody pressing Run. Open the agent's
detail view and use the **Schedules** section: give the schedule a name, the
input the agent should receive each time, a five-field cron expression such as
`0 9 * * 1-5`, and the IANA timezone the expression is read in, such as
`Asia/Shanghai`.

Each firing creates an ordinary task for that agent, so it shows up in the task
list with its own status, trace, and artifacts. The section lists each
schedule's next and last firing and the tasks it created, and lets you disable,
re-enable, or delete it. Deleting a schedule keeps the tasks it already created.
If its triggered-task list fails to load, the schedule card shows the error and
offers **Retry triggered tasks**.

A schedule pauses itself after five consecutive firings fail to start a task,
or when its creator's account is disabled; re-enable it once the cause is
fixed. If the server was down across a firing time, the schedule fires once
when it comes back and then resumes its regular times rather than replaying
every missed slot.

## Schedule a workflow

A published workflow can run on a timetable the same way. Open the workflow's
detail page and its **Schedules** tab: the workflow is already chosen,
so you give the schedule a name, the run input its input form asks for (the same
form the manual Run dialog uses; a workflow with no inputs needs none), a cron
expression, and a timezone. Each firing starts a workflow run, listed under
**Show triggered runs** with its status, and the run opens like any other. Only
published workflows can be scheduled — publish a draft first. Pausing,
consecutive-failure handling, and missed-firing behaviour work exactly as they
do for an agent schedule.

The **Schedules** entry in the sidebar shows every schedule in the space across
all agents and workflows, so you can see what unattended work is set up and
pause any of it; you can also create one there and pick what it runs. **Pause
all** and **Resume all** flip every schedule in the space at once, to halt or
restart all unattended work without touching each row.

## Plugins from the Marketplace

The **Marketplace** icon in the top bar lists the plugins this deployment
publishes — skills, subagents, MCP servers, and hooks. It's a browse surface:
installing happens where the agent actually runs, so the catalog hands you the
install command rather than a button. See [Plugins](plugins.md).

## Next

- Assign these to real work: [Conversations & issues](portal-issues.md).
- Tune what an agent can run: [Sandbox](sandbox.md) and [Tool permissions](tool-permissions.md).
