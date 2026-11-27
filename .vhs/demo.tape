# Run from the repository root on macOS/Linux: vhs .vhs/demo.tape
# Requires Go, Python 3, and an idle 43000/44040 on loopback.
Output docs/demo.gif
Set Shell "bash"
Set FontSize 16
Set Width 1160
Set Height 560
Set Theme { "name": "Dusk", "background": "#1B1A23", "foreground": "#ECEBF0", "cursor": "#9D8CFF", "selection": "#343148", "black": "#3A3943", "brightBlack": "#605F6B", "red": "#EB4268", "brightRed": "#EB4268", "green": "#12C78F", "brightGreen": "#7FC98F", "yellow": "#D4C55C", "brightYellow": "#D4C55C", "blue": "#7FA8E8", "brightBlue": "#7FA8E8", "magenta": "#6B50FF", "brightMagenta": "#9D8CFF", "cyan": "#C77DFF", "brightCyan": "#C77DFF", "white": "#ECEBF0", "brightWhite": "#ECEBF0" }

Hide
Set TypingSpeed 0.01s
Type 'export HOOKLINE_DATA_DIR=$(mktemp -d) EDITOR="python3 .vhs/demo_fixture.py edit"'
Enter
Type 'if go build -o "$HOOKLINE_DATA_DIR/hookline" ./cmd/hookline; then python3 .vhs/demo_fixture.py serve 43000 & fi'
Enter
Sleep 15s
Type 'receiver=$!; (sleep 3; python3 .vhs/demo_fixture.py capture 44040) >/dev/null 2>&1 & "$HOOKLINE_DATA_DIR/hookline" --port 44040 --theme dark --target http://127.0.0.1:43000'
Enter
Sleep 1s
Show

Sleep 5s
Type 'e'
Sleep 3s
Type 's'
Sleep 2s
Enter
Type '4'
Sleep 3s

Hide
Type 'q'
Sleep 1s
Type 'kill "$receiver"'
Enter
Type 'rm -f "$HOOKLINE_DATA_DIR/hookline" "$HOOKLINE_DATA_DIR/hookline.jsonl" "$HOOKLINE_DATA_DIR/config.json"; rmdir "$HOOKLINE_DATA_DIR"'
Enter
