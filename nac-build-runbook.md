# NAC Project — Build Runbook

Step-by-step setup for the Track A lab on RHEL under VMware, plus sizing for all
three tracks.

Companion to `nac-project-plan.md` (strategy and timeline). This document is the
hands-on part: what to build, in what order, with exact commands.

---

## 1. Sizing

### 1.1 Track A — lab VM

Host assumed: Windows laptop, Intel i5 11th gen, 8 GB RAM.

| Spec | Minimum | Recommended | Notes |
|---|---|---|---|
| vCPU | 2 | 2 | Workload is I/O and kernel-bound, not compute-bound. More cores buy nothing. |
| RAM | 2 GB | **3 GB** | RHEL minimal idles higher than Debian — budget for it. |
| Disk | 20 GB | **30 GB**, thin-provisioned | Actual consumption lands around 5–6 GB. |
| NICs | 1 (NAT / VMnet8) | 1 | See §1.2 — you do not need multiple VMnets. |
| Firmware | BIOS or UEFI | either | No preference. |

**Where the 3 GB goes:**

| Component | Approx RSS |
|---|---|
| RHEL 9 minimal, idle | 400–500 MB |
| PostgreSQL (default config) | 120 MB |
| Redis | 40 MB |
| FreeRADIUS | 60 MB |
| dnsmasq | 10 MB |
| Gateway daemon + portal | 70 MB |
| Open vSwitch (from week 5) | 60 MB |
| **Total services** | **~800 MB** |
| Page cache + headroom | remainder |

Host budget: Windows ~3.5 GB + VM 3 GB = **6.5 GB of 8 GB**. Workable, provided
you aren't running a heavy browser session alongside it. Drop the VM to 2 GB if
things get tight; the lab still fits.

### 1.2 Why one NIC, not four

An earlier design had six VMs across VMnet2/3/4. That is **not** what you are
building. With network namespaces, the entire guest / protected / services /
uplink topology lives inside one VM's kernel. The VM's single NAT interface
exists only so the VM itself can reach the internet for `dnf`.

A namespace is a kernel data structure, not a machine. The whole lab costs under
1 GB.

### 1.3 Track B — pilot hardware

| Item | Spec | Sizing rationale |
|---|---|---|
| Gateway | Intel N100 mini PC, 8 GB RAM, 128 GB SSD, 2× 1GbE | Runs everything the VM ran, plus real traffic for ~50 devices. 16 GB if you want headroom for later. |
| Managed switch | 8-port, 802.1Q + RADIUS (TL-SG2008P / GS308T) | 8 ports covers 4 clients, 2 APs, printer, uplink with none spare — take 16-port if budget allows. |
| APs | 2× multi-SSID, WPA2-Enterprise capable | One AP covers 4 people. The second is for roaming tests. |
| UPS | ~600 VA line-interactive | Gateway only. Enough for clean shutdown, not for riding out an outage. |
| Test printer | Raspberry Pi 4 (2 GB) + CUPS | Never the office printer. |

Two NICs on the gateway is the non-negotiable spec — one uplink, one trunk
downstream. A single-NIC box forces bridging, which breaks the isolation
guarantee the whole pilot rests on.

### 1.4 Track C — production sizing

Sized per 1,000 managed devices.

| Node | Count | Spec |
|---|---|---|
| Policy / portal | 2 (active-active behind VIP) | 4 vCPU, 8 GB |
| RADIUS | 2 (clients fail over natively) | 2 vCPU, 4 GB |
| PostgreSQL | 1 primary + 1 streaming replica | 4 vCPU, 16 GB, SSD |
| Redis | 3-node Sentinel | 2 vCPU, 4 GB |

**Be honest about the load.** 1,000 devices produces roughly:

- **Auth events:** ~1,000 in the morning arrival window, plus periodic reauth.
  Under 1/sec sustained.
- **RADIUS accounting:** interim updates every 5 minutes → ~200 writes/min.
- **Audit log growth:** ~10 events per session per day → ~3.6M rows/year,
  1–2 GB/year with indexes. At CERT-In's 180-day retention, under 1 GB live.

None of that is demanding. **Compute is not the constraint at this scale —
availability and correctness are.** Do not oversize the boxes; spend the effort
on failover testing instead. The reason for two of everything is that a NAC
outage means nobody can work, not that one node can't handle the throughput.

---

## 2. Create the VM

VMware Workstation Pro (free, no licence key required since Nov 2024).

**New VM wizard:** choose *"I will install the operating system later"* — this
skips Easy Install, which would create a user and packages you don't want.

Settings before first boot:

