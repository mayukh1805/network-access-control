#!/usr/bin/env bash
#
# lab.sh — Track A network namespace lab for the NAC project.
#
#   ./lab.sh up            build the topology
#   ./lab.sh down          tear everything down
#   ./lab.sh status        show namespaces, addresses, and the nft sets
#   ./lab.sh shell <ns>    drop into a namespace (gw|guest-a|guest-b|svc|printer)
#   ./lab.sh auth <ip>     add an IP to the authenticated set
#   ./lab.sh deauth <ip>   remove it
#   ./lab.sh grant <ip>    grant printer access (the authorization layer)
#   ./lab.sh revoke <ip>   revoke printer access
#   ./lab.sh test          run the assertions that should hold after `up`
#
# Requires: root, iproute2, nftables, dnsmasq, python3.
#
# UNTESTED — written against the documented behaviour of these tools, not
# verified on a live host. Expect to fix something on first run.

set -euo pipefail

# ---------------------------------------------------------------- config ----

GUEST_NET="10.77.10"     # quarantine + user segment
PRINTER_NET="10.77.30"   # protected resources
SVC_NET="10.77.40"       # postgres, redis, freeradius
UPLINK_NET="10.77.0"     # gw <-> root namespace

GUEST_IPV6="fd77:10::"     # guest segment
PRINTER_IPV6="fd77:30::"   # protected resources
SVC_IPV6="fd77:40::"       # services
UPLINK_IPV6="fd77:0::"     # gw <-> root namespace

PORTAL_PORT=8080
NAMESPACES=(gw guest-a guest-b svc printer)
RUNDIR=/run/nac-lab

# ------------------------------------------------------------- utilities ----

