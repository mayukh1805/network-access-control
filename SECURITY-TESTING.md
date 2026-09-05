# Security Testing

This document records the main security validation performed on the current NAC lab.

## Automated Tests

The built-in test suite completed successfully:

```text
6 passed, 0 failed

The tests verify:

Gateway reachability
Printer access is blocked before authentication
Guest-to-guest communication is blocked
Authentication alone does not grant printer access
Explicit printer authorization allows access
Revoking authorization blocks printer access again


# Authentication vs Authorization:

The following lifecycle was manually tested:

Unauthenticated
      ↓
Printer access denied
      ↓
Authenticated
      ↓
Printer access still denied
      ↓
Printer authorization granted
      ↓
Printer access allowed
      ↓
Authorization revoked
      ↓
Printer access denied

This confirms that authentication and authorization are separate security decisions.

#Guest Isolation:

Nmap discovery from guest-a was used to verify the guest network.

The gateway was visible, while guest-b was not discoverable from guest-a.

This supports the guest-to-guest isolation control.

#Firewall Testing:

The nftables forwarding chain was inspected to verify:

Default-drop forwarding policy
Established/related traffic handling
Authenticated guest access toward the uplink
Authorized guest access toward the protected printer

The firewall therefore acts as the enforcement point for the current NAC policy.

#Packet Capture:

tcpdump was used to observe traffic during a denied printer-access attempt.

The ICMP requests from guest-a were visible on the guest-side bridge, but no successful printer response was received.

This demonstrates that the traffic reaches the network enforcement path and is subsequently blocked by policy.

#Network Service Discovery

Nmap service detection identified the services intentionally exposed by the gateway, including:

DNS provided by dnsmasq
HTTP captive-portal service
Captive-portal API endpoint

This provides a basic view of the current guest-visible attack surface.

#Logging Validation

The RHEL logging pipeline was tested separately using a controlled log message.

The message successfully reached /var/log/messages through the system logging pipeline.

The final NAC implementation will replace this basic validation with structured application-level security events.

#Result

The current NAC foundation successfully demonstrates:

Segmentation
Default-deny enforcement
Guest isolation
Authentication
Separate authorization
Access granting
Access revocation
Network-level security validation
Basic security logging
