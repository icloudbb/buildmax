import { Button, IconButton, type MessageVars, type Translate } from "@buildmax/gui"
import type { ApiSecret, ApiSecretConsumption, ApiSecretEnvGrant } from "../lib/api/types"
import { useT, type MessageKey } from "../i18n"

/**
 * Editor for an agent's Space Secret consumption: a list of environment grants,
 * each delivering a selected item under a chosen variable name, or a whole
 * group under each item's own name with an optional prefix. Values are never
 * shown here -- only which secret and item a run receives. See
 * docs/design/space-secrets.md §6.
 *
 * Consumption-health: each grant is checked against the space's live secrets and
 * an unresolvable one is flagged in place -- a secret that no longer exists, is
 * disabled or destroyed, or an item its secret no longer has. This is the
 * read-only health of §15, surfaced where it is fixed.
 */

type GrantProblem = [MessageKey, MessageVars?]

// grantProblem names why a grant will not resolve at run time, or null when it
// is fine. A grant with no secret chosen yet is not a health problem -- it is
// an unfinished row -- so it returns null.
function grantProblem(grant: ApiSecretEnvGrant, secrets: ApiSecret[]): GrantProblem | null {
  if (!grant.secret) return null
  const secret = secrets.find((s) => s.id === grant.secret)
  if (!secret) return ["agents.secrets.health.missing"]
  if (secret.state === "destroyed") return ["agents.secrets.health.destroyed", { name: secret.name }]
  if (secret.state === "disabled") return ["agents.secrets.health.disabled", { name: secret.name }]
  if (grant.item && !secret.item_names.includes(grant.item)) {
    return ["agents.secrets.health.missingItem", { name: secret.name, item: grant.item }]
  }
  return null
}

/** grantHealth returns a message when a grant will not resolve at run time, or
 * null when it is fine. */
export function grantHealth(grant: ApiSecretEnvGrant, secrets: ApiSecret[], t: Translate<MessageKey>): string | null {
  const problem = grantProblem(grant, secrets)
  return problem ? t(...problem) : null
}

/** consumptionHealthCount reports how many grants will not resolve. */
export function consumptionHealthCount(
  consumption: ApiSecretConsumption | undefined,
  secrets: ApiSecret[],
): number {
  return (consumption?.env ?? []).filter((g) => grantProblem(g, secrets) != null).length
}

export function SecretConsumptionEditor({
  value,
  onChange,
  secrets,
}: {
  value: ApiSecretConsumption
  onChange: (next: ApiSecretConsumption) => void
  secrets: ApiSecret[]
}) {
  const t = useT()
  const grants = value.env ?? []

  function update(next: ApiSecretEnvGrant[]) {
    onChange({ ...value, env: next })
  }

  function setGrant(i: number, patch: Partial<ApiSecretEnvGrant>) {
    update(grants.map((g, j) => (j === i ? { ...g, ...patch } : g)))
  }

  const active = secrets.filter((s) => s.state === "active")

  return (
    <div className="secret-consumption">
      <div className="modal__label">{t("agents.secrets.title")}</div>
      <p className="modal__hint">{t("agents.secrets.hint")}</p>

      {secrets.length === 0 ? (
        <p className="modal__hint">{t("agents.secrets.none")}</p>
      ) : null}

      {grants.map((grant, i) => {
        const chosen = secrets.find((s) => s.id === grant.secret)
        const wholeGroup = !grant.item
        const health = grantHealth(grant, secrets, t)
        return (
          <div key={i} className="admin-card">
            {health ? (
              <p className="settings-section__error" role="alert">
                {health}
              </p>
            ) : null}
            <div className="secret-item-row">
              <select
                className="modal__input"
                value={grant.secret}
                onChange={(e) => setGrant(i, { secret: e.target.value, item: "", env_name: "" })}
              >
                <option value="">{t("agents.secrets.chooseSecret")}</option>
                {active.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
                {chosen && chosen.state !== "active" ? (
                  <option value={chosen.id}>{t("agents.secrets.disabledOption", { name: chosen.name })}</option>
                ) : null}
              </select>
              <IconButton
                variant="tertiary"
                onClick={() => update(grants.filter((_, j) => j !== i))}
                aria-label={t("agents.secrets.removeGrant")}
              >
                ✕
              </IconButton>
            </div>

            <label className="secret-remove-row">
              <input
                type="checkbox"
                checked={wholeGroup}
                onChange={(e) => setGrant(i, e.target.checked ? { item: "" } : { item: "", prefix: "" })}
              />
              {t("agents.secrets.wholeGroup")}
            </label>

            {wholeGroup ? (
              <div>
                <label className="modal__label">{t("agents.secrets.prefix")}</label>
                <input
                  className="modal__input"
                  placeholder={t("agents.secrets.prefixPlaceholder")}
                  value={grant.prefix ?? ""}
                  onChange={(e) => setGrant(i, { prefix: e.target.value })}
                />
              </div>
            ) : (
              <div className="secret-item-row">
                <select
                  className="modal__input"
                  value={grant.item ?? ""}
                  onChange={(e) => setGrant(i, { item: e.target.value })}
                >
                  <option value="">{t("agents.secrets.chooseItem")}</option>
                  {(chosen?.item_names ?? []).map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </select>
                <input
                  className="modal__input"
                  placeholder={t("agents.secrets.envPlaceholder")}
                  value={grant.env_name ?? ""}
                  onChange={(e) => setGrant(i, { env_name: e.target.value })}
                />
              </div>
            )}

            <label className="secret-remove-row">
              <input
                type="checkbox"
                checked={grant.optional ?? false}
                onChange={(e) => setGrant(i, { optional: e.target.checked })}
              />
              {t("agents.secrets.optional")}
            </label>
          </div>
        )
      })}

      <Button
        variant="secondary"
        disabled={secrets.length === 0}
        onClick={() => update([...grants, { secret: "", item: "", env_name: "" }])}
      >
        {t("agents.secrets.add")}
      </Button>
    </div>
  )
}
