---
id: desktop-ui-gui-build-prerequisite
title: Make e2e desktop-ui self-contained about its gui build prerequisite
roadmap: none
source: direct
depends_on: []
verification: ["./make e2e desktop-ui core", "./make check docs"]
claim:
---

## Outcome

A contributor who follows `docs/contribute/testing.md` can run
`./make e2e desktop-ui` from a clean checkout without a surprise failure. Today
the suite refuses with `gui not built (missing gui/dist/index.js)` while the
doc's "what each suite needs" row lists only Go, Node, and Chromium — an
undocumented prerequisite that makes a green path look broken.

## Scope

Close the gap in one of two ways (implementer picks, stating why):

- Build `gui` in `e2eDesktopUIPreflight` (`tools/mk/desktop_ui.go`) the same way
  it already auto-runs `npm ci` for the desktop frontend test deps, so the suite
  provisions its own prerequisite; or
- Document the `gui` build as a prerequisite for `desktop-ui` in
  `docs/contribute/testing.md` (the suite-needs table and the mirror
  `docs/zh-CN/contribute/testing.md`), keeping the early, well-placed refusal.

Whichever is chosen, remove the asymmetry where the preflight installs frontend
deps automatically but leaves the gui build manual, or explain why it stands.

## Out Of Scope

Changing the desktop build or the deliberate early-refusal design that keeps the
failure out of the Wails CLI's own output.

## Acceptance Criteria

- From a state with no `gui/dist`, either `./make e2e desktop-ui` builds it and
  proceeds, or `docs/contribute/testing.md` names the gui build as a
  prerequisite so the failure is expected.
- The English doc and its zh-CN mirror agree.

## Verification

`./make e2e desktop-ui core` from a clean `gui/dist` to prove the chosen path;
`./make check docs` for the doc/mirror consistency if the doc route is taken.

## Notes

Observed 2026-09-13 on `main` cceba61c: `./make e2e desktop-ui` exited 1
reporting that gui was not built (missing `gui/dist/index.js`) and telling the
reader to run `cd gui && npm ci && npm run build` (`tools/mk/desktop_ui.go:107`).
After that build, `./make e2e desktop-ui core` passed. The refusal itself is intentional
(the comment explains it avoids a confusing failure from inside `wails dev`);
the gap is only that the prerequisite is neither provisioned nor documented.
