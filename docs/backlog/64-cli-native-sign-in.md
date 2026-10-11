---
id: cli-native-sign-in
title: Sign the CLI in through Portal confirmation by default
roadmap: R5
source: docs/design/enterprise-identity-and-access.md#147-client-experience
depends_on: [60-native-sign-in-server.md]
verification: ["./make test", "./make e2e cli", "./make e2e kind", "./make check docs"]
claim:
pr:
---

## Outcome

`buildmax login` works for every account a deployment admits, including SSO
accounts under `local_login: system_admins` or `off`. On a laptop it opens the
browser. On an SSH or container host it prints a URL and code to confirm from
any device. It never asks for an IdP password.

## Scope

- `internal/interface/client`: start and poll calls for the native sign-in
  routes, and send `client_label` on start and on password login.
- `internal/interface/cli/login.go`:
  - keep the server-URL prompt, then start a request by default;
  - print the URL, the code, the expiry, and the cancel hint (design §14.7);
  - poll at the server's interval, adding 5 seconds on `slow_down` and backing
    off on network errors and `5xx` up to 30 seconds;
  - stop at expiry;
  - on success, save the credentials exactly as the password path does, then
    print the signed-in account and server.
- Open the browser at the complete URI with the existing `openExternalURL`
  helper. Move it out of `app_connect.go` so both callers share it. Skip
  opening when `SSH_CONNECTION` or `SSH_TTY` is set, or when a Linux/BSD host
  has neither `DISPLAY` nor `WAYLAND_DISPLAY`.
- Flags:
  - `--no-browser` forces print-only;
  - `--password` keeps today's email, password, and login-code prompts.
- The client label is the host name and OS (`alice-mbp · macOS`), truncated to
  64 characters.
- Each terminal outcome prints one sentence and exits non-zero: denied,
  expired, or server refusal. Ctrl-C stops polling.
- Remove the stale "TUI startup gate" comment on `interactiveLogin`.
- Update `manual/cli.md` and the sign-in pages in `manual/` and `manual/zh/`,
  and add a changelog entry.

## Out Of Scope

- Server routes: [60](60-native-sign-in-server.md). Portal page:
  [62](62-portal-native-sign-in-confirmation.md).
- Localizing CLI output: the CLI stays English (design §14.7).
- A non-interactive or unattended credential (design §7.4, §18).

## Acceptance Criteria

- With `local_login: off` and OIDC enabled, `buildmax login` signs in through a
  Portal SSO confirmation and the CLI enters managed mode.
- With `SSH_CONNECTION` set, no browser is launched and the URL and code are
  printed.
- `--password` behaves exactly as `buildmax login` did before this change.
- Denied and expired requests exit non-zero with their messages, and save
  nothing.
- The stored credentials refresh and log out exactly as a password login's do.

## Verification

- `./make test` for client polling and backoff, headless detection, and label
  composition, against a fake server.
- `./make e2e cli` for the login command, using an in-process server and a
  confirmation made through the API.
- `./make e2e kind` (or a scripted kind case) for a CLI sign-in confirmed
  through the mock OIDC Portal session.
- `./make check docs`.

## Notes

The CLI login code is in `internal/interface/cli/login.go`, and credential
storage is in `internal/interface/auth`. The CLI and Desktop share one stored
login, so a CLI sign-in also signs in Desktop on the same `BUILDMAX_HOME`, as
today.
