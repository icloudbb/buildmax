import { useEffect, useMemo, useRef, useState } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"
import type { Components } from "react-markdown"
import { navigate } from "../../router"
import { useLocale } from "@buildmax/gui"
import { useT } from "../../i18n"

/**
 * Help renders the end-user manual that ships inside the portal image. The pages
 * are plain markdown files under /help, mirrored from the repository-root manual/
 * directory at build time (portal/scripts/sync-manual.mjs). English is the source
 * and lives at /help; the Chinese translation lives at /help/zh. This page
 * fetches <base>/manifest.json for the table of contents and <base>/<slug>.md
 * for a page, so adding documentation never touches the portal bundle — only the
 * markdown. The manual follows the interface language, and switching it here
 * switches the interface too; the slug set is identical across languages, so
 * switching keeps the current page.
 */

type Lang = "en" | "zh"

/** English is served from /help, every other language from /help/<lang>. */
function basePath(lang: Lang): string {
  return lang === "en" ? "/help" : `/help/${lang}`
}

interface HelpPage {
  slug: string
  title: string
  // The markdown file on disk. English pages omit it (the file is `${slug}.md`);
  // the Chinese manual names its files in Chinese, so it sets this explicitly.
  file?: string
}

interface HelpSection {
  title: string
  pages: HelpPage[]
}

interface HelpManifest {
  title: string
  sections: HelpSection[]
}

/** The file a manifest entry points at: its `file`, or `${slug}.md` by default. */
function pageFile(page: HelpPage): string {
  return page.file ?? `${page.slug}.md`
}

/**
 * A page links to another with a relative markdown filename — `sandbox.md` in
 * English, `沙箱.md` in Chinese, optionally `./name.md#anchor`. Return the bare
 * filename so it can be matched against the manifest; anything absolute, external,
 * or not a `.md` file is not an in-manual link.
 */
function linkedFile(href: string): string | null {
  if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("#") || href.startsWith("/")) {
    return null
  }
  const name = href.replace(/^\.\//, "").split(/[#?]/)[0]
  if (!name.toLowerCase().endsWith(".md")) return null
  try {
    return decodeURIComponent(name)
  } catch {
    return name
  }
}

