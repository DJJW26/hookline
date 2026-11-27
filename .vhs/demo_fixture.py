"""Local test fixture for the Hookline demo."""

import json
import sys
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path


ROUTE = "/webhooks/stripe?test=1"
EVENT = {
    "id": "evt_demo",
    "type": "payment_intent.succeeded",
    "amount": 4200,
    "currency": "usd",
}


class Receiver(BaseHTTPRequestHandler):
    def do_POST(self):
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        accepted = self.path == ROUTE and payload == {**EVENT, "amount": 4500}
        body = json.dumps({"accepted": accepted, "amount": payload.get("amount")})
        self.send_response(200 if accepted else 422)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body.encode())


def capture(port):
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}{ROUTE}",
        data=json.dumps(EVENT, indent=2).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        print(f"Hookline accepted capture ({response.status})")


if __name__ == "__main__":
    action, argument = sys.argv[1:]
    if action == "serve":
        with HTTPServer(("127.0.0.1", int(argument)), Receiver) as server:
            server.serve_forever()
    elif action == "capture":
        capture(argument)
    elif action == "edit":
        path = Path(argument)
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["amount"] = 4500
        path.write_text(json.dumps(payload, indent=2), encoding="utf-8")
