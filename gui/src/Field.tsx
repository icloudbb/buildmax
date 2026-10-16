import {
  createContext,
  useContext,
  useId,
  type ComponentPropsWithRef,
  type ReactNode,
} from "react"
import { useGuiT } from "./messages"

interface FieldContextValue {
  id: string
  describedBy?: string
  invalid: boolean
}

const FieldContext = createContext<FieldContextValue | null>(null)

export interface FieldProps {
  label: ReactNode
  /** Local help, read with the control. */
  hint?: ReactNode
  /** What is wrong with this value; marks the control invalid. */
  error?: ReactNode
  optional?: boolean
  /** The control's id. Give it here, not on the control, so the label stays paired. */
  id?: string
  /**
   * Several controls answering one question (checkboxes, radios, a key/value
   * list): a fieldset whose legend names them, instead of one labelled control.
   */
  group?: boolean
  className?: string
  children: ReactNode
}

function joinIds(...ids: (string | undefined)[]): string | undefined {
  return ids.filter(Boolean).join(" ") || undefined
}

/** A label, its control, and the control's hint and error, wired for assistive technology. */
export function Field({ label, hint, error, optional = false, id, group = false, className, children }: FieldProps) {
  const t = useGuiT()
  const generatedId = useId()
  const controlId = id ?? generatedId
  const hintId = hint ? `${controlId}-hint` : undefined
  const errorId = error ? `${controlId}-error` : undefined
  const describedBy = joinIds(hintId, errorId)
  const classes = ["bm-field", group ? "bm-field--group" : "", className].filter(Boolean).join(" ")

  const labelContent = (
    <>
      {label}
      {optional ? <span className="bm-field__optional"> ({t("gui.field.optional")})</span> : null}
    </>
  )
  const help = (
    <>
      {hint ? (
        <p id={hintId} className="bm-field__hint">
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={errorId} className="bm-field__error" role="alert">
          {error}
        </p>
      ) : null}
    </>
  )

  if (group) {
    return (
      <fieldset className={classes} aria-describedby={describedBy}>
        <legend className="bm-field__label">{labelContent}</legend>
        {children}
        {help}
      </fieldset>
    )
  }

  return (
    <div className={classes}>
      <label className="bm-field__label" htmlFor={controlId}>
        {labelContent}
      </label>
      <FieldContext.Provider value={{ id: controlId, describedBy, invalid: Boolean(error) }}>
        {children}
      </FieldContext.Provider>
      {help}
    </div>
  )
}

// A control inside a Field takes its id and description from it; outside one it
// is an ordinary control that the caller names (aria-label, its own label).
function useFieldControl<P extends { id?: string; "aria-describedby"?: string; "aria-invalid"?: unknown; className?: string }>(
  props: P,
  baseClass: string,
): P {
  const field = useContext(FieldContext)
  return {
    ...props,
    id: props.id ?? field?.id,
    "aria-describedby": joinIds(props["aria-describedby"], field?.describedBy),
    "aria-invalid": props["aria-invalid"] ?? (field?.invalid ? true : undefined),
    className: [baseClass, props.className].filter(Boolean).join(" "),
  }
}

export type InputProps = ComponentPropsWithRef<"input">

export function Input(props: InputProps) {
  return <input {...useFieldControl(props, "bm-input")} />
}

export type SelectProps = ComponentPropsWithRef<"select">

export function Select(props: SelectProps) {
  return <select {...useFieldControl(props, "bm-input bm-select")} />
}

export type TextareaProps = ComponentPropsWithRef<"textarea">

export function Textarea(props: TextareaProps) {
  return <textarea {...useFieldControl(props, "bm-input bm-textarea")} />
}
