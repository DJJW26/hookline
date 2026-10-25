# Demo tape for hookline -- renders the README GIF.
#
#   vhs demo.tape        (from the repo root; install vhs first:
#                         go install github.com/charmbracelet/vhs@latest)
#
# What it does, in order: seeds a throwaway HOOKLINE_DATA_DIR with three
# real deliveries (a Stripe payment webhook, a GitHub pull_request webhook
# and a urlencoded form POST -- JSONL schema per docs/ARCHITECTURE.md),
# builds the binary from this checkout (go is on PATH), then launches
# ./hookline and walks the demo:
#
#   j        select the second delivery        Enter   open the detail pane
#   2        headers tab                       / term  live search
#   Escape   clear the search                  r       replay -> no-target hint
#   t        set a replay target               r       replay it
#   q        quit
#
# VHS runs ONE shell, so the exported HOOKLINE_DATA_DIR persists into the
# ./hookline process. python3 is only used to seed the history; swap it for
# python on setups that only ship that name. Setup happens under Hide so
# the GIF starts clean.
Output demo.gif
Set FontSize 20
Set Width 1100
Set Height 620
Set Theme { "name": "Dusk", "background": "#1B1A23", "foreground": "#ECEBF0", "cursor": "#9D8CFF", "selection": "#343148", "black": "#3A3943", "brightBlack": "#605F6B", "red": "#EB4268", "brightRed": "#EB4268", "green": "#12C78F", "brightGreen": "#7FC98F", "yellow": "#D4C55C", "brightYellow": "#D4C55C", "blue": "#7FA8E8", "brightBlue": "#7FA8E8", "magenta": "#6B50FF", "brightMagenta": "#9D8CFF", "cyan": "#C77DFF", "brightCyan": "#C77DFF", "white": "#ECEBF0", "brightWhite": "#ECEBF0" }
# Fast typing while hidden; the visible demo keys slow back down after Show.
Set TypingSpeed 1ms

Hide
Type 'export HOOKLINE_DATA_DIR=$(python3 -c "import tempfile;print(tempfile.mkdtemp(prefix=\"hookline-demo-\"))")'
Enter
Type 'python3 -c "import base64,json,os;d=os.environ[\"HOOKLINE_DATA_DIR\"];J=lambda o:json.dumps(o,separators=(\",\",\":\"));B=lambda o:base64.b64encode(J(o).encode()).decode();stripe={\"id\":\"1792932004112-8c1f4a2e9b03\",\"ts\":\"2026-10-25T12:40:04Z\",\"method\":\"POST\",\"path\":\"/hooks/stripe\",\"query\":\"\",\"headers\":{\"Content-Type\":[\"application/json\"],\"User-Agent\":[\"Stripe/1.1 (+https://stripe.com/docs/webhooks)\"],\"Stripe-Signature\":[\"t=1792932004,v1=5250a3e5b19cf4b0a4ff9f6901e2d4c8a1b3c6d5e4f7a8b9c0d1e2f3a4b5c6d7\"]},\"body_b64\":B({\"id\":\"evt_1QxYzAbCdEfGhIjKlMnOp\",\"object\":\"event\",\"api_version\":\"2024-06-20\",\"created\":1792932004,\"livemode\":False,\"type\":\"payment_intent.succeeded\",\"data\":{\"object\":{\"id\":\"pi_1QxYzAbCdEfGhIjKlMnOp\",\"object\":\"payment_intent\",\"amount\":4200,\"currency\":\"usd\",\"status\":\"succeeded\"}}}),\"src_ip\":\"203.0.113.7\",\"response_status\":200};github={\"id\":\"1792932007128-d4e07b1f5a92\",\"ts\":\"2026-10-25T12:40:07Z\",\"method\":\"POST\",\"path\":\"/hooks/github\",\"query\":\"delivery_id=72b3d2f0\",\"headers\":{\"Content-Type\":[\"application/json\"],\"User-Agent\":[\"GitHub-Hookshot/1a2b3c4\"],\"X-GitHub-Event\":[\"pull_request\"],\"X-GitHub-Delivery\":[\"72b3d2f0-eb1a-46c3-9d51-6a9f4b2c8d7e\"],\"X-Hub-Signature-256\":[\"sha256=3a7bd30c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f80\"]},\"body_b64\":B({\"action\":\"opened\",\"number\":42,\"pull_request\":{\"id\":2571043389,\"title\":\"listener: cap request bodies at 10 MiB\",\"user\":{\"login\":\"octocat\"},\"head\":{\"ref\":\"cap-body-size\",\"sha\":\"f2c1e9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1\"},\"base\":{\"ref\":\"main\",\"sha\":\"9d8c7b6a5f4e3d2c1b0a99887766554433221100\"}},\"repository\":{\"id\":900000000,\"full_name\":\"DJJW26/hookline\",\"name\":\"hookline\",\"owner\":{\"login\":\"DJJW26\"}},\"sender\":{\"login\":\"octocat\"}}),\"src_ip\":\"140.82.115.1\",\"response_status\":200};form={\"id\":\"1792932012734-2f9c6d3a81b4\",\"ts\":\"2026-10-25T12:40:12Z\",\"method\":\"POST\",\"path\":\"/signup\",\"query\":\"\",\"headers\":{\"Content-Type\":[\"application/x-www-form-urlencoded\"],\"User-Agent\":[\"curl/8.7.1\"]},\"body_b64\":base64.b64encode(b\"name=Ada+Lovelace&email=ada%40example.com&plan=pro\").decode(),\"src_ip\":\"127.0.0.1\",\"response_status\":200};open(d+\"/hookline.jsonl\",\"w\").write(\"\n\".join(J(r) for r in (stripe,github,form))+\"\n\")"'
Enter
# Build from the checkout (portable: no committed binary), then clear the
# screen so Show starts on a clean prompt. The Sleep covers a warm build;
# if it runs long the later commands simply queue in the tty buffer.
Type 'go build -o hookline ./cmd/hookline && clear'
Enter
Sleep 20s
Show
Set TypingSpeed 60ms

Type './hookline'
Enter
Sleep 2s
Type 'j'
Sleep 1s
Enter
Sleep 1s
Type '2'
Sleep 1s
Type '/'
Sleep 500ms
Type 'stripe'
Sleep 1s
Escape
Sleep 1s
Type 'r'
Sleep 1500ms
Type 't'
Sleep 500ms
Type 'http://127.0.0.1:3000/hooks'
Sleep 500ms
Enter
Sleep 1s
Type 'r'
Sleep 3s
Type 'q'
Sleep 500ms

# vhs demo.tape renders README.gif; install vhs first
