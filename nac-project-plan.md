# Network Access Control — Project Plan

Captive-portal authentication with a resource authorization layer.

---

## How the three tracks relate

These are not three options to choose between. They are three stages with different
purposes, and Track A stays alive for the entire life of the project.

| Track | Purpose | Environment | Duration |
|---|---|---|---|
| **A — Lab** | Development loop. Where all code gets written and debugged. | VMware + network namespaces on one laptop | Weeks 1–4, then permanent |
| **B — Parallel pilot** | Validation with real clients on real hardware, isolated from corporate | Own switch, own APs, own gateway, single uplink | Weeks 5–12 |
| **C — Production** | What it would take to make this something a company depends on | Redundant hardware, ops process, compliance | 6–12 months with a team — see the build-vs-buy verdict |

The relationship that matters: **Track A never stops being the place you write code.**
Track B is an integration environment you visit, not a place you develop. Every bug found
in B gets reproduced in A, fixed in A, and re-verified in B.

---

## Decisions to lock before writing code

These six are expensive to retrofit and cheap to get right at the start. Every one of them
is what makes Track A code survive contact with Track B and Track C.

1. **Enforcement is an interface, not an implementation.**
   `Enforce(session, policy) error` with two backends: nftables (Track A/B) and RADIUS CoA
   (Track B/C). If enforcement logic leaks into your business logic, you cannot move from
   a Linux gateway to switch-based enforcement without a rewrite.

2. **Session state lives in Postgres. Redis is a cache, never the source of truth.**
   You will reboot the gateway. Sessions must survive it.

3. **The policy engine is a pure function of `(identity, resource, context)`.**
   No network calls, no database reads inside it. This makes the authorization layer
   unit-testable without any networking at all, which is where most of your correctness
   confidence will come from.

4. **Identity comes from an IdP. You never store a password.**
   OIDC against Google Workspace or Microsoft 365 from day one. Local credentials are a
   trap — they create a credential store you have to secure, rotate, and offboard from.

5. **IPv6 is handled or explicitly disabled on user segments.**
   IPv4-only nftables rules with IPv6 enabled is a silent bypass of your entire system.

6. **The audit log is append-only from the first commit.**
   Every auth attempt, every policy decision, every session lifecycle event. Retrofitting
   audit trails after the fact means you can never trust the historical data.

---

## Track A — Lab (VMware + namespaces)

**Goal:** a fully working system, exercised by simulated clients, on one laptop.

### Environment

One Linux VM in VMware Workstation Pro (free, no license key). 2 vCPU, 3 GB RAM,
30 GB thin-provisioned. Debian or Ubuntu Server netinst, headless, SSH in from the host.

Inside that VM, the entire topology is built from network namespaces — not more VMs.
A namespace is a kernel data structure; the whole lab costs under 400 MB of RSS.

| Component | Namespace | Approx RSS |
|---|---|---|
| Client A, Client B | `guest-a`, `guest-b` | ~0 |
| Gateway daemon + nftables | `gw` | 40 MB |
| dnsmasq (DHCP + DNS) | `gw` | 10 MB |
| Portal | `gw` | 30 MB |
| PostgreSQL | `svc` | 120 MB |
| Redis | `svc` | 40 MB |
| FreeRADIUS | `svc` | 60 MB |
| CUPS (fake printer) | `printer` | 40 MB |

Open vSwitch runs on the same host as a kernel module plus daemon (~60 MB) and gives you
real 802.1Q with per-port VLAN assignment via `ovs-vsctl set port <p> tag=10`. That is a
genuine managed switch — you do not need a separate switch VM.

**Critical practice:** the entire topology must rebuild from a shell script in under five
seconds. If teardown and rebuild is slow, you will start leaving broken state around and
debugging ghosts.

### Weeks

**Week 1 — plumbing.** Namespaces, veth pairs, OVS bridge, routing, DHCP, DNS. Gateway
blocks everything except a hardcoded allowlist. Verify with `curl` from a client namespace.

