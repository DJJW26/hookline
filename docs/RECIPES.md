# Capture and replay recipes

These recipes use a local test receiver that echoes the request path and JSON body. Use sample data and separate Hookline history for each recipe. The receiver listens on `127.0.0.1:3000` and Hookline listens on `127.0.0.1:4041`. Install [Hookline](../README.md#install) and Python 3 before starting.

## Local quickstart

Save the following as `receiver.py` in a scratch directory. The optional `--stripe` mode is used in the next recipe. The basic mode needs only the Python standard library.

```python
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer


class Receiver(BaseHTTPRequestHandler):
    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        if stripe_secret:
            try:
                stripe.Webhook.construct_event(
                    raw, self.headers.get("Stripe-Signature", ""), stripe_secret
                )
            except (ValueError, stripe.SignatureVerificationError):
                self.send_error(400, "Stripe verification failed")
                return

        try:
            payload = json.loads(raw)
        except (ValueError, UnicodeError):
            self.send_error(400, "Expected JSON")
            return

        result = {"path": self.path, "body": payload}
        if stripe_secret:
            result = {
                "path": self.path,
                "signature": "verified",
                "edited": payload.get("hookline_test", False),
            }
        data = json.dumps(result).encode()
        print(data.decode(), flush=True)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


if __name__ == "__main__":
    stripe_secret = None
    if "--stripe" in sys.argv:
        import stripe

        stripe_secret = os.environ.get("HOOKLINE_SIGN_SECRET")
        if not stripe_secret:
            sys.exit("Set HOOKLINE_SIGN_SECRET to the full Stripe CLI signing secret")

    print("Test receiver listening on http://127.0.0.1:3000", flush=True)
    HTTPServer(("127.0.0.1", 3000), Receiver).serve_forever()
```

In a terminal in the scratch directory, run `python3 receiver.py` on macOS or Linux, or `py -3 receiver.py` in PowerShell. Leave it running. In a second terminal, start Hookline with isolated history.

```sh
export HOOKLINE_DATA_DIR="$(mktemp -d)"
hookline --port 4041 --target http://127.0.0.1:3000
```

```powershell
$env:HOOKLINE_DATA_DIR = Join-Path ([IO.Path]::GetTempPath()) ('hookline-' + [guid]::NewGuid().ToString('N'))
hookline --port 4041 --target http://127.0.0.1:3000
```

Send a sample from a third terminal.

```sh
curl -i 'http://127.0.0.1:4041/hooks/demo?source=local' \
  -H 'Content-Type: application/json' --data-binary '{"hello":"world"}'
```

```powershell
Invoke-WebRequest -Uri 'http://127.0.0.1:4041/hooks/demo?source=local' `
  -Method Post -ContentType 'application/json' -Body '{"hello":"world"}'
```

Select the captured request and press `enter` to inspect it. Press `r` to replay it and `4` in the detail pane to see the receiver’s `200` and echoed `/hooks/demo?source=local`. Press `e`, change `"world"` to `"hookline"`, save, and exit your editor. Review the diff and press `s`. The receiver prints the new body. Press `q` to quit Hookline and Ctrl+C in the receiver terminal to stop it.

## Stripe CLI with signature verification

Use a Stripe sandbox and the [Stripe CLI](https://docs.stripe.com/cli/install). Set up a virtual environment in the scratch directory with the [official Stripe Python SDK](https://github.com/stripe/stripe-python).

```sh
python3 -m venv .venv
.venv/bin/python -m pip install stripe
```

```powershell
py -3 -m venv .venv
.\.venv\Scripts\python.exe -m pip install stripe
```

1. In a separate terminal, run `stripe login`, then start the forwarder and keep it running. Copy the full signing secret printed by `stripe listen`, including the `whsec_` prefix. Use the CLI secret for this session. [Listen command](https://docs.stripe.com/cli/listen) · [Stripe signature errors](https://docs.stripe.com/events/manage-webhook-endpoints#signature-errors)

   ```sh
   stripe listen --events payment_intent.succeeded --forward-to 'http://127.0.0.1:4041/hooks/stripe?source=cli'
   ```

2. Set the secret in both the receiver terminal and the Hookline terminal. Enter it separately at each terminal’s prompt.

   ```bash
   read -rsp 'Stripe CLI signing secret: ' HOOKLINE_SIGN_SECRET
   printf '\n'
   export HOOKLINE_SIGN_SECRET
   ```

   ```powershell
   $env:HOOKLINE_SIGN_SECRET = Read-Host 'Stripe CLI signing secret' -MaskInput
   ```

   The shell example uses Bash and the masked prompt uses PowerShell 7. In the receiver terminal, run `.venv/bin/python receiver.py --stripe` or `.\.venv\Scripts\python.exe receiver.py --stripe` (PowerShell). In the Hookline terminal, create a fresh `HOOKLINE_DATA_DIR` as in the quickstart and run the following command.

   ```sh
   hookline --port 4041 --target http://127.0.0.1:3000 --sign stripe
   ```

3. In another terminal, run `stripe trigger payment_intent.succeeded` in the same sandbox. Check that Hookline captured `/hooks/stripe?source=cli`. Press `q`, then start Hookline again with the same command **in the same terminal** so it keeps its history and secret. [Trigger command](https://docs.stripe.com/cli/trigger)
4. Select the saved delivery and press `e`. Add `"hookline_test": true,` as the first property inside the top-level JSON object. Save and exit, then press `s` in diff review. In the receiver terminal, or under `enter` then `4` in Hookline, look for `"signature": "verified"`, `"edited": true`, and `"path": "/hooks/stripe?source=cli"`. The receiver verifies the exact bytes before decoding the JSON.

## GitHub webhook over a tunnel

1. Start the receiver in basic mode. In a separate terminal, start Hookline with fresh `HOOKLINE_DATA_DIR` and run `hookline --port 4041 --target http://127.0.0.1:3000`.
2. Point an existing HTTPS tunnel to `http://127.0.0.1:4041`. With a configured ngrok agent, run `ngrok http 4041`. Hookline uses `4041` here so ngrok can use `4040` for its local inspector. [ngrok inspector](https://ngrok.com/docs/gateway/agent/web-inspection-interface)
3. In a GitHub test repository you administer, open **Settings → Webhooks → Add webhook**. Use the tunnel’s public HTTPS URL with `/hooks/github?source=test` appended as the payload URL. Set the content type to `application/json`, choose **Just the push event**, and add the webhook. GitHub sends an initial `ping`. Make a test push if you also want to inspect a `push` event. [Create webhooks](https://docs.github.com/en/webhooks/using-webhooks/creating-webhooks)
4. Select the delivery in Hookline and check `X-GitHub-Event` in the Headers tab. Press `r`. The receiver should show `/hooks/github?source=test`. The target `http://127.0.0.1:3000` is a base URL. If you use `http://127.0.0.1:3000/debug` instead, the replay goes to `/debug/hooks/github?source=test` on the receiver.
5. In GitHub’s **Recent Deliveries**, the original `200` and `X-Hookline-ID` come from Hookline’s capture listener. That response acknowledges receipt. The dev receiver processes the payload when you replay it, and the Response tab shows its separate result. Hookline displays any history save failures in the TUI. When finished, disable or remove the test webhook and stop the tunnel. [Viewing webhook deliveries](https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/viewing-webhook-deliveries)
