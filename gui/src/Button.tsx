import type { AnchorHTMLAttributes, ButtonHTMLAttributes, ReactNode } from "react"

export type ButtonVariant = "primary" | "secondary" | "tertiary" | "danger"
export type ButtonSize = "default" | "compact"

function buttonClasses(variant: ButtonVariant, size: ButtonSize, className?: string): string {
  return ["bm-button", `bm-button--${variant}`, size === "compact" ? "bm-button--compact" : "", className]
    .filter(Boolean)
    .join(" ")
}

export interface ButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children"> {
  children: ReactNode
  variant?: ButtonVariant
  size?: ButtonSize
  busy?: boolean
}

/** Presentation only: callers decide the action and why it is unavailable. */
export function Button({
  children,
  variant = "secondary",
  size = "default",
  busy = false,
  disabled,
  className,
  type = "button",
  ...props
}: ButtonProps) {
  return (
    <button
      {...props}
      type={type}
      className={buttonClasses(variant, size, className)}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
    >
      <span className="bm-button__content">{children}</span>
    </button>
  )
}

export interface ButtonLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string
  children: ReactNode
  variant?: ButtonVariant
  size?: ButtonSize
}

export function ButtonLink({ children, variant = "secondary", size = "default", className, ...props }: ButtonLinkProps) {
  return <a {...props} className={buttonClasses(variant, size, className)}><span className="bm-button__content">{children}</span></a>
}

export interface IconButtonProps extends Omit<ButtonProps, "children"> {
  "aria-label": string
  children: ReactNode
}

export function IconButton({ className, ...props }: IconButtonProps) {
  return <Button {...props} className={["bm-button--icon", className].filter(Boolean).join(" ")} />
}
