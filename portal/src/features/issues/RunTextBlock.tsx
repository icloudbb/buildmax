import { useT } from "../../i18n"
import type { RunText } from "./result"

interface RunTextBlockProps {
  text: RunText
  /** Shorten to this many characters, for a summary that links to the whole. */
  limit?: number
}

/** A run's text, labelled with where it came from so it never reads as input. */
export function RunTextBlock({ text, limit }: RunTextBlockProps) {
  const t = useT()
  const label =
    text.source === "reply"
      ? t("issues.result.reply")
      : text.source === "workflowResult"
        ? t("issues.result.workflowResult")
        : t("issues.result.stepOutput", { step: text.stepId ?? "" })
  const value = limit != null && text.value.length > limit ? `${text.value.slice(0, limit).trimEnd()}…` : text.value
  return (
    <figure className="issue-result__text">
      <figcaption className="page-activity__meta">{label}</figcaption>
      <pre className="issue-outputs__preview">{value}</pre>
    </figure>
  )
}