**Week 2 — the portal loop.** HTTP redirect to portal, hardcoded credential accepted,
nftables rule inserted for that client's MAC/IP, client reaches the simulated internet.
Implement RFC 8908 (Captive Portal API) and advertise it via DHCP option 114.

**Week 3 — session lifecycle.** OIDC against a test IdP tenant. Tokens, expiry, idle
timeout, revocation. Build the reconciliation loop that rebuilds nftables state from
Postgres on boot — then test it by killing the gateway mid-session.

**Week 4 — authorization.** Printer namespace on its own VLAN, no L2 path from guests.
IPP proxy in front of it so print jobs carry identity and get logged per job. Verify that
authenticated-but-unauthorized is refused *and* that the refusal is logged with identity.

### Exit gate to Track B

- Full loop works: quarantine → portal → OIDC → authorized VLAN → internet
- Gateway reboot mid-session: sessions survive, firewall state rebuilds correctly
- Two concurrent clients with *different* roles get *different* resource access
- Audit log shows a complete, correct trace of both sessions
- Topology rebuilds from script in under 5 seconds

Do not order hardware until all five pass.

---

## Track B — Parallel office pilot

**Goal:** validate against real clients, real OS captive-portal implementations, and real
concurrency, without any possibility of affecting the corporate network.

### The property that makes it safe

**The gateway routes. It never bridges.**

No Layer 2 path exists between your switch and the corporate switch. That single decision
is what eliminates DHCP leaks, ARP visibility, spanning-tree interaction, and MAC table
contamination. Corporate sees exactly one device: your gateway's uplink interface,
behaving like an ordinary host.

If you ever configure a bridge on the uplink, or plug your switch directly into a
corporate port, you have silently converted parallel into inline. That is the mistake to
guard against.

### Phase B0 — Approval (start this in week 1, not week 5)

The only phase with a dependency on other people, so it starts earliest.

**Who:** whoever owns the network (IT/infra), security, facilities if you need cabling or
rack space.

**Bring a one-pager covering:** what you're building; that it runs on a physically separate
switch with a single controlled uplink; which four people are in the pilot; what happens if
it fails; how you tear it down. Lead with the isolation guarantee — it is the part they
care about.

**Ask specifically for:** a dedicated uplink port or permission to bring your own line;
permission to run DHCP on your own segment; a spot for a small box.

**Frame it as a two-month pilot with a defined end date**, not as new infrastructure.

### Phase B1 — Survey

From a laptop on office Wi-Fi:

```bash
ip addr; ip route          # their subnet and default gateway
cat /etc/resolv.conf       # their DNS
ip neigh                   # who else is on the segment
```

Pick a range far from theirs. `10.77.0.0/16` is unlikely to collide, but confirm rather
than assume — a subnet overlap produces routing failures that look like code bugs for
about two days.

Also scope: comms cabinet location, free power socket, shelf space, room temperature, and
whether an unused network drop already exists near your team.

### Phase B2 — Uplink choice

Ranked by how much friction they remove:

1. **Separate broadband line or 5G router with a data SIM** — total independence, not on
   corporate infrastructure at all. Roughly ₹800–1,500/month for basic business fibre.
   If the pilot has any budget, spend it here; it converts the hardest approval into the
   easiest one.
2. **Dedicated port on a guest or DMZ VLAN** — good if IT already has guest segmentation.
   Double NAT is ugly but harmless here.
3. **Regular corporate access port** — works, but every isolation guarantee now rests on
   your configuration being correct. Only with explicit blessing and after B4 passes.

### Phase B3 — Bill of materials

| Item | Suggestion | Rough cost |
|---|---|---|
| Gateway | Intel N100 mini PC, 8–16 GB, dual NIC | ₹13,000–18,000 |
| Managed switch | TP-Link TL-SG2008P or Netgear GS308T (802.1Q + RADIUS) | ₹5,000–8,000 |
| Access points | 2× TP-Link EAP225 (multi-SSID, WPA2-Enterprise) | ₹4,500 each |
| UPS | Small line-interactive unit, gateway only | ₹3,000 |
| Test printer | Raspberry Pi + CUPS | ≤ ₹5,000 |
| Cabling | Patch cords, keystone jacks, cable tester | ₹2,000 |