export function Help({ slug }: { slug?: string }) {
  const t = useT()
  const { locale, setLocale } = useLocale()
  const lang: Lang = locale === "zh-CN" ? "zh" : "en"
  const base = basePath(lang)

  const chooseLang = (next: Lang) => setLocale(next === "zh" ? "zh-CN" : "en")

  const [manifest, setManifest] = useState<HelpManifest | null>(null)
  const [manifestError, setManifestError] = useState(false)

  useEffect(() => {
    let alive = true
    setManifest(null)
    setManifestError(false)
    fetch(`${base}/manifest.json`)
      .then((r) => {
        if (!r.ok) throw new Error(`manifest ${r.status}`)
        return r.json()
      })
      .then((m: HelpManifest) => alive && setManifest(m))
      .catch(() => alive && setManifestError(true))
    return () => {
      alive = false
    }
  }, [base])

  const pages = useMemo(
    () => manifest?.sections.flatMap((s) => s.pages) ?? [],
    [manifest],
  )

  // The manual opens on its first page; a slug in the URL selects one directly.
  const activeSlug = slug ?? pages[0]?.slug
  const activePage = pages.find((p) => p.slug === activeSlug)
  const known = activeSlug ? activePage !== undefined : true
  const activeFile = activePage ? pageFile(activePage) : undefined

  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [notFound, setNotFound] = useState(false)
  const contentRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!activeFile) return
    let alive = true
    setLoading(true)
    setNotFound(false)
    setContent(null)
    fetch(`${base}/${encodeURIComponent(activeFile)}`)
      .then((r) => {
        if (!r.ok) throw new Error(String(r.status))
        return r.text()
      })
      .then((t) => alive && setContent(t))
      .catch(() => alive && setNotFound(true))
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [base, activeFile])

  // A new page starts at its top, not wherever the previous one was scrolled.
  useEffect(() => {
    contentRef.current?.scrollTo({ top: 0 })
  }, [activeSlug])

  // Map a linked markdown filename back to the page slug it routes to. Built from
  // the current language's manifest, so a Chinese page's `沙箱.md` link resolves
  // to the same slug English's `sandbox.md` does.
  const fileToSlug = useMemo(() => {
    const m = new Map<string, string>()
    for (const p of pages) m.set(pageFile(p), p.slug)
    return m
  }, [pages])

  const components: Components = useMemo(
    () => ({
      a(props) {
        const { href, children } = props
        const file = href ? linkedFile(href) : null
        const target = file ? fileToSlug.get(file) : undefined
        if (target) {
          return (
            <a
              href={`#/help/${target}`}
              onClick={(e) => {
                e.preventDefault()
                navigate({ name: "help", slug: target })
              }}
            >
              {children}
            </a>
          )
        }
        // Everything else is an external link or an in-page anchor.
        const external = href?.startsWith("http")
        return (
          <a
            href={href}
            target={external ? "_blank" : undefined}
            rel={external ? "noreferrer noopener" : undefined}
          >
            {children}
          </a>
        )
      },
    }),
    [fileToSlug],
  )

  const langToggle = (
    <div className="help__lang" role="group" aria-label={t("help.language")}>
      <button
        type="button"
        className={`help__lang-btn ${lang === "en" ? "help__lang-btn--active" : ""}`}
        aria-pressed={lang === "en"}
        onClick={() => chooseLang("en")}
      >
        EN
      </button>
      <button
        type="button"
        className={`help__lang-btn ${lang === "zh" ? "help__lang-btn--active" : ""}`}
        aria-pressed={lang === "zh"}
        onClick={() => chooseLang("zh")}
      >
        中文
      </button>
    </div>
  )

  if (manifestError) {
    return (
      <div className="help">
        <nav className="help__nav" aria-label={t("help.contents")}>
          <div className="help__nav-head">
            <p className="help__nav-title">{t("help.title")}</p>
            {langToggle}
          </div>
        </nav>
        <div className="help__content">
          <p className="help__error" role="alert">
            {t("help.manifestError")}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="help">
      <nav className="help__nav" aria-label={t("help.contents")}>
        <div className="help__nav-head">
          <p className="help__nav-title">{manifest?.title ?? t("help.title")}</p>
          {langToggle}
        </div>
        {manifest?.sections.map((section) => (
          <div key={section.title} className="help__nav-section">
            <p className="help__nav-heading">{section.title}</p>
            <ul className="help__nav-list">
              {section.pages.map((page) => (
                <li key={page.slug}>
                  <button
                    type="button"
                    className={`help__nav-link ${
                      page.slug === activeSlug ? "help__nav-link--active" : ""
                    }`}
                    aria-current={page.slug === activeSlug ? "page" : undefined}
                    onClick={() => navigate({ name: "help", slug: page.slug })}
                  >
                    {page.title}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>

      <div className="help__content" ref={contentRef}>
        {!known ? (
          <div className="help__empty">
            <p className="help__empty-title">{t("help.notFound")}</p>
            <button
              type="button"
              className="help__empty-link"
              onClick={() => navigate({ name: "help" })}
            >
              {t("help.back")}
            </button>
          </div>
        ) : notFound ? (
          <p className="help__error" role="alert">
            {t("help.pageError")}
          </p>
        ) : loading || content === null ? (
          <div className="help__skeleton" aria-hidden>
            <div className="help__skeleton-line help__skeleton-line--title" />
            <div className="help__skeleton-line" />
            <div className="help__skeleton-line" />
            <div className="help__skeleton-line help__skeleton-line--short" />
          </div>
        ) : (
          <article className="help__article markdown">
            <Markdown remarkPlugins={[remarkGfm]} components={components}>
              {content}
            </Markdown>
          </article>
        )}
      </div>
    </div>
  )
}
