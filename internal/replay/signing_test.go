package replay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DJJW26/hookline/internal/delivery"
	"github.com/DJJW26/hookline/internal/sign"
)

// Signing integration: Do must override — not append — the provider's
// signature headers, computed over the replay body. Expected values are
// re-derived here with crypto/hmac, independent of internal/sign.

// refHMAC is the reference HMAC-SHA256 the expectations are built from.
func refHMAC(key, msg []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	return mac.Sum(nil)
}

var sigTS = time.Unix(1761400000, 0).UTC()

// wantStripeHeader builds the expected Stripe-Signature value for key.
func wantStripeHeader(key []byte, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	return fmt.Sprintf("t=%s,v1=%s", ts, hex.EncodeToString(refHMAC(key, []byte(ts+"."+string(body)))))
}

func TestDoSigningOverridesCapturedSignature(t *testing.T) {
	body := []byte(`{"edited":true,"n":2}`)
	rawStripeKey := []byte("stripe-raw-key-bytes")
	whsec := "whsec_" + base64.StdEncoding.EncodeToString(rawStripeKey)
	githubSecret := "gh-secret-42"
	shopifySecret := "shpss-42"

	tests := []struct {
		name       string
		opt        Option
		sigHeader  string // header the provider checks
		staleValue string // what the original capture carried
		want       string // exact value the target must receive
	}{
		{
			name:       "stripe bare secret",
			opt:        WithSigning(sign.Params{Provider: "stripe", Secret: "rk_live_bare", Time: sigTS}),
			sigHeader:  "Stripe-Signature",
			staleValue: "t=1,v1=deadbeef",
			want:       wantStripeHeader([]byte("rk_live_bare"), sigTS, body),
		},
		{
			name:       "stripe whsec_ secret equals decoded raw key",
			opt:        WithSigning(sign.Params{Provider: "stripe", Secret: whsec, Time: sigTS}),
			sigHeader:  "Stripe-Signature",
			staleValue: "t=1,v1=deadbeef",
			want:       wantStripeHeader(rawStripeKey, sigTS, body),
		},
		{
			name:       "github",
			opt:        WithSigning(sign.Params{Provider: "github", Secret: githubSecret, Time: sigTS}),
			sigHeader:  "X-Hub-Signature-256",
			staleValue: "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			want:       "sha256=" + hex.EncodeToString(refHMAC([]byte(githubSecret), body)),
		},
		{
			name:       "shopify",
			opt:        WithSigning(sign.Params{Provider: "shopify", Secret: shopifySecret, Time: sigTS}),
			sigHeader:  "X-Shopify-Hmac-SHA256",
			staleValue: "c3RhbGUtYmFzZTY0",
			want:       base64.StdEncoding.EncodeToString(refHMAC([]byte(shopifySecret), body)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			var gotBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Values(tc.sigHeader)
				gotBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(200)
			}))
			defer srv.Close()

			d := delivery.Delivery{
				ID:     "d-sign",
				Method: "POST",
				Path:   "/webhook",
				Headers: map[string][]string{
					"Content-Type": {"application/json"},
					tc.sigHeader:   {tc.staleValue}, // stale signature from the capture
				},
				Body: body,
			}
			res, err := Do(context.Background(), srv.URL, d, tc.opt)
			if err != nil {
				t.Fatalf("Do error: %v", err)
			}
			if res.StatusCode != http.StatusOK {
				t.Fatalf("StatusCode = %d, want 200", res.StatusCode)
			}
			if len(got) != 1 {
				t.Fatalf("%s received %d values (%v), want exactly 1 — headers must be overridden, not appended",
					tc.sigHeader, len(got), got)
			}
			if got[0] == tc.staleValue {
				t.Fatalf("%s still carries the stale captured signature %q", tc.sigHeader, got[0])
			}
			if got[0] != tc.want {
				t.Errorf("%s = %q, want %q", tc.sigHeader, got[0], tc.want)
			}
			if string(gotBody) != string(body) {
				t.Errorf("replayed body = %q, want %q", gotBody, body)
			}
		})
	}
}

// Without WithSigning the captured signature header must pass through
// untouched — the option is opt-in and existing behavior cannot change.
func TestDoWithoutSigningForwardsCapturedSignature(t *testing.T) {
	stale := "t=1,v1=deadbeef"
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Values("Stripe-Signature")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := delivery.Delivery{
		Method:  "POST",
		Path:    "/webhook",
		Headers: map[string][]string{"Stripe-Signature": {stale}},
		Body:    []byte(`{}`),
	}
	if _, err := Do(context.Background(), srv.URL, d); err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if len(got) != 1 || got[0] != stale {
		t.Errorf("no-signing replay changed the signature: got %v, want [%q]", got, stale)
	}
}

// A signing failure (here: unknown provider) must abort the replay — never
// send the request carrying the stale captured signature.
func TestDoSigningErrorAbortsSend(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := delivery.Delivery{
		Method:  "POST",
		Path:    "/webhook",
		Headers: map[string][]string{"Stripe-Signature": {"t=1,v1=deadbeef"}},
		Body:    []byte(`{}`),
	}
	_, err := Do(context.Background(), srv.URL, d,
		WithSigning(sign.Params{Provider: "slack", Secret: "x", Time: sigTS}))
	if err == nil {
		t.Fatal("Do with unknown signing provider returned nil error")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("error %q does not name the signing problem", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("target received %d requests; a signing failure must abort the send", n)
	}
}
