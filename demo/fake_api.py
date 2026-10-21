#!/usr/bin/env python3
"""A fake Tailscale API with invented machines, for the ts-cli demo video.

Nothing here is real: names, addresses and keys are made up. It serves the
endpoints ts-cli uses (device list, key info, device actions) on a local port
and keeps changes (rename, tags, key expiry) in memory so the list reflects
them after a reload.

    python3 demo/fake_api.py PORT_FILE      # writes the chosen port to PORT_FILE
"""
import json
import re
import sys
import threading
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# tailnet -> API key
KEYS = {
    "acme.example": "tskey-api-kDEMOWORKCNTRL-00000000000000000000000000000000",
    "home.example": "tskey-api-kDEMOHOMECNTRL-00000000000000000000000000000000",
}

NOW = datetime.now(timezone.utc)


def ts(delta):
    return (NOW + delta).strftime("%Y-%m-%dT%H:%M:%SZ")


def dev(i, name, host, os_, ip, online, tags=(), expires_days=120, no_expiry=False, version="1.76.6", created_days=300, seen_min=0):
    d = {
        "id": f"n{i:04d}DEMO",
        "name": f"{name}.tail1a2b3c.ts.net",
        "hostname": host,
        "clientVersion": version,
        "os": os_,
        "created": ts(-timedelta(days=created_days)),
        "authorized": True,
        "keyExpiryDisabled": no_expiry,
        "addresses": [ip],
        "tags": list(tags),
        "updateAvailable": False,
        "isExternal": False,
        "blocksIncomingConnections": False,
    }
    if online:
        d["connectedToControl"] = True
    else:
        d["connectedToControl"] = False
        d["lastSeen"] = ts(-timedelta(minutes=seen_min))
    if not no_expiry:
        d["expires"] = ts(timedelta(days=expires_days))
    return d


DEVICES = {
    "acme.example": [
        dev(1, "web-01", "web-01", "linux", "100.64.10.11", True, ["tag:server", "tag:web"], 160),
        dev(2, "web-02", "web-02", "linux", "100.64.10.12", True, ["tag:server", "tag:web"], 160),
        dev(3, "web-03", "web-03", "linux", "100.64.10.13", True, ["tag:server", "tag:web"], 160),
        dev(4, "db-01", "db-01", "linux", "100.64.10.21", True, ["tag:server", "tag:db"], 90, no_expiry=True),
        dev(5, "build-mac", "build-mac", "macOS", "100.64.10.31", True, ["tag:ci"], 5),
        dev(6, "staging-01", "staging-01", "linux", "100.64.10.41", False, ["tag:server"], 100, seen_min=95),
        dev(7, "cam-pi", "cam-pi", "linux", "100.64.10.51", False, [], 100, seen_min=60 * 30),
        dev(8, "old-laptop", "old-laptop", "windows", "100.64.10.61", False, [], -12, seen_min=60 * 24 * 40),
    ],
    "home.example": [
        dev(11, "pi-kitchen", "pi-kitchen", "linux", "100.64.20.11", True, [], 200),
        dev(12, "nas-garage", "nas-garage", "linux", "100.64.20.12", True, ["tag:storage"], 200, no_expiry=True),
        dev(13, "laptop-anna", "laptop-anna", "macOS", "100.64.20.13", False, [], 200, seen_min=240),
    ],
}

LOCK = threading.Lock()


def find(device_id):
    for devices in DEVICES.values():
        for d in devices:
            if d["id"] == device_id:
                return d
    return None


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, code, obj=None):
        body = json.dumps(obj if obj is not None else {}).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def tailnet_for_token(self):
        token = self.headers.get("Authorization", "").removeprefix("Bearer ")
        for tailnet, key in KEYS.items():
            if key == token:
                return tailnet
        return None

    def body(self):
        n = int(self.headers.get("Content-Length") or 0)
        return json.loads(self.rfile.read(n) or b"{}") if n else {}

    def do_GET(self):
        tailnet = self.tailnet_for_token()
        if not tailnet:
            return self.reply(401, {"message": "invalid API key"})
        path = self.path.split("?")[0]
        m = re.fullmatch(r"/tailnet/([^/]+)/devices", path)
        if m:
            with LOCK:
                return self.reply(200, {"devices": DEVICES.get(tailnet, [])})
        m = re.fullmatch(r"/tailnet/([^/]+)/keys/([^/]+)", path)
        if m:
            days = 41 if tailnet == "acme.example" else 75
            return self.reply(200, {"expires": ts(timedelta(days=days))})
        self.reply(404, {"message": "not found"})

    def do_POST(self):
        if not self.tailnet_for_token():
            return self.reply(401, {"message": "invalid API key"})
        m = re.fullmatch(r"/device/([^/]+)/(key|expire|name|tags)", self.path)
        if not m:
            return self.reply(404, {"message": "not found"})
        body = self.body()
        with LOCK:
            d = find(m.group(1))
            if d is None:
                return self.reply(404, {"message": "device not found"})
            what = m.group(2)
            if what == "key":
                d["keyExpiryDisabled"] = bool(body.get("keyExpiryDisabled"))
                if d["keyExpiryDisabled"]:
                    d.pop("expires", None)
                else:
                    d["expires"] = ts(timedelta(days=180))
            elif what == "expire":
                d["expires"] = ts(-timedelta(minutes=1))
            elif what == "name":
                d["name"] = body["name"] + ".tail1a2b3c.ts.net"
            elif what == "tags":
                d["tags"] = body["tags"]
        self.reply(200)

    def do_DELETE(self):
        if not self.tailnet_for_token():
            return self.reply(401, {"message": "invalid API key"})
        m = re.fullmatch(r"/device/([^/]+)", self.path)
        with LOCK:
            for devices in DEVICES.values():
                devices[:] = [d for d in devices if not (m and d["id"] == m.group(1))]
        self.reply(200)


def main():
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    with open(sys.argv[1], "w") as f:
        f.write(str(server.server_address[1]))
    server.serve_forever()


if __name__ == "__main__":
    main()
