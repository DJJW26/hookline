package ui

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DJJW26/hookline/internal/delivery"
)

// Re-signing on replay: the request must go out freshly signed for the
// configured provider over the (possibly edited) body, the toast must say
// so, and a configured provider with a missing secret must refuse to send.

// refSig is the reference HMAC-SHA256 the UI-test expectations are built
// from — independent of internal/sign.
func refSig(key, msg []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	return mac.Sum(nil)
}

func TestReplaySigningMissingSecretWarnsAndDoesNotSend(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	in := testDelivery("POST", "/hooks/stripe", `{"a":1}`)
	m := newTestApp(t, []delivery.Delivery{in})
	m.cfg.SignProvider = "stripe" // signing requested...
	m.cfg.SignSecret = ""         // ...but the secret never made it into the env
	m.target = srv.URL

	cmd := m.replaySelected(in)
	if cmd == nil {
		t.Fatal("replaySelected returned nil cmd")
	}
	if m.toast == nil || !strings.Contains(m.toast.text, "HOOKLINE_SIGN_SECRET") {
		t.Fatalf("expected missing-secret warning toast, got %+v", m.toast)
	}
	if m.toast.kind != toastWarn {
		t.Errorf("toast kind = %v, want warn", m.toast.kind)
	}
	// Whatever the returned command produces, the target must never see a
	// request: an unsigned replay is exactly what the provider rejects.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if _, ok := msg.(replayResultMsg); ok {
			t.Error("missing secret still produced a replay send")
		}
	case <-time.After(200 * time.Millisecond):
		// still pending (the toast tick) — no replay was sent either way
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("target received %d requests; missing secret must not send", n)
	}
}

func TestReplayReSignsEachProvider(t *testing.T) {
	body := []byte(`{"replay":true,"n":3}`)
	stale := map[string]string{
		"stripe":  "t=1,v1=deadbeef",
		"github":  "sha256=" + strings.Repeat("00", 32),
		"shopify": "c3RhbGUtYmFzZTY0",
	}
	secrets := map[string]string{
		"stripe":  "rk_live_ui_test",
		"github":  "gh-ui-secret",
		"shopify": "shpss-ui-secret",
	}
	headers := map[string]string{
		"stripe":  "Stripe-Signature",
		"github":  "X-Hub-Signature-256",
		"shopify": "X-Shopify-Hmac-SHA256",
	}
	verify := map[string]func(t *testing.T, val string, sentBody []byte){
		"stripe": func(t *testing.T, val string, sentBody []byte) {
			t.Helper()
			i := strings.Index(val, ",v1=")
			if !strings.HasPrefix(val, "t=") || i < 0 {
				t.Fatalf("signature %q is not t=<ts>,v1=<hex>", val)
			}
			ts := val[2:i]
			if _, err := strconv.ParseInt(ts, 10, 64); err != nil {
				t.Fatalf("stripe timestamp %q not unix seconds: %v", ts, err)
			}
			want := refSig([]byte(secrets["stripe"]), []byte(ts+"."+string(sentBody)))
			got, err := hex.DecodeString(val[i+4:])
			if err != nil {
				t.Fatalf("v1 not hex: %v", err)
			}
			if !hmac.Equal(got, want) {
				t.Errorf("v1 = %x, want %x (HMAC over %q at t=%s)", got, want, sentBody, ts)
			}
		},
		"github": func(t *testing.T, val string, sentBody []byte) {
			t.Helper()
			got, err := hex.DecodeString(strings.TrimPrefix(val, "sha256="))
			if err != nil {
				t.Fatalf("signature not hex: %v", err)
			}
			if !hmac.Equal(got, refSig([]byte(secrets["github"]), sentBody)) {
				t.Errorf("X-Hub-Signature-256 = %x, want %x", got, refSig([]byte(secrets["github"]), sentBody))
			}
		},
		"shopify": func(t *testing.T, val string, sentBody []byte) {
			t.Helper()
			got, err := base64.StdEncoding.DecodeString(val)
			if err != nil {
				t.Fatalf("signature not base64: %v", err)
			}
			if !hmac.Equal(got, refSig([]byte(secrets["shopify"]), sentBody)) {
				t.Errorf("X-Shopify-Hmac-SHA256 = %x, want %x", got, refSig([]byte(secrets["shopify"]), sentBody))
			}
		},
	}

	for _, provider := range []string{"stripe", "github", "shopify"} {
		t.Run(provider, func(t *testing.T) {
			var gotVals []string
			var gotBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotVals = r.Header.Values(headers[provider])
				gotBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusCreated)
			}))
			defer srv.Close()

			in := testDelivery("POST", "/hooks/"+provider, string(body))
			in.Headers[headers[provider]] = []string{stale[provider]} // captured stale signature
			m := newTestApp(t, []delivery.Delivery{in})
			m.cfg.SignProvider = provider
			m.cfg.SignSecret = secrets[provider]
			m.target = srv.URL

			cmd := m.replaySelected(in)
			if cmd == nil {
				t.Fatal("replaySelected returned nil cmd")
			}
			msg := cmd() // perform the HTTP call synchronously
			rmsg, ok := msg.(replayResultMsg)
			if !ok {
				t.Fatalf("cmd produced %T, want replayResultMsg", msg)
			}
			if rmsg.err != nil {
				t.Fatalf("replay error: %v", rmsg.err)
			}
			if rmsg.resigned != provider {
				t.Errorf("resigned = %q, want %q", rmsg.resigned, provider)
			}
			model, _ := m.Update(msg)
			*m = *model.(*Model)

			if m.toast == nil || !strings.Contains(m.toast.text, "re-signed ("+provider+")") {
				t.Fatalf("toast should note re-signing, got %+v", m.toast)
			}
			if m.toast == nil || !strings.Contains(m.toast.text, "201") {
				t.Errorf("toast should report 201, got %+v", m.toast)
			}
			if len(gotVals) != 1 {
				t.Fatalf("%s received %d values (%v), want exactly 1", headers[provider], len(gotVals), gotVals)
			}
			if gotVals[0] == stale[provider] {
				t.Errorf("%s still carries the stale captured signature", headers[provider])
			}
			verify[provider](t, gotVals[0], gotBody)
		})
	}
}

// Signing is opt-in per Config: with no SignProvider the replay path is
// byte-identical to before (no signature overrides, no toast note).
func TestReplayWithoutSigningConfigUnchanged(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Values("Stripe-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	in := testDelivery("POST", "/hooks/stripe", `{"a":1}`)
	in.Headers["Stripe-Signature"] = []string{"t=1,v1=deadbeef"}
	m := newTestApp(t, []delivery.Delivery{in})
	m.target = srv.URL

	msg := m.replaySelected(in)()
	model, _ := m.Update(msg)
	*m = *model.(*Model)

	if len(got) != 1 || got[0] != "t=1,v1=deadbeef" {
		t.Errorf("captured signature forwarded as %v, want [t=1,v1=deadbeef]", got)
	}
	if m.toast != nil && strings.Contains(m.toast.text, "re-signed") {
		t.Errorf("unsigned replay toast mentions re-signing: %+v", m.toast)
	}
}
