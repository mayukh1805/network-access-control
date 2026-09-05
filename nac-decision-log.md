# NAC Project — Decision Log

A condensed record of the design conversation and the first build session.
Not a verbatim transcript — it captures the questions asked, what was decided,
and the reasoning, which is what the other four documents leave out.

Useful when someone asks *why* the design is the way it is.

---

## Part 1 — Design decisions

### Q: How should this be built at all?

**Decided:** captive portal for UX, RADIUS as the auth backend, enforcement via
VLAN reassignment plus RADIUS CoA — but with the enforcement layer abstracted so
a Linux-firewall implementation can ship first.

**Reasoning:** three options were on the table.

- *Layer 3 captive portal* — works on any hardware, easy to build, and weak. MAC
  addresses are trivially spoofable, so an attacker who sniffs an authenticated
  MAC inherits the session.
- *802.1X / WPA2-Enterprise* — cryptographically sound, but requires a supplicant
  profile on the client, so it is not the "type a code on a web page" experience
  that was asked for.
- *Hybrid* — portal for UX, RADIUS underneath, CoA to move the client between
  VLANs after login.

The hybrid needs managed switches or APs that speak CoA. Since the project starts
on a laptop, the resolution was: build the L3 version first, but keep enforcement
behind an interface so the CoA implementation is a drop-in later.

### Q: What hardware is needed?

**Decided:** none, initially. Network namespaces on one Linux host.

**Reasoning:** a namespace is a kernel data structure, not a machine. The whole
topology — clients, gateway, services, protected segment — costs under 1 GB.
Open vSwitch supplies real 802.1Q with per-port VLAN assignment on the same host,
so even the "managed switch" is software.

Hardware only becomes necessary for Layer 2 behaviour that cannot be simulated:
Wi-Fi association, real OS captive-portal detection, and physical port security.

### Q: Can this be done in VMware?

**Decided:** yes, one VM. Not the six-VM topology first sketched.

**Correction made here:** the initial answer proposed six VMs across
VMnet2/3/4 — one per component. That was wrong for this project. With
namespaces the entire topology lives inside a single VM's kernel, and the VM's
one NAT interface exists only so the VM itself can reach the internet for
package installs.

VMware genuinely cannot provide: Wi-Fi (no virtual 802.11), managed-switch
semantics in Workstation's VMnets (dumb L2 segments, no per-port VLAN), or iOS
client testing.

### Q: What about 8 GB RAM and an i5-11th gen?

**Decided:** 2 vCPU, 3 GB VM. CPU is a non-issue.

**Reasoning:** the workload is I/O and kernel-bound, not compute-bound — more
cores buy nothing. RAM is the only real constraint, and the namespace approach
means the services total under 1 GB. Windows at ~3.5 GB plus a 3 GB VM leaves
comfortable headroom.

### Q: Four teammates with laptops — can we emulate this live?

**Decided:** yes, but only if the laptop is the actual default gateway and DHCP
server for a segment the clients are on.

**The trap identified:** if everyone joins the existing office Wi-Fi and the
portal runs on one laptop, nothing has been built. Their default route points at
the real router. A login page can be served, but nothing can be blocked,
redirected, or enforced.

Wired plus a cheap switch was recommended over laptop-as-AP: deterministic, no
wireless driver issues, and the only path that supports 802.1X properly.

### Q: How to deploy in the office?

**Decided:** parallel overlay, never inline. Own switch, own APs, own gateway,
one controlled uplink.

**Reasoning:** inline means sitting between the office switch and the router —
every outage becomes everyone's outage. Parallel means a gateway failure affects
four pilot users who still have corporate Wi-Fi as fallback.

**The property that makes it safe:** *the gateway routes, it never bridges.* No
Layer 2 path exists between the pilot switch and the corporate switch, which
eliminates DHCP leaks, ARP visibility, spanning-tree interaction, and MAC table
contamination as a class.

**Step 0 identified as approval, not a formality.** A rogue DHCP server and a
rogue AP are exactly what corporate monitoring is built to detect. Getting
flagged early kills the project permanently; it is far easier to get approval for
something that does not exist yet than forgiveness for something already
plugged in.

### Q: Production grade?

**Decided:** for most organisations the correct production answer is **buy, not
build**.

**Reasoning:** a NAC is security-critical infrastructure whose failure mode is
either "nobody can work" or "unauthorized access to internal resources." Mature
options (PacketFence, ClearPass, ISE, Forescout) carry years of accumulated
edge-case handling — supplicant quirks, IoT profiling, MDM integration,
certificate lifecycle — that would otherwise be rediscovered one incident at a
time.

Building makes sense only when: policy requirements no product expresses;
licensing cost at your device count exceeds engineering cost; or the system is
your product.

**What the pilot is worth regardless:** enough understanding to evaluate and
operate a purchased NAC competently, and to write a requirements document that
is not guesswork.

