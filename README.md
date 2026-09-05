# Network Access Control (NAC) Lab

A virtualized Network Access Control (NAC) security project built on RHEL using Linux network namespaces, virtual Ethernet (veth) links, a Linux bridge, nftables, dnsmasq, and a captive-portal prototype.

The project demonstrates network segmentation, guest isolation, authentication, authorization, access control, and security testing.

## Architecture

The entire lab runs inside a single RHEL virtual machine under VMware Workstation.

| Namespace | Purpose | Address |
|---|---|---|
| `gw` | NAC gateway / firewall | `10.77.10.1`, `10.77.30.1`, `10.77.40.1`, `10.77.0.2` |
| `guest-a` | Simulated guest client | `10.77.10.100` |
| `guest-b` | Simulated guest client | `10.77.10.101` |
| `svc` | Simulated services endpoint | `10.77.40.10` |
| `printer` | Protected printer endpoint | `10.77.30.10` |

Networks:

- Guest: `10.77.10.0/24`
- Protected/printer: `10.77.30.0/24`
- Services: `10.77.40.0/24`
- Uplink: `10.77.0.0/30`

IPv6 is explicitly disabled in the current lab.

## Current Security Controls

### Network Segmentation

Guest, protected, services, and uplink networks are separated into different network segments.

### Default-Deny Firewall

The gateway uses nftables with a default-drop forwarding policy.

Traffic is allowed only when an explicit firewall rule permits it.

### Guest Isolation

`guest-a` and `guest-b` cannot communicate directly with each other, demonstrating protection against basic guest-to-guest lateral movement.

### Captive Portal

Unauthenticated guest HTTP traffic is redirected to a captive-portal prototype.

dnsmasq provides DHCP/DNS services and advertises the captive portal.

### Authentication and Authorization

The prototype has two demonstration users:

- `avi` — printer access
- `guest` — internet access only

Authentication and authorization are deliberately separate.

The firewall maintains separate nftables sets:

- `authenticated`
- `printer_allowed`

Therefore, authentication alone does not grant access to the protected printer.

## Validation

The automated lab tests currently report:

```text
6 passed, 0 failed

The tests verify:

Gateway reachability
Printer access blocked before authentication
Guest-to-guest isolation
Authentication alone does not grant printer access
Explicit printer authorization allows access
Revocation blocks printer access again

The complete authorization lifecycle was also manually validated:

DENIED
  ↓
AUTHENTICATED
  ↓
Still denied from printer
  ↓
AUTHORIZED
  ↓
Printer access allowed
  ↓
REVOKED
  ↓
DENIED
Security Testing

Nmap was used to examine the guest-visible attack surface and verify guest isolation.

tcpdump was used to observe denied printer traffic at the guest-side bridge.

nftables rules and counters were inspected to verify that the firewall was enforcing the denial.

Security Logging

The RHEL environment provides an application-to-system logging pipeline:

NAC Application
      ↓
syslog / logger
      ↓
rsyslog
      ↓
/var/log/messages

The final NAC implementation will generate structured events such as:

LOGIN_SUCCESS
LOGIN_FAILURE
ACCESS_GRANTED
ACCESS_DENIED
AUTHORIZATION_REVOKED

The /var/log/nac directory has also been created for future NAC-specific logging.

Current Files
File	Purpose
lab.sh	Builds, tests, and tears down the virtual NAC lab
portal.py	Temporary captive-portal authentication prototype
README.md	Project documentation
nac-project-plan.md	Project roadmap and architecture
nac-build-runbook.md	Build and execution procedures
nac-development-guide.md	Development guidance
nac-decision-log.md	Design decisions, bugs, fixes, and lessons
Known Limitations

The current implementation is a functional security lab, not a production-ready NAC system.

Current limitations include:

Hard-coded demonstration credentials
IP-based authorization
Runtime firewall state is not yet persistent
No production identity provider
No production HTTPS deployment
No complete session-management system
Uplink/NAT remains an implementation gap
IPv6 is explicitly disabled

These limitations are intentional and will be addressed during later development stages.

Planned Development

The project will progressively evolve toward:

Persistent Database
        ↓
Session + Device Model
        ↓
Go NAC Daemon
        ↓
Policy Engine + RBAC
        ↓
Firewall Reconciliation
        ↓
OIDC Identity Integration
        ↓
Device Posture + Quarantine
        ↓
IPP Printer Authorization
        ↓
Security Monitoring + API
        ↓
Dashboard
        ↓
802.1X / RADIUS Extension

The development approach is:

Learn → Build → Test → Break → Fix → Document → Commit

The current foundation is complete and validated. The next major stage is introducing the persistent backend and session model.
