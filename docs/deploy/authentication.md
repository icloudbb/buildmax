# Authentication

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/authentication.md)
> **Audience:** operators · **Status:** current
People sign in with an email address and a password. BuildMax has no way to
send email, and everything unusual below follows from that: accounts are created
by an operator, and the one-time codes that claim an account or reset a
forgotten password are delivered by hand.

For the broader alpha support boundaries, see the
[support matrix](../../manual/support.md).

## Creating An Account

Two commands on the server, and one code you pass along:

```bash
buildmax-server user create alice@example.com
buildmax-server user login-code alice@example.com
```

A new account has no password. The second command prints a code, once:

```text
Login code for alice@example.com:

  bmxlogin_5e9e03467d578f8c248175343d627e814bc3ed10a8a05655a1c500b27dbd17cd

Valid until 2026-08-15T19:22:58+08:00, and only once.
```

Send it over whatever channel you already trust. The person signs in with it —
"Forgot your password, or have a login code?" on the sign-in form — and then
sets a password from account settings. After that they sign in normally and you
are not involved again. `--ttl` changes the code's lifetime, which defaults to
an hour.

A login code is the only way to set a first password: the person signs in with
it and chooses their own, which then exists only where they put it. There is no
command that sets a password for someone — that would put a secret in shell
history and hand it to them over a channel you would then have to trust.

Both commands read the same `server.yaml` the server does, so inside a container
they need no extra configuration:

```bash
kubectl exec -n buildmax deploy/buildmax-server -- \
  buildmax-server user login-code alice@example.com
```

### What The Code Is

A code is not a weaker password. It is what an operator vouches for, spent once,
on the way to a password:

- **Single-use.** Redeeming it spends it, whether or not the sign-in that
  follows succeeds. Entering the wrong email address burns the code — issue
  another; that is cheaper than leaving a window for someone to retry a code
  they found.
- **Expiring.** An hour by default.
- **Bound to one account.** The code identifies the user, and the email in the
  request must match it. A code cannot sign anyone into a different account.
- **Stored as a SHA-256 hash.** A database backup yields no usable codes, and a
  lost code cannot be read back — issue a new one.

## System Administrators

A System Administrator is an authority over the **deployment**, held by an
account and separate from every Space role. Space `owner`, `admin`, and `member`
govern one space's people and shared automation; they say nothing about the
server. A grant is what says something about the server.

```bash
buildmax-server admin grant alice@example.com
buildmax-server admin revoke alice@example.com
```

`buildmax-server admin` is the break-glass path: the first grant, and revoking
the last administrator to recover a deployment that has none. Listing who holds
a grant, and routine grants and revocations, are done with `buildmax admin`
against a running server, or in the Portal.

Granting does not create the account — run `buildmax-server user create` first.
Like the account commands, these read the same `server.yaml` the server does,
so inside a container they need no extra configuration.

What the grant carries today is `/api/admin`: listing and inspecting accounts,
creating one, issuing a login code, disabling and enabling access, revoking
sessions, and granting or revoking the role itself. Portal's Administration area
exposes Administrators, Accounts, Spaces, Models, Plugins, Overview, and Audit.
Its Administrators section lists, grants, and revokes roles; `buildmax admin`
provides the signed-in command-line path over the same API. What the grant will
never carry is access to a space's issues, conversations, artifacts, files, or
run traces. Those stay behind space membership, and an administrator who is not
in your space cannot read them.

Disabling an account refuses every credential it holds: password, login code,
refresh token, the access token it is already carrying, and its webhook keys.
Sessions are revoked at the same time, and work it queued but that has not
started fails instead of running. It is not deletion — nothing is removed, and
enabling reverses the state and nothing else.

This command is also the recovery path, which is why the authority lives in the
database rather than in a configuration value. It behaves the same whether the
deployment has ten administrators or none, so a deployment that has lost every
one is recovered with the same line that created the first — no break-glass
credential to store, rotate, or leak. Revoking the last grant is allowed here
and refused through the API, for that reason.

Grants and revocations are recorded in the audit trail, as are account
creation, password setting, and login-code issuance from `buildmax-server
user`. Actions taken from a command line are recorded as the system actor
`buildmax-server`: the command holds the database credentials rather than a
session, so there is no person to name.

## What Signing In Returns

Two credentials, not one:

| | Lives | Stored on the server | Revocable |
|---|---|---|---|
| **Access token** | 15 minutes (`access_token_ttl`) | The token is a signed JWT, but the session it names is a row (`auth_session`) | Yes — the server checks that session every request, so revocation takes effect within `access_token_ttl` |
| **Refresh token** | 30 days (`refresh_token_ttl`) | Yes, as a hash in `user_refresh_token`, belonging to the session | Yes, immediately |