log()  { printf '\033[36m==>\033[0m %s\n' "$*"; }
audit_event() { logger -t nac "$*"; }
warn() { printf '\033[33m[!]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

gw()   { ip netns exec gw "$@"; }
nsx()  { local ns=$1; shift; ip netns exec "$ns" "$@"; }

require_root() {
  [[ $EUID -eq 0 ]] || die "must run as root (needs CAP_NET_ADMIN)"
}

require_tools() {
  local missing=()
  for t in ip nft dnsmasq python3; do
    command -v "$t" >/dev/null 2>&1 || missing+=("$t")
  done
  if (( ${#missing[@]} )); then
    die "missing: ${missing[*]}  —  apt install iproute2 nftables dnsmasq python3"
  fi
}

# ------------------------------------------------------------------ up ------

make_namespaces() {
  log "creating namespaces"
  for ns in "${NAMESPACES[@]}"; do
    ip netns add "$ns" 2>/dev/null || warn "namespace $ns already exists"
    nsx "$ns" ip link set lo up
    # IPv6 is enabled for dual-stack NAC enforcement.
  done
  gw sysctl -qw net.ipv4.ip_forward=1
  gw sysctl -qw net.ipv6.conf.all.forwarding=1

  # Namespaces share /etc/resolv.conf with the host unless given their own.
  # Without this, guests try to reach the host's upstream resolver, which is
  # not routable from 10.77.10.0/24.
  for g in guest-a guest-b; do
    mkdir -p "/etc/netns/${g}"
    echo "nameserver ${GUEST_NET}.1" > "/etc/netns/${g}/resolv.conf"
  done
}

# link <host-side-name> <namespace> <ns-side-name>
# Creates a veth pair with one end in gw and the other in the target namespace.
link() {
  local gwside=$1 ns=$2 nsside=$3
  ip link add "$gwside" type veth peer name "$nsside" netns "$ns"
  ip link set "$gwside" netns gw
  gw ip link set "$gwside" up
  nsx "$ns" ip link set "$nsside" up
}

make_topology() {
  log "wiring the guest segment"
  # A bridge, so both guests share a broadcast domain the way a real VLAN would.
  gw ip link add br-guest type bridge
  gw ip link set br-guest up
  gw ip addr add "${GUEST_NET}.1/24" dev br-guest
  gw ip -6 addr add "${GUEST_IPV6}1/64" dev br-guest

  for g in a b; do
    link "veth-g${g}" "guest-${g}" eth0
    gw ip link set "veth-g${g}" master br-guest
  done

  # Static addressing so the lab comes up deterministically. dnsmasq is running
  # too — run `dhclient eth0` inside a guest to exercise the DHCP path.
  nsx guest-a ip addr add "${GUEST_NET}.100/24" dev eth0
  nsx guest-a ip -6 addr add "${GUEST_IPV6}100/64" dev eth0
  nsx guest-a ip route add default via "${GUEST_NET}.1"
  nsx guest-a ip -6 route add default via "${GUEST_IPV6}1"
  nsx guest-b ip addr add "${GUEST_NET}.101/24" dev eth0
  nsx guest-b ip -6 addr add "${GUEST_IPV6}101/64" dev eth0
  nsx guest-b ip route add default via "${GUEST_NET}.1"
  nsx guest-b ip -6 route add default via "${GUEST_IPV6}1"

  log "wiring the protected segment"
  link vprn printer eth0
  gw ip addr add "${PRINTER_NET}.1/24" dev vprn
  gw ip -6 addr add "${PRINTER_IPV6}1/64" dev vprn
  nsx printer ip addr add "${PRINTER_NET}.10/24" dev eth0
  nsx printer ip -6 addr add "${PRINTER_IPV6}10/64" dev eth0
  nsx printer ip route add default via "${PRINTER_NET}.1"
  nsx printer ip -6 route add default via "${PRINTER_IPV6}1"

  log "wiring the services segment"
  link vsvc svc eth0
  gw ip addr add "${SVC_NET}.1/24" dev vsvc
  gw ip -6 addr add "${SVC_IPV6}1/64" dev vsvc
  nsx svc ip addr add "${SVC_NET}.10/24" dev eth0
  nsx svc ip -6 addr add "${SVC_IPV6}10/64" dev eth0
  nsx svc ip route add default via "${SVC_NET}.1"
  nsx svc ip -6 route add default via "${SVC_IPV6}1"

  log "wiring the uplink"
  ip link add nac-up0 type veth peer name up0 netns gw
  ip link set nac-up0 up
  ip addr add "${UPLINK_NET}.1/30" dev nac-up0
  ip -6 addr add "${UPLINK_IPV6}1/64" dev nac-up0
  gw ip link set up0 up
  gw ip addr add "${UPLINK_NET}.2/30" dev up0
  gw ip -6 addr add "${UPLINK_IPV6}2/64" dev up0
  gw ip route add default via "${UPLINK_NET}.1"
  gw ip -6 route add default via "${UPLINK_IPV6}1"
}

make_firewall() {
  log "loading nftables ruleset"
  gw nft -f - <<EOF
table inet nac {
  # Authenticated clients. Populated by the portal on successful login.
  set authenticated {
    type ipv4_addr
    flags timeout
  }

  # Clients whose role grants printer access. Deliberately separate from
  # authentication — this set is the authorization layer.
  set printer_allowed {
    type ipv4_addr
    flags timeout
  }
  set authenticated6 {
    type ipv6_addr
    flags timeout
  }

  set printer_allowed6 {
    type ipv6_addr
    flags timeout
  }

  chain prerouting {
    type nat hook prerouting priority dstnat; policy accept;

    # The captive portal redirect. Unauthenticated HTTP, including OS
    # detection probes, lands on the portal.
    iifname "br-guest" ip saddr != @authenticated tcp dport 80 redirect to :${PORTAL_PORT}
  }

  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    oifname "up0" masquerade
  }

  chain input {
    type filter hook input priority filter; policy drop;

    ct state established,related accept
    iif lo accept

    # Walled garden: DHCP, DNS, and the portal are always reachable.
    iifname "br-guest" udp dport { 53, 67 } accept
    iifname "br-guest" tcp dport { 53, ${PORTAL_PORT} } accept

    # ICMP to the gateway. Diagnostics need this, and a gateway that does not
    # answer ping is indistinguishable from a gateway that is down.
    iifname "br-guest" icmp type { echo-request, destination-unreachable, time-exceeded } accept
        # IPv6 control and diagnostic traffic.
    iifname "br-guest" icmpv6 type {
      nd-neighbor-solicit,
      nd-neighbor-advert,
      nd-router-solicit,
      nd-router-advert,
      echo-request,
      destination-unreachable,
      time-exceeded
    } accept
    # Services and printer segments talk to the gateway freely.
    iifname { "vsvc", "vprn" } accept

    iifname "up0" ct state established,related accept
  }

  chain forward {
    type filter hook forward priority filter; policy drop;

    ct state established,related accept

    # Authenticated clients reach the uplink.
    iifname "br-guest" ip saddr @authenticated oifname "up0" accept

    # Authenticated IPv6 clients reach the uplink.
    iifname "br-guest" ip6 saddr @authenticated6 oifname "up0" accept

    # Authorized clients reach the printer. Note both sets are required:
    # authentication alone does not grant this.
    iifname "br-guest" ip saddr @printer_allowed oifname "vprn" accept

    # Authorized IPv6 clients reach the printer.
    iifname "br-guest" ip6 saddr @printer_allowed6 oifname "vprn" accept

    # Everything else, including guest-to-guest, is dropped.
    # (Guest isolation is enforced by the bridge hairpin block below.)
  }
}
EOF

  # Client isolation: stop guests seeing each other at Layer 2. Without this,
  # an unauthenticated client can ARP-spoof an authenticated one.
  gw nft -f - <<'EOF'
table bridge nac_iso {
  chain forward {
    type filter hook forward priority filter; policy accept;
    iifname "veth-ga" oifname "veth-gb" drop
    iifname "veth-gb" oifname "veth-ga" drop
  }
}
EOF
}

start_services() {
  mkdir -p "$RUNDIR"

  log "starting dnsmasq (DHCP + DNS on the guest segment)"
  gw dnsmasq \
    --interface=br-guest \
    --except-interface=lo \
    --except-interface=up0 \
    --bind-interfaces \
    --dhcp-range="${GUEST_NET}.100,${GUEST_NET}.200,1h" \
    --dhcp-option=option:router,"${GUEST_NET}.1" \
    --dhcp-option=option:dns-server,"${GUEST_NET}.1" \
    --dhcp-option=114,"http://${GUEST_NET}.1:${PORTAL_PORT}/api/captive" \
    --address=/#/"${GUEST_NET}.1" \
    --pid-file="${RUNDIR}/dnsmasq.pid" \
    --log-facility="${RUNDIR}/dnsmasq.log"

  if [[ -f ./portal.py ]]; then
    log "starting portal stub on ${GUEST_NET}.1:${PORTAL_PORT}"
    gw python3 ./portal.py --port "$PORTAL_PORT" \
      >"${RUNDIR}/portal.log" 2>&1 &
    echo $! > "${RUNDIR}/portal.pid"
    sleep 0.5
  else
    warn "portal.py not found — skipping portal. Authorize manually: ./lab.sh auth <ip>"
  fi
}

cmd_up() {
  require_root; require_tools
  ip netns list 2>/dev/null | grep -q '^gw' && die "lab already up — run './lab.sh down' first"
  make_namespaces
  make_topology
  make_firewall
  start_services
  log "lab is up"
  echo
  cmd_status
}

# ---------------------------------------------------------------- down ------

cmd_down() {
  require_root
  log "stopping services"
  for p in portal dnsmasq; do
    if [[ -f "${RUNDIR}/${p}.pid" ]]; then
      kill "$(cat "${RUNDIR}/${p}.pid")" 2>/dev/null || true
      rm -f "${RUNDIR}/${p}.pid"
    fi
  done

  log "removing namespaces"
  # Deleting a namespace destroys the veths inside it automatically.
  for ns in "${NAMESPACES[@]}"; do
    ip netns del "$ns" 2>/dev/null || true
    rm -rf "/etc/netns/${ns}"
  done
  ip link del nac-up0 2>/dev/null || true

  log "lab is down"
}

# -------------------------------------------------------------- inspect -----

cmd_status() {
  require_root
  if ! ip netns list 2>/dev/null | grep -q '^gw'; then
    echo "lab is down"
    return
  fi
  echo "namespaces:"
  for ns in "${NAMESPACES[@]}"; do
    printf '  %-10s %s\n' "$ns" \
      "$(nsx "$ns" ip -4 -br addr show 2>/dev/null | grep -v '^lo' | tr '\n' ' ')"
  done
  echo
  echo "authenticated:"
  gw nft list set inet nac authenticated 2>/dev/null | sed -n 's/.*elements = {\(.*\)}.*/  \1/p' || echo "  (empty)"
  echo "printer_allowed:"
  gw nft list set inet nac printer_allowed 2>/dev/null | sed -n 's/.*elements = {\(.*\)}.*/  \1/p' || echo "  (empty)"
}

cmd_shell() {
  require_root
  local ns=${1:-}
  [[ -n $ns ]] || die "usage: ./lab.sh shell <gw|guest-a|guest-b|svc|printer>"
  log "entering $ns — exit to return"
  nsx "$ns" "${SHELL:-/bin/bash}"
}

# ---------------------------------------------------------- set control -----

cmd_auth() {
  require_root
  if [[ "$1" == *:* ]]; then
    gw nft add element inet nac authenticated6 "{ $1 timeout 1h }"
  else
    gw nft add element inet nac authenticated "{ $1 timeout 1h }"
  fi
  log "authenticated $1"
}
cmd_deauth() {
  require_root
  if [[ "$1" == *:* ]]; then
    gw nft delete element inet nac authenticated6 "{ $1 }"
  else
    gw nft delete element inet nac authenticated "{ $1 }"
  fi
  log "deauthenticated $1"
}
cmd_grant() {
  require_root
  if [[ "$1" == *:* ]]; then
    gw nft add element inet nac printer_allowed6 "{ $1 timeout 1h }"
  else
    gw nft add element inet nac printer_allowed "{ $1 timeout 1h }"
  fi
  log "printer access granted to $1"
}
cmd_revoke() {
  require_root
  if [[ "$1" == *:* ]]; then
    gw nft delete element inet nac printer_allowed6 "{ $1 }"
  else
    gw nft delete element inet nac printer_allowed "{ $1 }"
  fi
  log "printer access revoked from $1"
  audit_event "NAC AUTHORIZATION_REVOKED ip=$1 resource=printer"
}

# ----------------------------------------------------------------- test -----

cmd_test() {
  require_root
  local pass=0 fail=0

    # Reset dynamic authorization state so every test starts clean.
  gw nft flush set inet nac authenticated 2>/dev/null || true
  gw nft flush set inet nac printer_allowed 2>/dev/null || true

  check() {
    local desc=$1; shift
    # Note: pass=$((pass+1)), not ((pass++)). Post-increment returns the OLD
    # value, so ((x++)) exits 1 when x is 0 — which aborts the script under
    # set -e on the very first assertion.
    if "$@" >/dev/null 2>&1; then
      printf '  \033[32mPASS\033[0m %s\n' "$desc"; pass=$((pass+1))
    else
      printf '  \033[31mFAIL\033[0m %s\n' "$desc"; fail=$((fail+1))
    fi
  }
  check_fails() {
    local desc=$1; shift
    if "$@" >/dev/null 2>&1; then
      printf '  \033[31mFAIL\033[0m %s (succeeded, should not have)\n' "$desc"; fail=$((fail+1))
    else
      printf '  \033[32mPASS\033[0m %s\n' "$desc"; pass=$((pass+1))
    fi
  }

  echo "baseline — guest-a is unauthenticated:"
  check       "reaches the gateway (walled garden)" \
              nsx guest-a ping -c1 -W1 "${GUEST_NET}.1"
  check_fails "cannot reach the printer" \
              nsx guest-a ping -c1 -W1 "${PRINTER_NET}.10"
  check_fails "cannot reach guest-b (client isolation)" \
              nsx guest-a ping -c1 -W1 "${GUEST_NET}.101"

  echo
  echo "after authentication only:"
  cmd_auth "${GUEST_NET}.100" >/dev/null
  check_fails "still cannot reach the printer (authz is separate)" \
              nsx guest-a ping -c1 -W1 "${PRINTER_NET}.10"

  echo
  echo "after authorization:"
  cmd_grant "${GUEST_NET}.100" >/dev/null
  check       "reaches the printer" \
              nsx guest-a ping -c1 -W1 "${PRINTER_NET}.10"

  echo
  echo "revocation takes effect immediately:"
  cmd_revoke "${GUEST_NET}.100" >/dev/null
  check_fails "printer unreachable again" \
              nsx guest-a ping -c1 -W1 "${PRINTER_NET}.10"
  cmd_deauth "${GUEST_NET}.100" >/dev/null

  echo
  printf '%d passed, %d failed\n' "$pass" "$fail"
  (( fail == 0 ))
}

# ----------------------------------------------------------------- main -----

case "${1:-}" in
  up)     cmd_up ;;
  down)   cmd_down ;;
  status) cmd_status ;;
  shell)  cmd_shell "${2:-}" ;;
  auth)   cmd_auth "${2:?usage: ./lab.sh auth <ip>}" ;;
  deauth) cmd_deauth "${2:?usage: ./lab.sh deauth <ip>}" ;;
  grant)  cmd_grant "${2:?usage: ./lab.sh grant <ip>}" ;;
  revoke) cmd_revoke "${2:?usage: ./lab.sh revoke <ip>}" ;;
  test)   cmd_test ;;
  *)      sed -n '3,20p' "$0" | sed 's/^# \{0,1\}//' ;;
esac
