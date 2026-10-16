import { Button, Field, Input, useLocale, Spinner } from "@buildmax/gui"
import { useCallback, useEffect, useRef, useState } from "react"
import { useAuth } from "../../contexts/AuthContext"
import { useStableT, useT } from "../../i18n"
import { currentAccessToken } from "../../lib/api/session"
import { formatTimestamp, intlLocale } from "../../lib/dateFormat"
import { replaceRoute } from "../../router"
import { confirmNativeSignIn, denyNativeSignIn, lookUpNativeSignIn } from "../../features/nativeSignIn/api"
import {
  clientNameKey,
  decisionFailure,
  displayCode,
  lookUpFailure,
  sessionEndsSoon,
  type SignInPhase,
} from "../../features/nativeSignIn/model"

/**
 * Where a person confirms or denies a CLI or Desktop sign-in (design
 * enterprise-identity-and-access §14.4). It renders after the token gate and
 * before the Space gate, so any account that can sign in to the Portal can
 * confirm, Space or not.
 *
 * Opening the client's link looks the request up, which decides nothing: only
 * a click on Confirm or Deny does. After a decision the code leaves the
 * address, so neither Back nor a reload shows it again.
 */
export function NativeSignIn({ code }: { code?: string }) {
  const stableT = useStableT()
  const { user, logout } = useAuth()
  const [phase, setPhase] = useState<SignInPhase>(() => (code ? { kind: "loading" } : { kind: "entry" }))
  // Only the latest look-up may settle the page: a code typed while another
  // was still loading must not be overwritten by the earlier answer.
  const lookUpSeq = useRef(0)

  // apiFetch sends the in-memory token, which a refresh replaces; reading it at
  // call time keeps a refresh from re-running the look-up.
  const lookUp = useCallback(
    (raw: string) => {
      const value = raw.trim()
      if (!value) return
      const seq = ++lookUpSeq.current
      setPhase({ kind: "loading" })
      lookUpNativeSignIn(value, currentAccessToken() ?? "")
        .then((request) => {
          if (seq !== lookUpSeq.current) return
          setPhase({
            kind: "request",
            code: value,
            request,
            endsSoon: sessionEndsSoon(request.session_expires_at, Date.now()),
            deciding: null,
            error: null,
          })
        })
        .catch((err) => {
          if (seq !== lookUpSeq.current) return
          setPhase(lookUpFailure(err, value, stableT("signIn.failed")))
        })
    },
    [stableT]
  )

  useEffect(() => {
    if (code) lookUp(code)
  }, [code, lookUp])

  function decide(kind: "confirm" | "deny") {
    if (phase.kind !== "request" || phase.deciding) return
    const current = phase
    setPhase({ ...current, deciding: kind, error: null })
    const call = kind === "confirm" ? confirmNativeSignIn : denyNativeSignIn
    call(current.code, currentAccessToken() ?? "")
      .then(() => {
        setPhase({
          kind: "decided",
          decision: kind === "confirm" ? "confirmed" : "denied",
          platform: current.request.platform,
        })
        if (code) replaceRoute({ name: "signIn" })
      })
      .catch((err) => setPhase(decisionFailure(current, err, stableT("signIn.request.decisionFailed"))))
  }

  function signInAgain() {
    // Signing out shows the sign-in page over this address; carrying the code
    // in it brings the person back to this request afterwards, SSO included.
    if (phase.kind === "request" && !code) replaceRoute({ name: "signIn", code: phase.code })
    logout()
  }

  function enterAnother() {
    lookUpSeq.current++
    setPhase({ kind: "entry" })
    if (code) replaceRoute({ name: "signIn" })
  }

  return (
    <NativeSignInView
      phase={phase}
      email={user?.email ?? ""}
      onLookUp={lookUp}
      onConfirm={() => decide("confirm")}
      onDeny={() => decide("deny")}
      onSignInAgain={signInAgain}
      onEnterAnother={enterAnother}
    />
  )
}

export interface NativeSignInViewProps {
  phase: SignInPhase
  /** The signed-in Portal account, which is the one a confirmation signs in. */
  email: string
  onLookUp: (code: string) => void
  onConfirm: () => void
  onDeny: () => void
  onSignInAgain: () => void
  onEnterAnother: () => void
}

/** The page for one state, with no data fetching of its own. */
export function NativeSignInView(props: NativeSignInViewProps) {
  const t = useT()
  return (
    <div className="login-page">
      <div className="login-page__card sign-in-page__card">
        <h1 className="login-page__title">{t("signIn.title")}</h1>
        <PhaseBody {...props} />
      </div>
    </div>
  )
}

