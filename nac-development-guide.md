# NAC Project — Development Guide

Weeks 2–4 of Track A. Replacing the stub with something that would survive
contact with real hardware.

Prerequisite: `./lab.sh up && ./lab.sh test` passes. If it doesn't, see
`nac-build-runbook.md`.

---

## Where you are

Week 1 is done. The loop works end to end: DHCP → redirect → login → nftables
set → access, with authorization as a separate decision from authentication.

`portal.py` proves that loop and is wrong in every way that matters. It shells
out to `nft`, stores nothing, and has no concept of a session. It exists to be
deleted.

---

## 1. The design decision that comes first

**Do this before writing any OIDC code.** Retrofitting persistence under an auth
flow built on fire-and-forget is materially more painful than building it in the
right order.

### The problem with the stub

`portal.py` authorizes an **IP address**. That is wrong in a way the lab will
never show you:

- DHCP renews with a different lease → authorization evaporates
- A laptop sleeps and wakes on a stale IP → same
- A phone reconnects with a randomized MAC → appears as a new device
- Worse: an IP is recycled to a different device → authorization *transfers*

### The model

Separate the two things the stub conflates. A **session** belongs to a person and
has a lifetime. A **binding** is where that person currently is on the network,
and it is *expected* to change.

```sql
CREATE TABLE sessions (
  id           uuid PRIMARY KEY,
  subject      text        NOT NULL,   -- IdP 'sub' claim, stable per person
  email        text        NOT NULL,
  roles        text[]      NOT NULL,   -- derived from IdP group membership
  issued_at    timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  revoked_at   timestamptz
);

CREATE TABLE bindings (
  session_id   uuid        NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  mac          macaddr     NOT NULL,
  ip           inet        NOT NULL,
  bound_at     timestamptz NOT NULL DEFAULT now(),
  last_seen    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (session_id, mac)
);

-- One identity per IP at a time. This constraint is the whole point.
CREATE UNIQUE INDEX bindings_ip_unique ON bindings (ip);

CREATE TABLE audit (
  id         bigserial PRIMARY KEY,
  at         timestamptz NOT NULL DEFAULT now(),
  subject    text,
  event      text NOT NULL,   -- auth.success, auth.deny, policy.deny, session.revoke
  resource   text,
  decision   text,
  detail     jsonb
);
```

When DHCP hands out a new lease, you **rebind** — you do not reauthenticate.
That distinction is the difference between a system people tolerate and one they
complain about daily.

### Roles come from the IdP, not from you

`roles` is populated from group claims. You never maintain a role table. When
someone leaves the company and IT disables their account, they lose network
access at next reauth without anyone touching your system. That property is most
of the argument for OIDC over a local credential store.

---

## 2. Daemon structure

Go, per decision #1 in the plan.

```
cmd/nacd/main.go
internal/session/     lifecycle over Postgres
internal/policy/      pure function: (identity, resource, ctx) -> decision
internal/enforce/     interface + nftables implementation
internal/portal/      HTTP handlers
internal/audit/       append-only writer
```

### The enforcement interface

This is the decision that lets you move from a Linux gateway to switch-based
enforcement without a rewrite.

```go
package enforce

type Grant struct {
    IP       netip.Addr
    Resource string   // "internet", "printer"
}

type Enforcer interface {
    // Sync reconciles actual state to match desired. Not Add/Remove.
    Sync(ctx context.Context, desired []Grant) error
}
```

**`Sync`, not `Add`/`Remove`.** The daemon computes the complete desired set from
Postgres and hands it over; the enforcer diffs against what is actually in the
nft sets and reconciles the difference.

This is what makes gateway restarts survivable, and what makes a second
implementation (`enforce/radius` doing CoA) a drop-in rather than a refactor.
An `Add`/`Remove` interface cannot recover from a state divergence it did not
observe — and state *will* diverge.

### The policy engine

```go
package policy

type Decision struct {
    Allow  bool
    Reason string
}

// Pure. No database, no network, no clock reads that aren't passed in.
func Evaluate(identity Identity, resource string, ctx Context) Decision
```

Keeping this pure is what lets you unit-test the authorization layer exhaustively
without any networking. Most of your correctness confidence will come from here,
not from integration tests.

---

## 3. Week 2 — daemon and reconciliation

Build the daemon with hardcoded credentials still in place. Change one thing at a
time: this week replaces the *storage and enforcement*, not the auth mechanism.

**The reconciliation loop:** run `Sync` on boot and every 10 seconds thereafter.
Read all live sessions and bindings from Postgres, compute the desired grant set,
hand it to the enforcer.

