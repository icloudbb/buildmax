import type { ReactNode } from "react"
import { Skeleton } from "./Spinner"

// The page anatomy from docs/design/portal-frontend-page-system.md, as small
// compositions rather than one page component with a flag per template. They
// own layout and the rules a template enforces; the surface supplies the
// words, the objects, and the actions.

function classes(...names: (string | false | undefined)[]): string {
  return names.filter(Boolean).join(" ")
}

export interface PageHeaderProps {
  /** The page's one real heading. */
  title: ReactNode
  subtitle?: ReactNode
  /** The view's actions, its one primary action first in importance. */
  actions?: ReactNode
  className?: string
}

/** Scope heading and the page's actions, for collections, settings, and administration. */
export function PageHeader({ title, subtitle, actions, className }: PageHeaderProps) {
  return (
    <div className={classes("bm-page-header", className)}>
      <div className="bm-page-header__text">
        <h1 className="bm-page-header__title">{title}</h1>
        {subtitle ? <p className="bm-page-header__subtitle">{subtitle}</p> : null}
      </div>
      {actions ? <div className="bm-page-header__actions">{actions}</div> : null}
    </div>
  )
}

export interface DetailHeaderProps {
  /** The object's name, as the page's one heading. */
  title: ReactNode
  /** The object's status, usually a StatusLabel, read before anything else about it. */
  status?: ReactNode
  /** Who owns or executes it, and when; one line. */
  meta?: ReactNode
  /** The object's next action. The way back to its parent is the shell's breadcrumb, not a header action. */
  actions?: ReactNode
  className?: string
}

/** An object's title and status, then its owner and next action. */
export function DetailHeader({ title, status, meta, actions, className }: DetailHeaderProps) {
  return (
    <div className={classes("bm-page-header", "bm-detail-header", className)}>
      <div className="bm-page-header__text">
        <h1 className="bm-page-header__title">{title}</h1>
        {status || meta ? (
          <p className="bm-detail-header__meta">
            {status}
            {meta ? <span className="bm-detail-header__facts">{meta}</span> : null}
          </p>
        ) : null}
      </div>
      {actions ? <div className="bm-page-header__actions">{actions}</div> : null}
    </div>
  )
}

export interface CollectionFrameProps {
  /** Names the collection for assistive technology. */
  label: string
  /** No authoritative answer yet: show the rows' shape, and claim no count. */
  loading: boolean
  /** The count, view switch, or filters over the rows. */
  toolbar?: ReactNode
  /** Set when the request succeeded with no objects; shown in place of rows. */
  empty?: ReactNode
  /** Pagination, shown only under rows. */
  footer?: ReactNode
  className?: string
  /** The rows. */
  children?: ReactNode
}

/**
 * Rows or a specific empty state, never both, and never a count or pagination
 * before the request has answered.
 */
export function CollectionFrame({ label, loading, toolbar, empty, footer, className, children }: CollectionFrameProps) {
  const showRows = !loading && !empty
  return (
    <section className={classes("bm-collection", className)} aria-label={label} aria-busy={loading || undefined}>
      {toolbar && !loading ? <div className="bm-collection__toolbar">{toolbar}</div> : null}
      {loading ? <Skeleton lines={3} className="bm-collection__skeleton" /> : empty ? empty : children}
      {footer && showRows ? <div className="bm-collection__footer">{footer}</div> : null}
    </section>
  )
}

export interface EditorActionsProps {
  /** Cancel, then the primary Save. */
  children: ReactNode
  className?: string
}

/**
 * An editor's Save and Cancel. On a narrow screen the bar stays at the bottom
 * of the viewport while the form scrolls, so saving never needs a scroll back.
 */
export function EditorActions({ children, className }: EditorActionsProps) {
  return <div className={classes("bm-editor-actions", className)}>{children}</div>
}