| Setting | Value |
|---|---|
| Guest OS | Linux → Red Hat Enterprise Linux 9 64-bit |
| Memory | 3072 MB |
| Processors | 2 cores, 1 socket |
| Disk | 30 GB, **split into multiple files**, **not** preallocated |
| Controller | SCSI (default) |
| Network | NAT (VMnet8) |
| Sound / printer / USB / camera | **Remove** |

Then **VM Settings → Processors → tick "Disable side channel mitigations."**
Measurable performance gain on 11th-gen silicon, and an acceptable tradeoff for
a disposable lab VM.

Leave *Virtualize Intel VT-x/EPT* unchecked — you are not nesting hypervisors.

---

## 3. Prepare RHEL

### 3.1 If you already have a RHEL VM

Check three things before reusing it:

```bash
systemctl get-default          # want: multi-user.target
free -m                        # a GUI install idles ~1.5 GB
subscription-manager status    # repos must be entitled or dnf fails
```

If it says `graphical.target`:

```bash
sudo systemctl set-default multi-user.target && sudo reboot
```

No subscription? The Red Hat Developer subscription is free for individual use
and entitles the repos you need.

```bash
sudo subscription-manager register --username <your-rh-login>
dnf repolist        # expect BaseOS and AppStream
```

On RHEL 9, Simple Content Access is on by default, so registration alone
entitles you — `attach --auto` is usually unnecessary.

**Offline alternative — local repo from the install ISO.** Every package needed
is on the install media. Re-attach the ISO in VM Settings → CD/DVD (tick
*Connected*), then:

```bash
sudo mkdir -p /mnt/iso && sudo mount /dev/sr0 /mnt/iso
```

Create `/etc/yum.repos.d/local.repo`:

```ini
[BaseOS]
name=RHEL BaseOS (ISO)
baseurl=file:///mnt/iso/BaseOS
enabled=1
gpgcheck=0

[AppStream]
name=RHEL AppStream (ISO)
baseurl=file:///mnt/iso/AppStream
enabled=1
gpgcheck=0
```

Unblocks you tonight, but pins you to the ISO's package versions. Register when
you can.

**Take a VMware snapshot now**, before anything below. Namespaces are isolated,
but the root-namespace veth and any firewalld changes are not.

### 3.2 Get off the console before anything else

VMware clipboard sync runs through `vmtoolsd` and only works into a *graphical*
session. After switching to `multi-user.target` there is no paste in the console.
Do not fight this — SSH in and paste works normally.

```bash
ip -4 -br addr show          # note the IP
systemctl enable --now sshd
```

**RHEL 9 sets `PermitRootLogin prohibit-password`**, so root cannot log in with a
password. Create a regular user rather than working around it:

```bash
useradd -m -G wheel avi
passwd avi
```

`wheel` gives sudo, which is all the script needs — everything runs under `sudo`
anyway. Verify SSH works *while you still have the console as a fallback*.

Then VS Code on Windows with the Remote-SSH extension. Do **not** use VMware
shared folders (`hgfs`) — slow, and the permission behaviour will make you think
your script is broken.

### 3.2 Packages

```bash
sudo dnf install -y iproute nftables dnsmasq python3 tcpdump \
                    open-vm-tools git

# the lab runs its own dnsmasq instance
sudo systemctl disable --now dnsmasq

# clean shutdown + time sync from the host
sudo systemctl enable --now vmtoolsd
```

Note `iproute`, not `iproute2` — the package is named differently on RHEL.
chrony is already installed and enabled; leave it. **Clock skew will break OIDC
and TLS in week 3 with errors that look like anything but a clock problem**, so
confirm `chronyc tracking` is sane after any VM suspend.

### 3.3 Check the nft version

```bash
nft --version
```

RHEL 9 ships nft 1.0.x — fine. RHEL 8 ships 0.9.x, which is fussier. If the
ruleset fails to load, the `redirect to :8080` line is the first suspect.

---

## 4. Get the files in

SSH is already running. From Windows PowerShell:

```powershell
scp lab.sh portal.py user@<vm-ip>:~/nac/
```

Find the VM's IP with `ip -4 -br addr show`.

**Set line endings once, before editing anything from Windows:**

```bash
git config --global core.autocrlf input
```

A file that picks up CRLF fails with `\r: command not found`. Fix with
`sed -i 's/\r$//' lab.sh`.

**Editing workflow:** VS Code on Windows with the Remote-SSH extension, pointed
at the VM. Files live in the VM, editor runs on Windows, terminal is the VM's
shell. Do **not** use VMware shared folders (`hgfs`) — slow, and the permission
behaviour will make you think your script is broken.

---

## 5. SELinux