The access token goes with every request and names a session (`sid`). The server
resolves that `auth_session` row on every authenticated request through one
funnel, so `POST /api/auth/logout`, an administrator's revoke, and account
disablement stop an already-issued access token on its next call rather than at
its expiry. The refresh token goes to `POST /api/auth/token/refresh` and nowhere
else, and comes back replaced: each exchange spends the one presented and issues
the next.

Each login opens its own session with an absolute lifetime
(`session_absolute_ttl`, default 90 days): past that ceiling the session is
inactive no matter how often its refresh token rotated, and the person signs in
again. Signing in from a laptop does not disturb a session on a phone, and
logging one out leaves the other alone.

**Where the refresh token lives depends on the client.** Native CLI and Desktop
clients hold it in an OS credential store and use the JSON routes above. The
Portal never receives it as script-readable data: it signs in through
`POST /api/auth/portal/login`, which returns only the access token and sets the
refresh token as a Secure, HttpOnly, `SameSite=Strict` cookie scoped to
`/api/auth/portal`. The Portal exchanges that cookie for a fresh access token at
`POST /api/auth/portal/session` (on load, on reload, and after a 401) and clears
it at `POST /api/auth/portal/logout`. These routes require a same-origin `Origin`
and set no permissive CORS, so the Portal and API must share one origin — a
reverse proxy in production, and the dev server's `/api` proxy locally.

### Reuse Ends The Session

A refresh token presented after it was already exchanged means two copies exist.
The server cannot tell which holder is the legitimate one, so it revokes the
whole session — the honest holder is signed out too. An `auth.refresh_reuse`
audit event records it.

`refresh_rotation_grace` (default 30 seconds) is the one exemption. The CLI and
Desktop share one credentials file across processes, so two of them refreshing
in the same moment is ordinary rather than suspicious; inside that window both
get a usable token. Raising it widens the window in which a stolen token goes
unnoticed.

### The Bound On A Leaked Access Token

Revoking or disabling stops an access token because the server checks its session
on the next request, but a route that reached a user without passing that funnel
would not make that check — the architecture tests exist to keep every
authenticated route on it. The token itself is still a bearer credential with no
per-token revocation list, so `access_token_ttl` (default 15 minutes) remains the
ceiling on how long a leaked one works if the session check is ever bypassed.
Keeping it short costs nothing but refresh traffic.

## Self-Registration Is Closed, And Has No UI

`POST /api/auth/otp` refuses `intent: signup` with `403` unless `server.yaml`
sets `allow_signup: true`. Accounts come from `buildmax-server user create`, and
the Portal offers no sign-up form.

Even with `allow_signup: true`, self-registration only creates the account: the
new account has no password, and there is no way to send its owner anything, so
an operator still has to issue a login code. That is why there is no form for
it.

Nothing verifies that whoever types an address controls it, which is the real
reason this stays closed. On a deployment reachable only from a trusted network
open registration may be what you want; on anything else it is how someone
claims a colleague's address. The server logs a warning at startup whenever it
is on.

## Passwords

Stored as an argon2id hash with a per-account salt, so a database dump yields no
usable passwords and nothing that can be looked up in a precomputed table. The
hashing parameters travel inside each stored hash, which means raising them
later applies to new passwords without invalidating existing ones.

The only rule is length: **at least 12 characters**, at most 1024. There is no
"one digit and one symbol" requirement, because composition rules push people
toward short predictable passwords that satisfy them.

Changing a password requires the current one. Setting the *first* password does
not, because someone who just redeemed a login code has none — that is the
recovery flow finishing. A session by itself is deliberately not enough to
change an existing password: a stolen access token still works until its session
is revoked or it expires, so allowing a password change on the session alone
would let that window become a lasting takeover.

Changing a password does **not** sign existing sessions out. Revoke those
separately if that is the intent.

> **Login is not rate limited.** Nothing throttles password attempts, so a
> server anyone can reach can be brute-forced online. The 12-character minimum
> and a memory-hard hash make each guess expensive, but they are not a
> substitute for throttling. Put a rate limiter in front of a deployment that
> untrusted networks can reach. A unified rate-limiting capability is planned
> and not built.

## Single Sign-On (OIDC)

A deployment can let people sign in through the identity provider their
organization already runs, over OpenID Connect. **Okta is the first supported
provider.** SSO proves who someone is; BuildMax still owns the account, the
session, and every authorization decision — an IdP group or role claim never
grants access here.

Turn it on with an `oidc` block in `server.yaml`:

```yaml
public_base_url: https://buildmax.example.com   # required; the redirect URI is built from it
oidc:
  enabled: true
  display_name: Okta                # labels the Portal sign-in button
  issuer: https://example.okta.com  # the only URL trust root; must be https
  client_id: 0oaExampleClientId
  provisioning: jit                 # jit (default) or existing_only
  allowed_email_domains:            # required and non-empty for jit
    - example.com
  session_max_age: 12h              # ceiling on an SSO session; default 12h
```

