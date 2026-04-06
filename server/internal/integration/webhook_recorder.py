#!/usr/bin/env python3

import base64
import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


state_lock = threading.Lock()
records = []
delay_ms = 0


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/_admin/records":
            self.send_response(404)
            self.end_headers()
            return

        with state_lock:
            payload = {
                "records": list(records),
                "delay_ms": delay_ms,
            }

        body = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        content_length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(content_length)

        if self.path == "/_admin/reset":
            request = json.loads(body.decode("utf-8") or "{}")
            next_delay = int(request.get("delay_ms", 0))
            with state_lock:
                global delay_ms
                delay_ms = max(next_delay, 0)
                records.clear()
            self.send_response(204)
            self.end_headers()
            return

        if self.path != "/":
            self.send_response(404)
            self.end_headers()
            return

        record = {
            "body_base64": base64.b64encode(body).decode("ascii"),
            "headers": {key: self.headers.get_all(key) for key in self.headers.keys()},
            "signature": self.headers.get("X-Teraslack-Signature", ""),
        }

        with state_lock:
            records.append(record)
            current_delay = delay_ms

        if current_delay > 0:
            time.sleep(current_delay / 1000)

        self.send_response(204)
        self.end_headers()

    def log_message(self, format, *args):
        return


def main():
    server = ThreadingHTTPServer(("0.0.0.0", 8090), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
