import { useState, type ReactNode } from "react"
import {
  Button,
  ButtonLink,
  CollectionFrame,
  DetailHeader,
  EditorActions,
  Field,
  IconButton,
  Input,
  Menu,
  MenuGroup,
  MenuItem,
  MenuItemRadio,
  MenuSeparator,
  PageHeader,
  Select,
  Skeleton,
  Spinner,
  StatusLabel,
  Textarea,
  ThemeToggle,
  useToast,
  type ButtonSize,
  type ButtonVariant,
} from "@buildmax/gui"
import { Alert, type AlertTone } from "../../components/state/Alert"
import { CopyButton } from "../../components/CopyButton"
import { EmptyState } from "../../components/state/EmptyState"
import { ResourceUnavailable, type ResourceUnavailableKind } from "../../components/ResourceUnavailable"
import { RevisionHistory } from "../../components/RevisionHistory"
import "./Specimen.css"

// The specimen is a design artifact, not part of the app: it renders the shared
// presentation system once, so a reviewer can confirm every action role and
// resource state reads the same across themes and widths before trusting the
// migrated pages. It reuses the real components — a divergence here would be a
// divergence in Portal. See docs/design/portal-frontend-page-system.md.

const VARIANTS: ButtonVariant[] = ["primary", "secondary", "tertiary", "danger"]
const SIZES: ButtonSize[] = ["default", "compact"]

const COLOR_TOKENS = [
  "--color-bg",
  "--color-bg-elevated",
  "--color-bg-muted",
  "--color-bg-hover",
  "--color-text",
  "--color-text-muted",
  "--color-text-subtle",
  "--color-border",
  "--color-border-subtle",
  "--color-link",
  "--color-input-bg",
  "--color-input-border",
  "--color-focus",
  "--color-primary",
  "--color-primary-text",
  "--color-primary-hover",
  "--color-danger",
  "--color-danger-text",
  "--color-status-running",
  "--color-status-success",
]

const SPACE_TOKENS = ["--space-1", "--space-2", "--space-3", "--space-4", "--space-6", "--space-8"]
const RADIUS_TOKENS = ["--radius-control", "--radius-panel"]

// Business, lifecycle, and run status as the server stores them, in either
// case. The specimen proves no raw enum reaches a user: every one goes through
// the one status vocabulary, and an unknown value is humanized.
const STATUS_GROUPS: { title: string; values: string[] }[] = [
  { title: "Business status", values: ["todo", "in_progress", "done"] },
  { title: "Lifecycle", values: ["draft", "published", "archived", "no_runs"] },
  {
    title: "Run status",
    values: ["PENDING", "SCHEDULED", "RUNNING", "SUCCEEDED", "FAILED", "CANCELED", "needs_answer"],
  },
  { title: "Workflow steps", values: ["retry_wait", "waiting", "blocked", "failing", "canceling", "workflow_step"] },
]

const ALERT_TONES: AlertTone[] = ["error", "forbidden", "notFound", "stale"]
const UNAVAILABLE_KINDS: ResourceUnavailableKind[] = ["notFound", "forbidden", "error"]

const noop = () => {
  /* specimen: actions are inert */
}

function Section({ id, title, note, children }: { id: string; title: string; note?: string; children: ReactNode }) {
  return (
    <section className="spec-section" aria-labelledby={`${id}-heading`}>
      <h2 className="spec-section__title" id={`${id}-heading`}>
        {title}
      </h2>
      {note ? <p className="spec-section__note">{note}</p> : null}
      {children}
    </section>
  )
}

function ButtonRow({ variant, size }: { variant: ButtonVariant; size: ButtonSize }) {
  return (
    <div className="spec-row">
      <span className="spec-row__label">
        {variant} · {size}
      </span>
      <Button variant={variant} size={size}>
        Default
      </Button>
      <Button variant={variant} size={size} disabled>
        Disabled
      </Button>
      <Button variant={variant} size={size} busy>
        Working
      </Button>
    </div>
  )
}

function SpecimenMenu() {
  const [language, setLanguage] = useState("en")
  return (
    <Menu
      trigger={(props) => (
        <Button {...props} variant="secondary">
          Ada Lovelace
        </Button>
      )}
    >
      <MenuItem onSelect={noop}>Account</MenuItem>
      <MenuItem onSelect={noop}>Help</MenuItem>
      <MenuSeparator />
      <MenuGroup label="Language">
        <MenuItemRadio checked={language === "en"} onSelect={() => setLanguage("en")}>
          English
        </MenuItemRadio>
        <MenuItemRadio checked={language === "zh-CN"} lang="zh-CN" onSelect={() => setLanguage("zh-CN")}>
          简体中文
        </MenuItemRadio>
      </MenuGroup>
      <MenuSeparator />
      <MenuItem onSelect={noop}>Sign out</MenuItem>
    </Menu>
  )
}