function PhaseBody({ phase, email, onLookUp, onConfirm, onDeny, onSignInAgain, onEnterAnother }: NativeSignInViewProps) {
  const t = useT()
  const { locale } = useLocale()
  const [input, setInput] = useState("")

  switch (phase.kind) {
    case "entry":
      return (
        <form
          className="login-page__form"
          onSubmit={(e) => {
            e.preventDefault()
            onLookUp(input)
          }}
        >
          <p className="login-page__subtitle sign-in-page__intro">{t("signIn.entry.intro")}</p>
          <Field id="sign-in-code" label={t("signIn.entry.label")}>
            <Input
              type="text"
              className="sign-in-page__code-input"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="XXXX-XXXX"
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
            />
          </Field>
          <Button type="submit" variant="primary" disabled={!input.trim()}>
            {t("signIn.entry.submit")}
          </Button>
        </form>
      )

    case "loading":
      return <Spinner label={t("signIn.loading")} />

    case "invalid":
    case "throttled":
      return (
        <div className="sign-in-page__state">
          <p className="login-page__error" role="alert">
            {t(phase.kind === "invalid" ? "signIn.invalid" : "signIn.throttled")}
          </p>
          <Button variant="secondary" onClick={onEnterAnother}>
            {t("signIn.enterAnother")}
          </Button>
        </div>
      )

    case "failed":
      return (
        <div className="sign-in-page__state">
          <p className="login-page__error" role="alert">
            {phase.message}
          </p>
          <Button variant="secondary" onClick={() => onLookUp(phase.code)}>
            {t("signIn.retry")}
          </Button>
        </div>
      )

    case "decided":
      return (
        <p className="sign-in-page__outcome" role="status">
          {t(phase.decision === "confirmed" ? "signIn.confirmed" : "signIn.denied", {
            client: t(clientNameKey(phase.platform)),
          })}
        </p>
      )

    case "request": {
      const { request, deciding, error } = phase
      const time = (iso: string) =>
        new Date(iso).toLocaleTimeString(intlLocale(locale), { hour: "2-digit", minute: "2-digit" })
      const label = request.client_label?.trim()
      return (
        <div className="sign-in-page__request">
          <p className="sign-in-page__lead">
            {t("signIn.request.heading", { client: t(clientNameKey(request.platform)) })}
          </p>
          <dl className="sign-in-page__facts">
            <div>
              <dt>{t("signIn.request.clientTerm")}</dt>
              <dd>
                {label ? (
                  <>
                    <span className="sign-in-page__label">{label}</span>
                    <span className="sign-in-page__unverified">{t("signIn.request.unverified")}</span>
                  </>
                ) : (
                  <span className="sign-in-page__muted">{t("signIn.request.noLabel")}</span>
                )}
              </dd>
            </div>
            <div>
              <dt>{t("signIn.request.codeTerm")}</dt>
              <dd>
                <span className="sign-in-page__code">{displayCode(phase.code)}</span>
                <span className="sign-in-page__muted">{t("signIn.request.codeHint")}</span>
              </dd>
            </div>
            <div>
              <dt>{t("signIn.request.timeTerm")}</dt>
              <dd>{t("signIn.request.time", { started: time(request.created_at), expires: time(request.expires_at) })}</dd>
            </div>
            <div>
              <dt>{t("signIn.request.accountTerm")}</dt>
              <dd>
                <span className="sign-in-page__label">{email}</span>
                <span className="sign-in-page__muted">
                  {t("signIn.request.sessionEnds", { ends: formatTimestamp(request.session_expires_at, locale) })}
                </span>
              </dd>
            </div>
          </dl>

          {phase.endsSoon && (
            <div className="sign-in-page__notice" role="status">
              <p>{t("signIn.request.endsSoon")}</p>
              <Button variant="secondary" size="compact" disabled={deciding !== null} onClick={onSignInAgain}>
                {t("signIn.request.signInAgain")}
              </Button>
            </div>
          )}

          <p className="sign-in-page__warning">{t("signIn.request.warning")}</p>

          {error && (
            <div className="sign-in-page__state">
              <p className="login-page__error" role="alert">
                {error.message}
              </p>
              {error.reauthenticate && !phase.endsSoon && (
                <Button variant="secondary" size="compact" onClick={onSignInAgain}>
                  {t("signIn.request.signInAgain")}
                </Button>
              )}
            </div>
          )}

          <div className="sign-in-page__actions">
            <Button variant="primary" busy={deciding === "confirm"} disabled={deciding !== null} onClick={onConfirm}>
              {t("signIn.request.confirm")}
            </Button>
            <Button variant="secondary" busy={deciding === "deny"} disabled={deciding !== null} onClick={onDeny}>
              {t("signIn.request.deny")}
            </Button>
          </div>
        </div>
      )
    }
  }
}