Inject the client secret at deploy time rather than writing it to the file:

```bash
BUILDMAX_OIDC_CLIENT_SECRET=…   # never served, logged, or handed to a worker
```

**How a sign-in becomes an account.** On the first verified sign-in, BuildMax
links the IdP identity to an account by the exact `(issuer, subject)` pair — a
value that never changes even if the person's email does. If no link exists yet,
it links an operator-created account whose email matches the verified address;
otherwise, under `provisioning: jit`, it creates the account (and its personal
Space) when the verified email's domain is in `allowed_email_domains`. An empty
domain list means *nobody* is provisioned, not everybody. `provisioning:
existing_only` never creates accounts — an operator provisions them and SSO only
authenticates. An email already linked to a different identity is refused for an
operator to reconcile, never silently moved.

**Native login alongside SSO.** `local_login` gates password and login-code
sign-in independently of SSO:

- `all` (default) — every account can still sign in natively.
- `system_admins` — only System Administrators can, a break-glass path for when
  the IdP is unreachable while everyone else uses SSO.
- `off` — no native login. Only sensible with SSO configured; the server warns
  at startup if nothing is left that anyone can sign in with.

**Setting up the Okta application.** Create an OIDC **Web** application
(confidential client, `client_secret_basic`). Set its sign-in redirect URI to
`<public_base_url>/api/auth/oidc/callback`. Grant the `openid`, `email`, and
`profile` scopes, and assign the people or groups who should reach this
deployment. Copy the issuer, client ID, and client secret into the configuration
above.

A System Administrator can see a person's linked identities at
`GET /api/admin/users/{user_id}/identities` and, **while the account is
disabled**, remove one with the matching `DELETE`. Unlinking removes the binding
only — never the account, its memberships, or its history — so an operator can
correct a mismatch and re-enable the account for a fresh first association.

The IdP's own end-to-end qualification (a pinned Okta tenant, key and secret
rotation drills, RP-initiated logout) is still being completed; the login,
association, and admin surfaces above are in place.

## What Is Still Missing

There is no second factor and no self-service recovery: a forgotten password
means asking an operator for a login code, or signing in through SSO if it is
configured. Nothing verifies that a native email address belongs to the person
using it — addresses are identifiers here, not proof — which is one reason a
deployment serving people outside your organization wants the OIDC provider
above in front of it.

Login attempts are not throttled. See the note under [Passwords](#passwords).

A System Administrator can list live login sessions in Portal's account detail
or `GET /api/admin/users/{user_id}/sessions`, and revoke one through
`DELETE /api/admin/users/{user_id}/sessions/{session_id}` or all through the
collection DELETE route. The list shows session ID, platform, authentication
method, creation, last-seen, and absolute expiry; last-seen is throttled, not a
continuous device-presence signal. There is no self-service session-management
page or dedicated admin CLI session verb. Revoking a session retires its refresh
tokens and marks the `auth_session` row revoked, so an already-issued access
token under it stops on its next request rather than at expiry.

## The Other Credentials

| Credential | Config | Guards |
|---|---|---|
| **JWT secret** | `jwt_secret` / `BUILDMAX_JWT_SECRET` | Signing for all user access tokens. Required. Generate with `openssl rand -hex 32` and inject at deploy time rather than committing it. |
| **Run token** | minted per run, delivered as `BUILDMAX_RUN_TOKEN` | The `/api/worker/*` routes. Signed with the JWT secret, it names one run's user, space, and task, and authorizes that run alone. Not an operator setting — the scheduler issues one for every dispatched run. Lifetime is `worker.run_token_ttl`; there is no renewal, so it must outlast your longest run. |
| **Webhook keys** | created per user via the API | Inbound `POST /api/webhook`. Stored as a SHA-256 hash; the plaintext is shown once at creation. See [reference/webhook.md](../reference/webhook.md). |

Rotating the JWT secret invalidates every issued access token at once. Refresh
tokens survive it — they are stored rows, not signatures — so clients exchange
theirs and carry on rather than needing new login codes. That is usually what
you want from a key rotation, but it means the secret is no longer the way to
sign everyone out.

A run token cannot be revoked before it expires either, for the same reason: it
is a signature, not a row. What bounds it instead is scope — one run — and run
status, since the inference route refuses a run that is no longer executing.

Portal's account detail and the Admin API support both single-session and
all-session revocation. A single-session revoke is checked against the named
account and leaves its other sessions intact. Because the server checks a token's
session on every request, a revoked session's access token stops on its next
call rather than at expiry. These operations do not require direct database
access.

## Reporting Problems

Report authentication or authorization vulnerabilities privately as described in
[SECURITY.md](../../SECURITY.md). Do not open a public issue.