Namespaces are unaffected, but two things in the script will likely trip policy:
dnsmasq writing its pid and log into a custom directory, and `portal.py` shelling
out to `nft`.

**Diagnose before working around it:**

```bash
sudo setenforce 0
sudo ./lab.sh up
sudo ausearch -m AVC -ts recent
```

If it works permissive and fails enforcing, you have your answer.

**Cleaner fix for the dnsmasq denials** — edit `RUNDIR` in `lab.sh` to
`/var/run/nac-lab`, then label it:

```bash
sudo mkdir -p /var/run/nac-lab
sudo semanage fcontext -a -t dnsmasq_var_run_t "/var/run/nac-lab(/.*)?"
sudo restorecon -Rv /var/run/nac-lab
sudo setenforce 1
```

Permissive mode is a defensible choice for a disposable lab VM. **Do not carry
that habit into Track B**, where the gateway is real infrastructure.

---

## 6. Patch the uplink NAT gap

`lab.sh` as written masquerades on the gateway's `up0`, so packets arrive in the
root namespace with source `10.77.0.2` — and stop, because nothing forwards them
onward. Guest traffic reaches the gateway and dies there.

Fine for weeks 1–4 (a local nginx as "the internet" is the recommended approach
anyway). If you want real upstream, on RHEL do it through firewalld:

```bash
sudo firewall-cmd --permanent --zone=trusted --add-interface=nac-up0
sudo firewall-cmd --permanent --zone=public --add-masquerade
sudo firewall-cmd --reload
sudo sysctl -w net.ipv4.ip_forward=1
sudo ip route add 10.77.0.0/16 via 10.77.0.2 dev nac-up0
```

`nac-up0` is created by `lab.sh up` and destroyed by `down`, so the zone
assignment and route need reapplying each cycle — or move both into the script's
`make_topology`.

**firewalld does not otherwise interfere.** Each namespace has its own netfilter
instance, so firewalld (which manages only the root namespace) never sees the
`inet nac` table inside `gw`.

---

## 7. Run it

```bash
cd ~/nac
chmod +x lab.sh portal.py
sudo ./lab.sh up
sudo ./lab.sh status
```

Expected: five namespaces, addresses assigned, both nft sets empty.

### 7.1 Verify

```bash
sudo ./lab.sh test
```

The assertion that matters most:

```
PASS  still cannot reach the printer (authz is separate)
```

That is the authorization layer proving it is a separate decision from
authentication. Keep this assertion as you replace the stub — it is the property
most likely to quietly break when the policy engine gets real.

### 7.2 Watch the redirect fire

```bash
sudo ./lab.sh shell guest-a
curl -v http://example.com     # response comes from the portal, not example.com
exit
```

### 7.3 Manual control

```bash
sudo ./lab.sh auth   10.77.10.100   # authenticate
sudo ./lab.sh grant  10.77.10.100   # authorize for printer
sudo ./lab.sh revoke 10.77.10.100
sudo ./lab.sh deauth 10.77.10.100
```

---

## 8. Snapshots

The most valuable VMware feature for this project.

| Snapshot | When |
|---|---|
| `clean` | After §3, before any project files |
| `lab-working` | After `./lab.sh test` passes |
| `deps` | After Postgres / Redis / FreeRADIUS install (week 3) |

Take one before every risky change. Reverting beats rebuilding at 1am, and
they're near-instant on a thin-provisioned disk.

---

## 9. Demo capture

For showing progress. Build the set around **failure then success** — a
screenshot of something working is weak evidence; the same thing failing, then
succeeding, proves enforcement is real.

**The single most valuable shot** — split panes:

```bash
# left
sudo ./lab.sh shell guest-a
# right
watch -n1 'sudo ip netns exec gw nft list set inet nac authenticated'
```

Run `curl -v http://example.com` on the left, log in, run it again. One image
shows the client experience *and* the kernel state changing underneath.

**The four before/after pairs:**

1. Unauthenticated `curl` → lands on the portal
2. After login → reaches the internet
3. Authenticated but **not** authorized → printer still unreachable
4. After `revoke` → printer unreachable again, immediately

Number 3 is the one to linger on. Most captive-portal demos cannot show it.

**Also capture:** `./lab.sh test` output (written to be screenshotted); the
topology diagram for context; guest-a unable to ping guest-b (client isolation);
gateway restarted mid-session with sessions surviving; an audit log line showing
a denial with an identity attached.

**Leave out:** config files and raw nft rules. They belong in the plan document,
not in a deck — a wall of syntax makes the reader feel excluded rather than
impressed.

