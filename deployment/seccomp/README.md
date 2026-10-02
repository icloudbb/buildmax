# Worker seccomp profile

`worker-bwrap.json` is a `securityContext.seccompProfile: {type: Localhost}`
profile for the worker Job pod (`internal/infra/k8s/job.go`). It exists
because Kubernetes' `RuntimeDefault` seccomp profile blocks bubblewrap from
creating the user/mount/pid namespace it needs. The current worker pod runs as
root with every capability dropped except `SYS_ADMIN` and `NET_ADMIN`, uses this profile and
an unconfined AppArmor setting, and keeps a read-only root filesystem. Those
settings are the result of testing `bwrap` against real pods; the initial
non-root configuration could not make an added capability effective at exec.
See [`docs/design/agent-sandbox-policy.md`](../../docs/design/agent-sandbox-policy.md)
and [`docs/current-state.md`](../../docs/current-state.md) for how this fits
the broader sandbox effort.

## Provenance

Base: Docker's own default seccomp profile,
[`daemon/pkg/oci/fixtures/default.json`](https://github.com/moby/moby/blob/master/daemon/pkg/oci/fixtures/default.json)
at blob `8d4d21145eef22ced811b84244da6f6f1b5e8704`, © Docker/Moby project,
licensed Apache License 2.0 — the same profile `RuntimeDefault` is modeled
on, deny-by-default (`SCMP_ACT_ERRNO`) with ~375 syscalls allowed. Used as
the base rather than writing an allowlist from scratch, for the same reason
[`sandbox-boundaries.md`](../../docs/design/sandbox-boundaries.md) shells out
to `bwrap`/`sandbox-exec` rather than reimplementing them: a widely-deployed,
audited default is safer than a bespoke one.

## What changed, and why

Root cause, confirmed with `strace` against a real pod: bubblewrap needs
`unshare`, `setns`, `mount`, `umount2`, `pivot_root`, `clone`, and `clone3` to
build its sandbox, and Docker's own default profile gates most of these
behind `"includes": {"caps": ["CAP_SYS_ADMIN"]}` — a rule that is *dropped
entirely* from the compiled filter when the container's capability bounding
set is empty, which the worker pod's `Capabilities: {drop: [ALL]}` makes it.
This profile removes that gate for exactly those seven syscalls, allowing
them unconditionally, and leaves every other entry from the base profile
untouched. The kernel's own privilege model still enforces the real boundary:
`unshare(CLONE_NEWUSER)` needs no capability at all by design, and the process
only gains capabilities *inside* the namespace it just created for itself —
this profile just stops seccomp from pre-emptively blocking the syscall
before the kernel gets to make that decision.

Verified end to end against a real pod carrying the worker's exact
`PodSecurityContext`: a full `bwrap --ro-bind / / --ro-bind /proc /proc --dev
/dev --tmpfs /tmp --bind <ws> <ws> --die-with-parent --unshare-pid
--unshare-ipc --unshare-uts -- <command>` run — the same argv
`internal/infra/sandbox/bwrap_linux.go` builds — confining a real command:
read inside the bound workspace succeeded, a write outside it was denied with
`Read-only file system`.

## The other half of the fix: `--ro-bind /proc /proc`, not `--proc /proc`

Allowing these syscalls was necessary but not sufficient. With `--unshare-pid`
and a *freshly mounted* `/proc` (`--proc /proc`, bubblewrap's default), the
kernel additionally refused the mount with `EPERM` and logged `VFS: Mount too
revealing` — a real, deliberate Linux VFS protection
(`SB_I_USERNS_VISIBLE` / `mnt_already_visible()` in `fs/namespace.c`), not a
seccomp restriction and not fixable by this profile: it stops a nested,
less-privileged mount namespace from mounting a fresh procfs that would
un-hide paths a container runtime already masked in an ancestor mount
namespace. `internal/infra/sandbox/bwrap_linux.go` re-binds the parent's
`/proc` read-only instead. The accepted cost: a sandboxed process sees the
host container's process list under `/proc`, not an isolated one — the
trade-off for being able to run bubblewrap inside a container's own PID
namespace at all. This is not specific to Kubernetes' seccomp defaults; the
same failure reproduced with seccomp fully disabled and the pod running as
real root, so it would reproduce on any container runtime that masks `/proc`
paths in the way containerd and dockerd both do by default.

## A third restriction: AppArmor, not just seccomp

This profile alone was not sufficient on a host carrying Ubuntu's
`apparmor_restrict_unprivileged_userns` hardening: even with every syscall
above unconditionally allowed, `bwrap` still failed with `Creating new
namespace failed: Operation not permitted`, because a node's default
AppArmor confinement independently denies the unprivileged
`unshare(CLONE_NEWUSER)` `bwrap` needs. `internal/infra/k8s/job.go`'s
container `securityContext` now also sets `appArmorProfile: {type:
Unconfined}`; `deployment/compose/compose.yaml`'s `security_opt` carries the
Compose equivalent (`apparmor:unconfined`), alongside its own copy of this
seccomp profile. Not every host runs AppArmor, so this was invisible in
earlier testing that happened not to hit one that does — same shape of gap
as the seccomp fix above being invisible from a Mac, where the sandbox
backend is Seatbelt and neither restriction exists.

## A fourth restriction: non-root capability grants are not effective at exec

With the seccomp and AppArmor overrides above, the pod got past
`unshare(CLONE_NEWUSER)` itself and failed a step later: `bwrap: setting up
uid map: Permission denied`, writing its own `/proc/self/uid_map` — the
self-mapping an unprivileged user makes into the namespace it just created,
which needs no capability at all in the ordinary (uncontained) case.
Granting the pod `SETUID`/`SETGID` via `securityContext.capabilities.add`
did not fix it: a capability a container runtime adds to a **non-root**
pod lands in that pod's capability *bounding* set only, never its
*effective* set at the initial process's exec time — confirmed directly
(`cat /proc/self/status` inside a throwaway container showed `CapBnd` set
and `CapEff` all zero) and ruled out every other candidate one at a time:
`no-new-privileges` on or off, `SYS_ADMIN` added instead, Docker's
completely untouched default capability set instead of `cap-drop ALL` plus
an explicit add, and file capabilities on the `bwrap` binary itself (which
`bwrap` explicitly refuses to run with — `Unexpected capabilities but not
setuid, old file caps config?` — as a suspicious configuration). None of
that gap exists for a **root** pod: an added capability is effective
immediately.

The worker Job pod therefore now runs as **root (uid 0)** with `SYS_ADMIN`
added rather than non-root with `SETUID`/`SETGID` added — see
`internal/infra/k8s/job.go`'s `containerSecurityContext` and
`docs/reference/configuration.md`'s "How A Worker Pod Is Confined." The
pod's containment is the capability/seccomp/AppArmor set here plus `bwrap`'s
own workspace-scoped sandboxing of the worker's Bash calls, not the pod's
own uid. The Compose target's `local_process` worker needed the identical
`cap_add: SYS_ADMIN` fix, for the same reason: it also runs root.

## Capabilities, network, and `/proc`

Run as root, `bwrap` keeps its capabilities for the command unless told
otherwise: before `--cap-drop ALL` was added, Bash inside the sandbox ran with
the pod's `CAP_SYS_ADMIN` effective (`CapEff` `0x200000`). It now runs with
none. `NET_ADMIN` was added to the pod for `--unshare-net`: `bwrap` brings up
loopback in the new network namespace, which fails with `loopback: Failed
RTM_NEWADDR` without it, and a user namespace is not an alternative here
(`setting up uid map: Operation not permitted`). With it, a Bash command whose
network tier is not `open` has only loopback, where `socat` bridges the
`HTTP_PROXY` port to the sandbox proxy's Unix socket. Verified on a DOKS worker
pod: through the proxy `200`, a direct connection refused, `ip link set lo down`
refused inside the sandbox.

The sandbox still re-binds the container's `/proc`, because a fresh procfs
cannot be mounted inside the pod's PID namespace. The worker process marks
itself non-dumpable (`PR_SET_DUMPABLE`), so its `/proc/<pid>/environ` — which
holds the storage key and run token — needs `CAP_SYS_PTRACE`, which neither the
pod nor the capability-free sandbox has.

## Distribution

Kubernetes' `Localhost` seccomp profile type requires the JSON file to exist
on every node under `<kubelet-root-dir>/seccomp/` (default
`/var/lib/kubelet/seccomp/`) before a pod naming it can start — there is no
way to inline the profile in the pod spec. The reference deployment ships it
via a `DaemonSet` that copies this file from a `ConfigMap` onto that path on
every node; see `deployment/buildmax-deploy.yaml`.