**Total: ₹35,000–45,000.** A scrappy version (one AP, unmanaged switch, no UPS) lands
around ₹12,000 but caps you at Layer 3 enforcement with no VLANs.

**Use a dedicated test printer.** The office printer is on the corporate LAN; moving it
into your protected VLAN takes it from everyone else. A Pi running CUPS is
indistinguishable for IPP purposes and breaks nothing.

### Phase B4 — Addressing and the isolation contract

**VLANs:**

- **10** `10.77.10.0/24` — quarantine. Unauthenticated. Reaches portal and DNS only.
- **20** `10.77.20.0/24` — authenticated users. Internet, no lateral access.
- **30** `10.77.30.0/24` — protected resources. Reachable only via the policy engine.
- **99** `10.77.99.0/24` — management. Switch, AP admin, gateway SSH. Unreachable from
  any user VLAN and from the uplink.

Gateway takes a trunk port with a subinterface per VLAN. **Enable client isolation on the
APs** — otherwise an unauthenticated device can ARP-spoof an authenticated one on the same
segment.

**The isolation contract — explicit configuration, not intentions:**

*NAT everything outbound.* Masquerade on uplink. Corporate sees only the gateway IP.

*Bind DHCP to downstream interfaces only.* The single most important config line in the
project — a rogue DHCP server answering on the corporate segment is the classic way these
projects get shut down.

```
interface=vlan10
interface=vlan20
except-interface=uplink
bind-interfaces
```

Belt and braces, so a config mistake still cannot leak:

```
nft add rule inet filter output oifname "uplink" udp sport 67 drop
```

*No routing protocols.* Corporate has no route to `10.77.0.0/16` and never learns one.

*Uplink port is a plain untagged access port.* Nothing tagged ever leaves toward corporate.

### Phase B5 — Bench build

Assemble and configure everything on a desk with **the uplink unplugged**. Gateway
subinterfaces, switch VLANs, AP SSIDs, DHCP scopes, portal, RADIUS.

Only when the full auth loop works end to end on the bench does anything move to the
cabinet. Debugging a half-configured system that is already plugged into corporate is how
incidents happen.

### Phase B6 — Prove the isolation

Run from a laptop on the **corporate** network, with your gear powered on and uplinked.
This is the evidence you show IT. Keep the output.

| Test | Command | Expected |
|---|---|---|
| Subnets unreachable | `nmap -sn 10.77.10.0/24` | No hosts |
| No L2 adjacency | `arp-scan --localnet` | Only the gateway's uplink MAC |
| No rogue DHCP | `nmap --script broadcast-dhcp-discover` | Only corporate's DHCP responds |
| Management unreachable | `nmap -p 22,80,443 10.77.99.0/24` | Nothing |

From the gateway, confirm nothing un-NATed escapes:

```bash
tcpdump -i uplink -n 'not host <gateway-uplink-ip>'
```

Silent output expected. Any `10.77.x.x` source here is a NAT gap.

**Re-run the DHCP test after every gateway config change.** It matters most and breaks
most easily.

### Phase B7 — Physical install

Uplink goes in **last**, after everything is mounted, powered, and verified.

Label every cable, the gateway, the switch, each AP:

> PILOT NETWORK — Avi, \<team\> — remove by \<date\> — \<phone\>

This feels like overkill until someone finds an unfamiliar box in the comms cabinet at 8pm
and has to decide whether to unplug it. Labels are also most of what makes IT comfortable,
because they signal the thing is temporary and owned.

Gateway on the UPS. Not the switch or APs — on power loss you want a clean shutdown that
preserves session state, not corrupted Postgres.

### Phase B8 — Pilot and soak (4 weeks)

Four teammates use it as their **secondary** network. They keep corporate Wi-Fi as fallback
for the entire pilot — that fallback is what makes this low-risk enough to approve.

**What real clients break that namespaces did not:**