function SpecimenToast() {
  const toast = useToast()
  return (
    <Button variant="secondary" onClick={() => toast("Added Release model. It is available to signed-in users now.")}>
      Show a toast
    </Button>
  )
}

export function Specimen() {
  return (
    <div className="spec-page">
      <header className="spec-head">
        <div>
          <h1 className="spec-head__title">Portal component specimen</h1>
          <p className="spec-head__lede">
            The shared action grammar, semantic tokens, status vocabulary, and resource states in one place. Resize the
            window to 390, 768, and 1280 CSS pixels and toggle the theme; each role and state should keep its meaning.
            This page is a design and review artifact served at <code>/specimen</code>, outside the authenticated shell.
          </p>
        </div>
        <ThemeToggle />
      </header>

      <Section
        id="buttons"
        title="Action grammar"
        note="One Button family owns geometry, focus, hover, disabled, and busy. Domain code supplies role and label. A busy button keeps its width and blocks re-submission."
      >
        <div className="spec-stack">
          {VARIANTS.map((variant) =>
            SIZES.map((size) => <ButtonRow key={`${variant}-${size}`} variant={variant} size={size} />)
          )}
        </div>

        <h3 className="spec-subhead">Link semantics (ButtonLink)</h3>
        <div className="spec-row">
          <ButtonLink href="#buttons" variant="primary">
            Primary link
          </ButtonLink>
          <ButtonLink href="#buttons" variant="secondary">
            Secondary link
          </ButtonLink>
          <ButtonLink href="#buttons" variant="tertiary">
            Tertiary link
          </ButtonLink>
        </div>

        <h3 className="spec-subhead">Icon-only (IconButton — named, 44px target)</h3>
        <div className="spec-row">
          <IconButton aria-label="Refresh" variant="secondary">
            ↻
          </IconButton>
          <IconButton aria-label="Delete" variant="danger">
            ✕
          </IconButton>
          <IconButton aria-label="Working" variant="secondary" busy>
            ↻
          </IconButton>
        </div>

        <h3 className="spec-subhead">Long label keeps its width when busy</h3>
        <div className="spec-row">
          <Button variant="primary">Publish workflow revision</Button>
          <Button variant="primary" busy>
            Publish workflow revision
          </Button>
        </div>
      </Section>

      <Section
        id="tokens"
        title="Semantic tokens"
        note="Page CSS consumes these roles, not raw hex. Colors invert between themes; space, type, and radius are shared across both."
      >
        <h3 className="spec-subhead">Color</h3>
        <div className="spec-swatches">
          {COLOR_TOKENS.map((token) => (
            <div className="spec-swatch" key={token}>
              <span className="spec-swatch__chip" style={{ background: `var(${token})` }} />
              <code className="spec-swatch__name">{token}</code>
            </div>
          ))}
        </div>

        <h3 className="spec-subhead">Space</h3>
        <div className="spec-stack">
          {SPACE_TOKENS.map((token) => (
            <div className="spec-row" key={token}>
              <code className="spec-row__label">{token}</code>
              <span className="spec-bar" style={{ width: `var(${token})` }} />
            </div>
          ))}
        </div>

        <h3 className="spec-subhead">Type</h3>
        <p style={{ fontSize: "var(--font-size-page)" }}>Page heading — --font-size-page</p>
        <p style={{ fontSize: "var(--font-size-section)" }}>Section heading — --font-size-section</p>
        <p style={{ fontSize: "var(--font-size-body)" }}>Body text — --font-size-body</p>
        <p style={{ fontSize: "var(--font-size-meta)" }} className="spec-muted">
          Compact metadata — --font-size-meta
        </p>

        <h3 className="spec-subhead">Radius</h3>
        <div className="spec-row">
          {RADIUS_TOKENS.map((token) => (
            <div className="spec-radius" key={token} style={{ borderRadius: `var(${token})` }}>
              <code>{token}</code>
            </div>
          ))}
        </div>
      </Section>

      <Section
        id="status"
        title="Status vocabulary"
        note="One function maps a status to its label, whichever case the server stored it in; StatusLabel adds a mark for its tone. The text carries the meaning, never the color."
      >
        {STATUS_GROUPS.map((group) => (
          <div key={group.title}>
            <h3 className="spec-subhead">{group.title}</h3>
            <div className="spec-chips">
              {group.values.map((value) => (
                <span className="spec-chip" key={value}>
                  <StatusLabel status={value} />
                  <code className="spec-chip__raw">{value}</code>
                </span>
              ))}
            </div>
          </div>
        ))}
      </Section>

      <Section
        id="fields"
        title="Form fields"
        note="Field pairs a label with its control and reads its hint and error with it. Input, Select, and Textarea share one geometry, focus, disabled, and invalid look."
      >
        <div className="spec-form">
          <Field label="Title" hint="Shown in the Issue list.">
            <Input defaultValue="Write the 0.3 release notes" />
          </Field>
          <Field label="Name" error="A name is required.">
            <Input defaultValue="" />
          </Field>
          <Field label="Description" optional>
            <Textarea rows={3} defaultValue="Collect merged pull requests and group them by area." />
          </Field>
          <Field label="Status">
            <Select defaultValue="in_progress">
              <option value="todo">To do</option>
              <option value="in_progress">In progress</option>
              <option value="done">Done</option>
            </Select>
          </Field>
          <Field label="Executor" hint="Only an administrator can change it.">
            <Input defaultValue="Release writer" disabled />
          </Field>
          <Field group label="Notify" hint="Choose any.">
            <label className="spec-check">
              <input type="checkbox" defaultChecked /> When a run fails
            </label>
            <label className="spec-check">
              <input type="checkbox" /> When a run succeeds
            </label>
          </Field>
        </div>
      </Section>

      <Section
        id="menus"
        title="Menu"
        note="Arrow keys move between items, Home and End jump to the ends, Escape closes and returns focus to the button, and Tab or a click outside closes it."
      >
        <SpecimenMenu />
      </Section>

      <Section
        id="feedback"
        title="Progress and confirmation"
        note="Spinner stands in for a section still loading and Skeleton for the shape of rows or a detail; neither claims a count. A toast confirms an effect the page already shows, and is never the only evidence."
      >
        <h3 className="spec-subhead">Spinner</h3>
        <div className="spec-stack">
          <Spinner />
          <Spinner label="Loading members…" />
        </div>
        <h3 className="spec-subhead">Skeleton</h3>
        <Skeleton lines={3} />
        <h3 className="spec-subhead">Toast</h3>
        <SpecimenToast />
      </Section>

      <Section
        id="anatomy"
        title="Page anatomy"
        note="A collection leads with its heading and one primary action, a detail with its object's name and status, and an editor keeps Save and Cancel in reach — a sticky bar on narrow screens."
      >
        <h3 className="spec-subhead">Collection: PageHeader and CollectionFrame</h3>
        <div className="spec-stack">
          <div className="spec-frame">
            <span className="spec-frame__label">Loading</span>
            <div className="spec-frame__body">
              <CollectionFrame label="Issues list (loading)" loading toolbar={<span>0 issues</span>} />
            </div>
          </div>
          <div className="spec-frame">
            <span className="spec-frame__label">Empty</span>
            <div className="spec-frame__body">
              <CollectionFrame
                label="Issues list (empty)"
                loading={false}
                empty={<EmptyState message="No issues yet. Create the first one to start tracking work." />}
              />
            </div>
          </div>
          <div className="spec-frame">
            <span className="spec-frame__label">Rows</span>
            <div className="spec-frame__body">
              <CollectionFrame
                label="Issues list"
                loading={false}
                toolbar={<span className="spec-muted">2 issues</span>}
                footer={<span className="spec-muted">Page 1 of 1</span>}
              >
                <ul className="spec-rows">
                  <li>
                    Write the 0.3 release notes <StatusLabel status="in_progress" />
                  </li>
                  <li>
                    Document the sandbox network tiers <StatusLabel status="todo" />
                  </li>
                </ul>
              </CollectionFrame>
            </div>
          </div>
        </div>

        <h3 className="spec-subhead">Object detail: DetailHeader</h3>
        <div className="spec-frame">
          <div className="spec-frame__body">
            <DetailHeader
              title="Write the 0.3 release notes"
              status={<StatusLabel status="in_progress" />}
              meta="Owner: Ada · Executor: Release writer"
              actions={
                <>
                  <Button variant="tertiary">Refresh</Button>
                  <Button variant="secondary">Edit</Button>
                </>
              }
            />
          </div>
        </div>

        <h3 className="spec-subhead">Editor: EditorActions</h3>
        <div className="spec-frame">
          <div className="spec-frame__body">
            <div className="spec-form">
              <Field label="Title">
                <Input defaultValue="Write the 0.3 release notes" />
              </Field>
            </div>
            <EditorActions>
              <Button variant="secondary">Cancel</Button>
              <Button variant="primary">Save changes</Button>
            </EditorActions>
          </div>
        </div>
      </Section>

      <Section
        id="states"
        title="Resource and permission states"
        note="Every remote boundary resolves to exactly one of these. Each carries its own next action, never color alone."
      >
        <h3 className="spec-subhead">Loading</h3>
        <Spinner />

        <h3 className="spec-subhead">Ready empty</h3>
        <EmptyState message="No schedules yet. Schedule an Agent to run at a set time." />
        <EmptyState
          message="No issues yet. Create the first one to start tracking work."
          action={{ label: "New issue", onClick: noop }}
        />

        <h3 className="spec-subhead">Alerts (error · forbidden · not found · stale)</h3>
        <div className="spec-stack">
          {ALERT_TONES.map((tone) => (
            <Alert
              key={tone}
              tone={tone}
              message={
                tone === "stale"
                  ? "Showing the last loaded result; the refresh failed."
                  : "The request did not complete."
              }
              retry={tone === "forbidden" ? undefined : { label: "Retry", onClick: noop }}
              navigate={{ label: "Back to collection", onClick: noop }}
            />
          ))}
        </div>

        <h3 className="spec-subhead">Detail load failure (ResourceUnavailable)</h3>
        <div className="spec-stack">
          {UNAVAILABLE_KINDS.map((kind) => (
            <ResourceUnavailable
              key={kind}
              resourceLabel="Issue"
              kind={kind}
              errorMessage={kind === "error" ? "connection reset" : null}
              onRetry={noop}
              backLabel="Back to Issues"
              onBack={noop}
            />
          ))}
        </div>

        <h3 className="spec-subhead">Copy control</h3>
        <div className="spec-row">
          <CopyButton value="run_01MnscxjADkxBGX6BLCBwjj9" label="Copy run ID" />
        </div>
      </Section>

      <Section
        id="history"
        title="Revision history"
        note="Shared across agents and workflows. Shown here loading, ready, empty, and errored."
      >
        <div className="spec-stack">
          <RevisionHistory
            title="Loading"
            state={{ kind: "loading" }}
            onRetry={noop}
            currentRevision={0}
            canRestore={false}
            restoringRevision={null}
            restoreError={null}
            onRestore={noop}
          />
          <RevisionHistory
            title="Ready"
            state={{
              kind: "ready",
              data: [
                { id: "r3", revision: 3, createdBy: "ada", createdAt: "2026-01-12T09:58:00Z", summary: "Tighten the retry policy" },
                { id: "r2", revision: 2, createdBy: "grace", createdAt: "2026-01-12T09:00:00Z", summary: "Add the review step" },
                { id: "r1", revision: 1, createdBy: "ada", createdAt: "2026-01-11T16:30:00Z", summary: null },
              ],
            }}
            onRetry={noop}
            currentRevision={3}
            canRestore
            restoringRevision={null}
            restoreError={null}
            onRestore={noop}
          />
          <RevisionHistory
            title="Empty"
            state={{ kind: "readyEmpty" }}
            onRetry={noop}
            currentRevision={0}
            canRestore={false}
            restoringRevision={null}
            restoreError={null}
            onRestore={noop}
          />
          <RevisionHistory
            title="Errored"
            state={{ kind: "error", error: { kind: "error", message: "Could not load history." } }}
            onRetry={noop}
            currentRevision={0}
            canRestore={false}
            restoringRevision={null}
            restoreError={null}
            onRestore={noop}
          />
        </div>
      </Section>

      <Section
        id="reflow"
        title="Narrow reflow"
        note="A representative header and empty collection constrained to 390 and 768 CSS pixels. The primary action keeps its priority and accessible name at every width."
      >
        <div className="spec-frames">
          {[390, 768].map((width) => (
            <div className="spec-frame" key={width} style={{ width }}>
              <span className="spec-frame__label">{width}px</span>
              <div className="spec-frame__body">
                <PageHeader
                  title="Issues"
                  actions={
                    <Button variant="primary" size="compact">
                      New issue
                    </Button>
                  }
                />
                <EmptyState message="No issues yet. Create the first one to start tracking work." />
              </div>
            </div>
          ))}
        </div>
      </Section>
    </div>
  )
}