**Practicalities:** bump terminal font to ~16pt, clear scrollback before each
shot. Prefer a 20-second screen recording for the auth loop specifically, since
the redirect is temporal and stills carry it badly.

---

## 10. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `dnf`: no enabled repositories | RHEL not registered | §3.1 — register, or use the ISO repo |
| No paste in console after `multi-user.target` | Clipboard sync needs a graphical session | §3.2 — SSH in instead |
| SSH as root: permission denied | RHEL 9 `PermitRootLogin prohibit-password` | §3.2 — create a `wheel` user |
| `./lab.sh: Permission denied` | `scp` didn't preserve the exec bit | `chmod +x lab.sh portal.py` |
| `\r: command not found` | CRLF line endings | `sed -i 's/\r$//' lab.sh` |
| Guest can't ping the gateway | ICMP missing from the walled garden | Fixed in current `lab.sh` |
| `test` aborts after one line | `((x++))` returns 1 when x is 0, `set -e` kills it | Fixed — uses `x=$((x+1))` |
| `could not resolve host` in a guest | Namespaces share `/etc/resolv.conf` with the host | Fixed — per-namespace resolv.conf |
| `down` reports success but `up` says already up | Errors were swallowed by `\|\| true` | Fixed — `down` now reports and exits non-zero |
| `ip netns del`: device or resource busy | Orphaned `ip netns exec` holding the mount | `pkill -f 'ip netns exec'`, then §10.1 |
| `umount -l`: not mounted, `rm`: busy | Mount pinned in another mount namespace | **Reboot.** §10.1 |
| dnsmasq won't start | System instance already bound | `sudo systemctl stop dnsmasq` |
| Permission denied writing pid/log | SELinux | §5 |
| guest-a can ping guest-b | Bridge table didn't load | `sudo ip netns exec gw nft list ruleset` — confirm `table bridge nac_iso` |
| Guests reach gateway but not internet | Uplink NAT gap | §6 |
| OIDC / TLS failures after VM resume | Clock skew | `chronyc makestep` |

### 10.1 Stuck namespaces

`ip netns del` reporting EBUSY means the bind mount at `/run/netns/<ns>` is
pinned — usually by an `ip netns exec` process that outlived its terminal.

```bash
sudo ip netns pids gw
sudo pkill -f 'ip netns exec'
sudo ip netns del gw
```

If `umount -l` says *not mounted* and `rm -f` says *busy*, the mount lives in
another mount namespace and propagation is blocking the unlink. **Reboot the
VM.** Namespaces do not survive a reboot anyway, so nothing is lost, and the
alternative is twenty minutes of `/proc/*/mountinfo` archaeology to recover
thirty seconds.

**Prevention** — add to `make_namespaces`:

```bash
mkdir -p /run/netns
mount --bind /run/netns /run/netns 2>/dev/null || true
mount --make-private /run/netns 2>/dev/null || true
```

The bind-to-itself looks odd but is required: `--make-private` only operates on
an actual mount point, and `/run/netns` is otherwise just a directory on tmpfs.

**Habit:** leave `./lab.sh shell` with `exit`, never by closing the terminal.
That is what creates the orphan.

### 10.2 Note on the bridge table

It needs `nf_tables_bridge`, which loads automatically when the table is created.
It does **not** need `br_netfilter` — that module passes bridged traffic up to
the ip/inet families, which is a different thing. If isolation fails, verify the
table loaded rather than chasing modules.

---

## 11. Teardown

```bash
sudo ./lab.sh down
```

Namespaces are runtime kernel state, so **nothing survives a reboot** — after
every VM boot, run `./lab.sh up` again. That is by design; instant rebuild is the
whole point of the namespace approach.

**VMware suspend is fine and preferred.** It preserves the running lab exactly,
including namespaces and firewall state, so you can close the laptop mid-debug.
Let chrony correct the clock after resume before testing anything time-sensitive.

---

## 12. Host-side memory reclaim

Worth doing once on an 8 GB machine:

- **Core Isolation / Memory Integrity → off.** Windows Security → Device
  security. Costs a few hundred MB and hurts virtualization performance.
- **Hyper-V and WSL2 → off** if unused. VMware coexists with Hyper-V now, but
  pays a real performance tax for it.

---

## Appendix — what carries forward to Track B

Nothing about the Windows host does. Track B's gateway is a mini PC running
Linux on bare metal in a cabinet with a UPS. The laptop goes back to being where
you write code.

What *does* carry forward is the code, provided the six locked decisions in the
plan document held: enforcement behind an interface, Postgres as source of truth,
policy engine as a pure function, identity from an IdP, IPv6 explicitly handled,
audit log append-only from the first commit.