- **Detection differs per OS.** Windows probes `msftconnecttest.com`, Apple probes
  `captive.apple.com`, Android probes `connectivitycheck.gstatic.com`. Each expects a
  specific response shape. Untestable in a namespace.
- **VPNs and corporate agents** tunnel straight past your enforcement and make you think
  your firewall is broken. Everyone disconnects VPNs.
- **Wi-Fi still associated while on ethernet** — the laptop may prefer the other route.
- **Concurrent logins** are the first real test of the session table and reconciliation
  loop. Expect to find a race. That is the point.
- **MAC randomization** — modern devices randomize per SSID, so a reconnecting client
  appears as a new device if your binding is MAC-based.

The interesting bugs surface between day 3 and day 20: session leaks, DHCP lease expiry
racing session expiry, a laptop that sleeps and wakes on a stale IP.

### Operational requirements for the pilot

**Break-glass.** One labelled switch port on an untagged VLAN straight to the uplink, no
enforcement. When the portal is down at 9am and four people cannot work, you want a cable
to move, not a debugging session.

**External monitoring.** If the portal is down, nobody gets network and nobody can tell
you, because they have no network. A cron job on your laptop hitting the portal and posting
to Slack is sufficient.

**Fail-open vs fail-closed, decided deliberately.** If the policy engine is unreachable:
fail-closed on the protected VLAN, fail-open to internet is a reasonable pilot compromise.
Make it explicit in code, not emergent from a timeout.

**Teardown plan.** One page, written. Which cables come out, in what order, how people get
back to corporate Wi-Fi. You will need this at short notice.

### Failure containment

| Failure | Blast radius |
|---|---|
| Gateway dies | 4 pilot users fall back to corporate Wi-Fi. Nobody else notices. |
| Uplink dies | Pilot users keep local network and printer, lose internet. Worth testing deliberately. |
| Portal down, gateway up | Nobody can authenticate — and nobody can report it. Hence external monitoring. |
| Need immediate bypass | Break-glass port. |

### Exit gate to Track C

- 4 weeks of daily use with no incident affecting the corporate network
- Isolation tests (B6) still pass at end of soak
- All four OS captive-portal detection paths work reliably
- Session state survives at least three unplanned gateway restarts
- Audit log is complete and queryable for the full soak period

---

## Track C — Production grade

### The verdict first

**For most organisations, the correct production answer is to buy, not build.**

A NAC is security-critical infrastructure whose failure mode is either "nobody can work"
or "unauthorized access to internal resources." Mature options exist: PacketFence
(open source), Aruba ClearPass, Cisco ISE, Forescout. They carry years of accumulated
edge-case handling — supplicant quirks, printer and IoT profiling, MDM integration,
certificate lifecycle — that you would otherwise rediscover one incident at a time.

Building your own makes sense when: you have policy requirements no product expresses;
licensing cost at your device count exceeds engineering cost; or the system itself is your
product. "We could build this" is not on that list.

**What the pilot is genuinely worth regardless:** you will understand NAC deeply enough to
evaluate and operate a purchased one competently, and to write a requirements document
that isn't guesswork. That is a real outcome and worth the twelve weeks on its own.

### If you build anyway — what changes

**Authentication.** Captive portal with MAC binding is not a production auth mechanism.
MAC addresses are trivially spoofable. Production means **802.1X with EAP-TLS** and device
certificates distributed via MDM. The captive portal survives only as the guest/BYOD path,
on a segment that reaches nothing internal.

**Enforcement moves off the gateway.** With RADIUS CoA, the switch and AP enforce; the
gateway only decides. A gateway outage then degrades to "no new authentications" rather
than "all traffic stops." This is why the enforcement-as-interface decision matters.

**High availability.** Two gateways minimum, keepalived/VRRP for the virtual IP, Postgres
streaming replication with automatic failover, Redis Sentinel. Two RADIUS servers — clients
handle failover natively. Test failover monthly; untested failover is not HA.

