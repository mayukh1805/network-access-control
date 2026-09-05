#!/usr/bin/env python3
"""
portal.py — throwaway captive portal for the Track A lab.

Exists to prove the loop end to end: redirect -> login -> nft set -> access.
Everything real (OIDC, sessions in Postgres, the policy engine, reconciliation)
replaces this. Do not grow it.

Runs inside the gw namespace. The client's source IP survives the nftables
redirect, so request.client_address is the address to authorize.
"""

import argparse
import html
import json
import subprocess
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# Demo roles. Replace with IdP group membership.
USERS = {
    "avi":   {"password": "demo", "printer": True},
    "guest": {"password": "demo", "printer": False},
}

LOGIN_PAGE = """<!doctype html>
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Network Login</title>
<style>
 body{{font:16px system-ui;margin:0;display:grid;place-items:center;height:100vh;background:#111;color:#eee}}
 form{{width:min(320px,90vw);display:grid;gap:12px}}
 input,button{{padding:12px;border-radius:8px;border:1px solid #444;background:#1c1c1c;color:#eee;font:inherit}}
 button{{background:#2a5;border:0;color:#000;font-weight:600}}
 .e{{color:#f66;font-size:14px}}
 .n{{font-size:12px;color:#888;line-height:1.5}}
</style>
<form method="post" action="/login">
  <h2>Network Login</h2>
  <div class="e">{error}</div>
  <input name="username" placeholder="username" autocapitalize="off" autofocus>
  <input name="password" type="password" placeholder="password">
  <button>Connect</button>
  <p class="n">Demo lab. Try <b>avi</b> (printer access) or
     <b>guest</b> (internet only). Password is <b>demo</b>.</p>
</form>
"""

SUCCESS_PAGE = """<!doctype html>
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Connected</title>
<style>body{font:16px system-ui;margin:0;display:grid;place-items:center;
 height:100vh;background:#111;color:#eee;text-align:center}</style>
<div><h2>Connected</h2><p>%s</p></div>
"""


def nft(*args) -> bool:
    """Run an nft command. Returns False on failure rather than raising."""
    try:
        subprocess.run(["nft", *args], check=True, capture_output=True)
        return True
    except (subprocess.CalledProcessError, FileNotFoundError) as exc:
        print(f"nft failed: {' '.join(args)} :: {exc}")
        return False


def authorize(ip: str, printer: bool) -> bool:
    ok = nft("add", "element", "inet", "nac", "authenticated", f"{{ {ip} timeout 1h }}")
    if printer:
        ok = nft("add", "element", "inet", "nac", "printer_allowed",
                 f"{{ {ip} timeout 1h }}") and ok
    return ok


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _send(self, code: int, body: str, ctype="text/html; charset=utf-8"):
        raw = body.encode()
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        path = urllib.parse.urlparse(self.path).path

        # RFC 8908 Captive Portal API. dnsmasq advertises this via DHCP
        # option 114, which is how modern clients discover the portal
        # without relying on HTTP probe heuristics.
        if path == "/api/captive":
            ip = self.client_address[0]
            captive = not is_authenticated(ip)
            self._send(200, json.dumps({
                "captive": captive,
                "user-portal-url": f"http://{self.headers.get('Host', '')}/",
            }), "application/captive+json")
            return

        self._send(200, LOGIN_PAGE.format(error=""))

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        form = urllib.parse.parse_qs(self.rfile.read(length).decode())
        user = form.get("username", [""])[0].strip()
        pw = form.get("password", [""])[0]
        ip = self.client_address[0]

        record = USERS.get(user)
        if not record or record["password"] != pw:
            print(f"DENY  {ip} user={user!r}")
            self._send(401, LOGIN_PAGE.format(error="Invalid credentials"))
            return

        if not authorize(ip, record["printer"]):
            self._send(500, LOGIN_PAGE.format(error="Could not apply policy"))
            return

        role = "internet + printer" if record["printer"] else "internet only"
        print(f"ALLOW {ip} user={user!r} role={role}")
        self._send(200, SUCCESS_PAGE % html.escape(f"{user} — {role}"))

    def log_message(self, *_):
        pass  # we do our own logging


def is_authenticated(ip: str) -> bool:
    try:
        out = subprocess.run(
            ["nft", "list", "set", "inet", "nac", "authenticated"],
            check=True, capture_output=True, text=True).stdout
        return ip in out
    except Exception:
        return False


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=8080)
    ap.add_argument("--bind", default="0.0.0.0")
    args = ap.parse_args()
    print(f"portal listening on {args.bind}:{args.port}")
    ThreadingHTTPServer((args.bind, args.port), Handler).serve_forever()
