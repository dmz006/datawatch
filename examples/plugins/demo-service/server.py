#!/usr/bin/env python3
"""Reference service-kind plugin — a real, minimal HTTP server.

Usage: server.py <port>

Exists to prove the managed-lifecycle supervisor (BL403 Phase 2)
against something real: a process datawatch can actually start, probe
via GET /healthz, and stop — not a mock. Stdlib only, no dependencies,
so it runs anywhere Python 3 does.
"""
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # quiet — datawatch captures our stdout/stderr anyway
        pass

    def do_GET(self):
        if self.path == "/healthz":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"status":"ok"}')
        else:
            self.send_response(404)
            self.end_headers()


def main() -> int:
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8765
    HTTPServer(("127.0.0.1", port), Handler).serve_forever()
    return 0


if __name__ == "__main__":
    sys.exit(main())
