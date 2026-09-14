import type { InputField, InputFormValues } from "./runInput"

interface RunInputFormProps {
  fields: InputField[]
  values: InputFormValues
  disabled?: boolean
  onChange: (name: string, value: string | boolean) => void
}

/**
 * Renders the input form generated from a workflow's input_schema: one control
 * per top-level field, chosen by the field's type. A field outside the scalar
 * subset (object or array) gets a raw-JSON textarea. The server validates the
 * assembled input against the full schema, so this form aims to be usable rather
 * than to re-implement schema validation.
 */
export function WorkflowRunInputForm({ fields, values, disabled, onChange }: RunInputFormProps) {
  if (fields.length === 0) return null
  return (
    <div className="workflow-run-input">
      <h3 className="workflow-run-input__title">Run input</h3>
      {fields.map((field) => {
        const value = values[field.name]
        const label = (
          <span className="workflow-run-input__label">
            {field.name}
            {field.required ? <span className="workflow-run-input__required"> *</span> : null}
          </span>
        )
        if (field.type === "boolean") {
          return (
            <label key={field.name} className="workflow-run-input__field workflow-run-input__field--boolean">
              <input
                type="checkbox"
                checked={value === true}
                disabled={disabled}
                onChange={(e) => onChange(field.name, e.target.checked)}
              />
              {label}
            </label>
          )
        }
        return (
          <label key={field.name} className="workflow-run-input__field">
            {label}
            {field.description ? (
              <span className="workflow-run-input__description">{field.description}</span>
            ) : null}
            {field.type === "enum" ? (
              <select
                value={typeof value === "string" ? value : ""}
                disabled={disabled}
                onChange={(e) => onChange(field.name, e.target.value)}
              >
                <option value="">Choose…</option>
                {field.enumValues?.map((option) => (
                  <option key={option} value={option}>
                    {option}
                  </option>
                ))}
              </select>
            ) : field.type === "json" ? (
              <textarea
                rows={3}
                value={typeof value === "string" ? value : ""}
                disabled={disabled}
                placeholder="JSON value"
                onChange={(e) => onChange(field.name, e.target.value)}
              />
            ) : (
              <input
                type={field.type === "number" || field.type === "integer" ? "number" : "text"}
                value={typeof value === "string" ? value : ""}
                disabled={disabled}
                onChange={(e) => onChange(field.name, e.target.value)}
              />
            )}
          </label>
        )
      })}
    </div>
  )
}
