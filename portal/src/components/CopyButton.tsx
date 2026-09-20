import { Button } from "@buildmax/gui"
import { useEffect, useState } from "react"
import { copyText } from "../lib/clipboard"

interface CopyButtonProps {
  /** The text to copy. */
  value: string
  /** Button label before copying. Defaults to "Copy". */
  label?: string
}

/**
 * CopyButton copies `value` and reflects the outcome — "Copied" on success,
 * "Copy failed" when even the fallback could not — so a click always tells the
 * user what happened instead of appearing to do nothing.
 */
export function CopyButton({ value, label = "Copy" }: CopyButtonProps) {
  const [state, setState] = useState<"idle" | "ok" | "fail">("idle")

  useEffect(() => {
    if (state === "idle") return
    const timer = window.setTimeout(() => setState("idle"), 2000)
    return () => window.clearTimeout(timer)
  }, [state])

  return (
    <Button
      variant="secondary"
      size="compact"
      onClick={() => {
        void copyText(value).then((ok) => setState(ok ? "ok" : "fail"))
      }}
    >
      {state === "ok" ? "Copied" : state === "fail" ? "Copy failed" : label}
    </Button>
  )
}