**Then prove it:**

```bash
sudo systemctl restart nacd     # or kill -9 the process
sudo ./lab.sh test              # sessions must survive
```

Add that as an assertion in `lab.sh`. It is the least impressive-looking and most
impressive-to-an-engineer property in the project — it is what separates this
from a demo.

Also worth testing deliberately: flush the nft sets by hand while the daemon is
running, and confirm they repopulate within 10 seconds. That is the divergence
case the reconciliation loop exists for.

---

## 4. Week 3 — OIDC

Now the auth flow is a thin layer over machinery that already works: redirect to
the IdP, validate the token, create a session row, map group claims to roles,
insert a binding.

### Two lab-specific traps

**The walled garden must include the IdP.** Add the IdP endpoints to the
nftables ruleset *before* you start, or you will spend an hour debugging a
redirect that cannot reach its destination. Google and Microsoft both serve
across several domains plus CDN hosts — expect to iterate on this allowlist.

**The DNS hijack breaks OIDC.** `lab.sh` currently runs dnsmasq with
`--address=/#/10.77.10.1`, resolving every name to the gateway. That is correct
captive-portal behaviour pre-auth and it makes the lab self-contained, but the
client must genuinely reach the IdP for OIDC to work.

For week 3, drop the hijack and fix the upstream NAT (§6 of the runbook) instead.

The real design needs the hijack to be **conditional** — on for unauthenticated
clients, off once authenticated. dnsmasq cannot do this per-client, so production
runs two resolver views or drops the hijack entirely and relies on the HTTP
redirect alone. Decide which and write it down; this looks fine in the lab and
confuses people in the pilot.

### Certificate handling

Your portal needs to be reachable over HTTP for OS captive-portal detection to
work — the probes are deliberately plaintext. But the OIDC redirect back to your
portal should be HTTPS.

For the lab, a local CA with the root installed on client namespaces is the
honest exercise. Do it as a separate task, not as a blocker on the auth flow.

---

## 5. Week 4 — the IPP proxy

This is the piece that makes the project distinctive. Most captive portal
projects stop at authentication; the authorization layer is what your lead should
be looking at.

**Do not** just open a firewall hole to the printer for authorized users. That is
what the current `printer_allowed` nft set does, and it gives you no per-job
identity and no audit trail.

Instead: the printer is unreachable from any user VLAN. An IPP proxy on the
gateway accepts print jobs, consults the policy engine, and forwards to the
printer on the protected segment.

What that buys you:

- Per-job authorization, not per-session
- Audit entries with identity: `user=avi resource=printer job=doc.pdf decision=allow`
- Denials that are *logged* rather than silently dropped packets
- The printer never exposed, even to authorized users

The denial log entry is the artifact worth screenshotting. `connection refused`
proves nothing; `user=guest resource=printer decision=deny reason=role` proves
the authorization layer exists.

---

## 6. Assertion discipline

**Every bug you fix gets an assertion in `./lab.sh test`.**

The four bugs found on day one — ICMP missing from the walled garden, `((x++))`
aborting under `set -e`, `resolv.conf` shared across namespaces, a mount pinned
by an orphaned `exec` — produced zero new assertions. That is the one habit to
change.

`up` then `test` should remain the single command that tells you whether the
system is intact. As the daemon replaces the stub, the assertions are what stop a
refactor from silently removing a security property.

The assertion that matters most, and that must never be deleted:

```
PASS  still cannot reach the printer (authz is separate)
```

An authenticated client that is still blocked from the printer. When the policy
engine gets real, this is the property most likely to quietly break.

---

## 7. Ordering, restated

The dependency chain, in the order that avoids rework:

1. **Session/binding schema** — everything else assumes it
2. **Daemon + `Sync` enforcer** — with hardcoded creds still in place
3. **Reconciliation loop + restart-survival assertion**
4. **OIDC** — drops in cleanly once 1–3 exist
5. **IPP proxy** — needs the policy engine from 2

Steps 1–3 change *storage and enforcement* while holding the auth mechanism
constant. Step 4 changes the auth mechanism while holding everything else
constant. Changing both at once is where a week disappears.

---

## 8. What carries to Track B

Provided the six locked decisions held, all of it. The gateway daemon runs
unchanged on the mini PC; `lab.sh` is replaced by real interfaces and VLANs, and
nothing above it needs to know.

The one thing that does change: `enforce/nftables` gains a sibling,
`enforce/radius`, and the pilot decides which to use by config. That is the whole
payoff of the `Sync` interface.