**Scale.** At thousands of devices: RADIUS accounting volume becomes the dominant write
load, session tables need partitioning and aggressive retention, and the reconciliation
loop must handle tens of thousands of rules without O(n²) behavior. Load-test at 10× your
expected device count.

**Security posture.** Threat model written down. Pen test by someone who wasn't involved
in building it. Secrets in a vault, not config files. Signed releases. Dependency scanning.
The management VLAN reachable only via a bastion with MFA.

**Operations.** On-call rotation — this becomes a 24/7 dependency. Runbooks for every
failure mode in the table above. Change management with staged rollout across sites.
SLOs with error budgets. Structured logging into whatever the company already uses.

**Device profiling.** Real networks are full of things that cannot authenticate: printers,
cameras, badge readers, HVAC controllers. You need MAC Authentication Bypass with
fingerprint-based profiling and a workflow for onboarding new device types. This is the
single largest hidden cost in production NAC, and the main thing commercial products are
actually selling.

### Compliance — raise before you collect a single log

You are logging which named person was on the network, when, from which device. Different
category from anything in Track A.

**Log retention.** CERT-In's 2022 directions require covered organisations in India to
retain ICT system logs for a rolling 180 days within Indian jurisdiction, with defined
incident reporting timelines. A company of your size is likely in scope. Ask your security
team what they already do rather than inventing a retention policy.

**Notice and consent.** Under the DPDP Act, the portal needs an acceptable-use and privacy
notice before the login button, stating what you collect and how long you keep it. Legal
will hand you the standard language — do not draft it.

**Audit trail integrity.** Append-only, tamper-evident, with defined access controls on who
can read it. Network auth logs are attractive to an insider trying to cover tracks.

Neither is a blocker. Both are much cheaper at design time than after two months of logs
with no defined retention.

### Realistic cost

6–12 months with 2–3 engineers, plus ongoing operational load indefinitely. Compare
honestly against a PacketFence deployment (weeks, plus operational load) or a commercial
licence before committing.

---

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| IT approval denied or delayed | Medium | Blocks Track B entirely | Start B0 in week 1; offer separate broadband to remove the corporate dependency |
| Rogue DHCP leaks to corporate | Low | Severe — project shut down | `bind-interfaces` + outbound nftables drop + B6 test after every config change |
| Subnet collision with corporate | Medium | Confusing multi-day debugging | Survey first (B1), pick a distant range |
| FreeRADIUS config stalls the project | High | 2–4 week slip | Deliberate scope cut: Track A/B can ship on nftables enforcement alone |
| Session/firewall state divergence | High | Users lose access unpredictably | Postgres as source of truth + continuous reconciliation loop from day 1 |
| Gateway hardware failure during pilot | Medium | Pilot down | Break-glass port; corporate Wi-Fi retained as fallback throughout |
| Scope creep toward production | High | Never ships anything | Explicit exit gates; Track C decision is buy-vs-build, made deliberately |

---

## Timeline

```
Week  1  2  3  4  5  6  7  8  9 10 11 12
      ─────────────
      Track A build
      ▲
      B0 approval conversation starts here
            ────────
            Hardware ordered on verbal yes
                  ──────────
                  B1–B5 survey, BOM, bench build
                        ──
                        B6 isolation verification
                          ─
                          B7 install (one evening)
                           ─────────────────
                           B8 pilot + soak
                                          ▲
                                          Track C: buy-vs-build decision
```

Track A continues as the development loop throughout and beyond.

---

## Scope-cut order, if time runs short

Cut in this order. The last two are what make the project credible; the first two are
what make it impressive.

1. **802.1X / RADIUS CoA** — ship on nftables enforcement. This is the most likely stall
   point and the least transferable learning.
2. **Second AP** — one is enough for four people.
3. **Real internet upstream** — a local nginx serving "you're through" demonstrates the
   same thing.
4. ~~IPP proxy and per-job authorization~~ — do not cut. This *is* the authorization layer.
5. ~~The 4-week soak~~ — do not cut. A narrower system that survived four weeks of daily
   use is worth more than a broader one that has only ever been demoed.