**Sizing honesty:** 1,000 devices produces under 1 auth/sec sustained and ~200
accounting writes/minute. Compute is not the constraint at this scale —
availability and correctness are. The reason for two of everything is that a NAC
outage stops work, not that one node cannot handle throughput.

---

## Part 2 — Build journal, day one

Environment: existing RHEL VM under VMware on a Windows host.

### Friction before any code ran

| Issue | Root cause |
|---|---|
| `systemctl get-default` → `graphical.target` | ~1 GB of RAM to a desktop never looked at. Switched to `multi-user.target`, idle dropped from ~1.5 GB to 533 MB. |
| `dnf`: no enabled repositories | RHEL not registered with an entitlement server. Free developer subscription, or a local repo from the install ISO. |
| Paste stopped working | VMware clipboard sync runs through `vmtoolsd` and only works into a graphical session. Resolution: stop using the console, SSH in. |
| SSH as root: permission denied | RHEL 9 defaults to `PermitRootLogin prohibit-password`. Created a `wheel` user. |
| `./lab.sh: Permission denied` | `scp` does not preserve the exec bit without `-p`. |

### Four bugs found by running it

**1. ICMP missing from the walled garden.**
The input chain's policy is drop, and the walled garden only permitted DNS,
DHCP, and the portal port. Ping to the gateway was dropped — and the test suite
asserted that ping works. *The test was right; the ruleset was wrong.*

**2. `((x++))` aborting under `set -e`.**
Post-increment returns the *old* value, so `((fail++))` exits 1 when `fail` is 0.
With `set -e` that killed the script on the very first assertion, producing one
FAIL line and a prompt. Replaced with `fail=$((fail+1))`.

Worth remembering generally — it fails only on the first increment, which makes
it look like something else went wrong.

**3. Namespaces share `/etc/resolv.conf` with the host.**
Guests were trying to reach the host's upstream resolver, unroutable from
`10.77.10.0/24`. Fixed with per-namespace `/etc/netns/<ns>/resolv.conf`.

Added alongside: `--address=/#/10.77.10.1` in dnsmasq, resolving every name to
the gateway. Not a workaround — it is what real captive portals do pre-auth, and
it makes the lab self-contained without upstream NAT.

**4. Teardown swallowing its own errors.**
`cmd_down` had `|| true` on every step, so a failure looked identical to
success — `down` reported success while `up` insisted the lab was already
running. Rewrote it to report failures and exit non-zero.

The underlying cause was an orphaned `ip netns exec` process holding the bind
mount at `/run/netns/gw`, created by closing a terminal instead of typing `exit`.
`umount -l` reported *not mounted* while `rm -f` reported *busy* — the mount was
pinned in another mount namespace. **Resolution: reboot.** Namespaces do not
survive a reboot anyway, so the alternative was twenty minutes of
`/proc/*/mountinfo` archaeology to recover thirty seconds.

### The lesson from those four

None were in the design. All four were in the seams between components — the
ruleset versus the test's assumptions, bash arithmetic semantics, namespace
filesystem sharing, mount propagation.

That ratio is normal for this kind of work, and it is the argument for keeping
the namespace lab as the development loop rather than debugging any of it on real
hardware.

**Habit change identified:** those four bugs produced zero new assertions in
`./lab.sh test`. Every bug fixed should add one.

---

## Part 3 — Corrections made along the way

Recorded because they are the kind of thing that otherwise gets repeated.

| Claimed | Actually |
|---|---|
| Six VMs across VMnet2/3/4 | One VM. Namespaces hold the whole topology; the single NAT NIC is only for package installs. |
| Bridge isolation may need `modprobe br_netfilter` | It needs `nf_tables_bridge`, which loads automatically. `br_netfilter` passes bridged traffic up to the ip/inet families — a different thing. |
| `lab.sh` handles upstream internet | It masquerades on the gateway's `up0`, but nothing NATs onward in the root namespace. Documented as a known gap; irrelevant for weeks 1–4 since a local server as "the internet" is the recommended approach anyway. |
| The walled garden was complete | ICMP was missing, so the gateway was unpingable. |

---

## Part 4 — Open questions

Carried forward, not yet resolved.

**Conditional DNS hijack.** `--address=/#/` is unconditional, so it stays on
after authentication. dnsmasq cannot vary this per client. Production either runs
two resolver views or drops the hijack and relies on the HTTP redirect alone.
Looks fine in the lab, confuses people in the pilot.

**IP-based authorization is wrong.** The stub authorizes an IP address, not a
person. DHCP renewal, sleep/wake, and MAC randomization all break it — and a
recycled IP transfers authorization to a different device. The session/binding
schema in the development guide is the fix, and it is the next thing to build.

**Uplink NAT.** Needs the firewalld configuration in runbook §6, plus reapplying
the zone assignment after each `up` cycle — or moving it into the script.

**Approval conversation.** Not yet started. It is the only phase with a
dependency on other people, so it has the longest lead time.
