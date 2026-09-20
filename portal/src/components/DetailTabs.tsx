import { useRef, type KeyboardEvent } from "react"

// DetailTab is one section of a resource detail page. count, when positive, is
// shown as a small badge after the label (a run total, a revision number).
export interface DetailTab<T extends string> {
  id: T
  label: string
  count?: number
}

interface DetailTabsProps<T extends string> {
  tabs: DetailTab<T>[]
  active: T
  onChange: (id: T) => void
  // label names the tablist for assistive tech; idPrefix namespaces the tab and
  // panel element ids, e.g. "agent" gives agent-tab-<id> controlling
  // agent-panel-<id>. The host page renders each panel with the matching id.
  label: string
  idPrefix: string
}

// DetailTabs is the tab strip shared by the resource detail pages (agent,
// workflow). It owns the roving-focus keyboard model — Arrow keys move between
// tabs, Home/End jump to the ends — so every detail page presents the same
// keyboard-operable, horizontally scrollable tablist. Panels stay with the host
// page, which shows the one whose id matches active.
export function DetailTabs<T extends string>({ tabs, active, onChange, label, idPrefix }: DetailTabsProps<T>) {
  const refs = useRef<Array<HTMLButtonElement | null>>([])

  function handleKeyDown(event: KeyboardEvent<HTMLButtonElement>, current: T) {
    const index = tabs.findIndex((t) => t.id === current)
    let nextIndex: number
    switch (event.key) {
      case "ArrowRight":
        nextIndex = (index + 1) % tabs.length
        break
      case "ArrowLeft":
        nextIndex = (index - 1 + tabs.length) % tabs.length
        break
      case "Home":
        nextIndex = 0
        break
      case "End":
        nextIndex = tabs.length - 1
        break
      default:
        return
    }
    event.preventDefault()
    onChange(tabs[nextIndex].id)
    refs.current[nextIndex]?.focus()
  }

  return (
    <nav className="detail-tabs" aria-label={label} role="tablist">
      {tabs.map((t, index) => (
        <button
          key={t.id}
          ref={(element) => {
            refs.current[index] = element
          }}
          type="button"
          role="tab"
          className={t.id === active ? "detail-tabs__tab detail-tabs__tab--active" : "detail-tabs__tab"}
          aria-selected={t.id === active}
          aria-controls={t.id === active ? `${idPrefix}-panel-${t.id}` : undefined}
          id={`${idPrefix}-tab-${t.id}`}
          tabIndex={t.id === active ? 0 : -1}
          onClick={() => onChange(t.id)}
          onKeyDown={(event) => handleKeyDown(event, t.id)}
        >
          {t.label}
          {t.count !== undefined && t.count > 0 ? <span className="detail-tabs__count">{t.count}</span> : null}
        </button>
      ))}
    </nav>
  )
}
