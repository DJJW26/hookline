package sign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The expectations below are computed independently of Compute: the test
// re-derives every HMAC with crypto/hmac directly, so a bug in Compute and
// a bug in its own expectations cannot cancel out.

// refMAC is the reference HMAC-SHA256 the expectations are built from.
func refMAC(key, msg []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	return mac.Sum(nil)
}

var (
	ts           = time.Unix(1700000000, 0).UTC()
	body         = []byte(`{"id":"evt_123","type":"payment_intent.succeeded"}`)
	rawStripeKey = []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04}
	whsecPfx     = "whsec_" + base64.StdEncoding.EncodeToString(rawStripeKey)
	bareStrpe    = "rk_live_51H8xQf"
	githubSec    = "it's-a-github-secret"
	shopifySc    = "shpss_9f8e7d6c"
)

// stripeValue builds the expected Stripe-Signature header value for key
// over "<t>.<body>", exactly as Stripe's verification docs define it.
func stripeValue(key []byte, t time.Time, b []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	payload := ts + "." + string(b)
	return "t=" + ts + ",v1=" + hex.EncodeToString(refMAC(key, []byte(payload)))
}

func TestComputeMatchesIndependentExpectations(t *testing.T) {
	tests := []struct {
		name   string
		params Params
		body   []byte
		want   map[string]string // exact header name → exact value
	}{
		{
			name:   "stripe bare secret used as raw key",
			params: Params{Provider: "stripe", Secret: bareStrpe, Time: ts},
			body:   body,
			want:   map[string]string{"Stripe-Signature": stripeValue([]byte(bareStrpe), ts, body)},
		},
		{
			name:   "stripe whsec_ secret decodes to the raw key",
			params: Params{Provider: "stripe", Secret: whsecPfx, Time: ts},
			body:   body,
			want:   map[string]string{"Stripe-Signature": stripeValue(rawStripeKey, ts, body)},
		},
		{
			name:   "github sha256 hex of HMAC over body",
			params: Params{Provider: "github", Secret: githubSec, Time: ts},
			body:   body,
			want: map[string]string{
				"X-Hub-Signature-256": "sha256=" + hex.EncodeToString(refMAC([]byte(githubSec), body)),
			},
		},
		{
			name:   "shopify base64 of HMAC over body",
			params: Params{Provider: "shopify", Secret: shopifySc, Time: ts},
			body:   body,
			want: map[string]string{
				"X-Shopify-Hmac-SHA256": base64.StdEncoding.EncodeToString(refMAC([]byte(shopifySc), body)),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compute(tc.params, tc.body)
			if err != nil {
				t.Fatalf("Compute error: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("Compute returned %d headers (%v), want exactly 1", len(got), got)
			}
			for name, wantVal := range tc.want {
				gotVal, ok := got[name]
				if !ok {
					t.Fatalf("Compute returned headers %v, want header %q", got, name)
				}
				if gotVal != wantVal {
					t.Errorf("%s = %q, want %q", name, gotVal, wantVal)
				}
			}
		})
	}
}

// Each provider's raw signature must differ once the body is tampered with
// — the whole point of re-signing is that the provider's hmac.Equal on its
// side fails for a stale signature.
func TestComputeTamperedBodyDoesNotMatch(t *testing.T) {
	tampered := []byte(`{"id":"evt_123","type":"payment_intent.failed"}`)
	tests := []struct {
		name    string
		params  Params
		extract func(header string) []byte // raw MAC bytes out of the header value
	}{
		{
			name:   "stripe",
			params: Params{Provider: "stripe", Secret: bareStrpe, Time: ts},
			extract: func(header string) []byte {
				const v1 = ",v1="
				i := strings.Index(header, v1)
				if i < 0 {
					t.Fatalf("no v1 segment in %q", header)
				}
				raw, err := hex.DecodeString(header[i+len(v1):])
				if err != nil {
					t.Fatalf("v1 not hex: %v", err)
				}
				return raw
			},
		},
		{
			name:   "github",
			params: Params{Provider: "github", Secret: githubSec, Time: ts},
			extract: func(header string) []byte {
				raw, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
				if err != nil {
					t.Fatalf("signature not hex: %v", err)
				}
				return raw
			},
		},
		{
			name:   "shopify",
			params: Params{Provider: "shopify", Secret: shopifySc, Time: ts},
			extract: func(header string) []byte {
				raw, err := base64.StdEncoding.DecodeString(header)
				if err != nil {
					t.Fatalf("signature not base64: %v", err)
				}
				return raw
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			good, err := Compute(tc.params, body)
			if err != nil {
				t.Fatalf("Compute (good body) error: %v", err)
			}
			bad, err := Compute(tc.params, tampered)
			if err != nil {
				t.Fatalf("Compute (tampered body) error: %v", err)
			}
			var goodVal, badVal string
			for _, v := range good {
				goodVal = v
			}
			for _, v := range bad {
				badVal = v
			}
			if !hmac.Equal(tc.extract(goodVal), tc.extract(goodVal)) {
				t.Fatal("sanity: identical bodies compared unequal")
			}
			if hmac.Equal(tc.extract(goodVal), tc.extract(badVal)) {
				t.Errorf("tampered body matched the untampered signature: %q vs %q", goodVal, badVal)
			}
		})
	}
}

func TestStripeWhsecSecretEqualsDecodedKeyUsedRaw(t *testing.T) {
	viaWhsec, err := Compute(Params{Provider: "stripe", Secret: whsecPfx, Time: ts}, body)
	if err != nil {
		t.Fatalf("Compute (whsec_) error: %v", err)
	}
	viaRaw, err := Compute(Params{Provider: "stripe", Secret: string(rawStripeKey), Time: ts}, body)
	if err != nil {
		t.Fatalf("Compute (raw key) error: %v", err)
	}
	if !hmac.Equal([]byte(viaWhsec["Stripe-Signature"]), []byte(viaRaw["Stripe-Signature"])) {
		t.Errorf("whsec_ secret and decoded raw key disagree: %q vs %q",
			viaWhsec["Stripe-Signature"], viaRaw["Stripe-Signature"])
	}
}

func TestComputeErrors(t *testing.T) {
	tests := []struct {
		name    string
		params  Params
		body    []byte
		wantIn  string // substring of the error message
		wantNot string // must not appear: secrets never leak into errors
	}{
		{
			name:   "stripe empty secret",
			params: Params{Provider: "stripe", Secret: "", Time: ts},
			body:   body,
			wantIn: "empty secret",
		},
		{
			name:   "github empty secret",
			params: Params{Provider: "github", Secret: "", Time: ts},
			body:   body,
			wantIn: "empty secret",
		},
		{
			name:   "shopify empty secret",
			params: Params{Provider: "shopify", Secret: "", Time: ts},
			body:   body,
			wantIn: "empty secret",
		},
		{
			name:    "unknown provider",
			params:  Params{Provider: "slack", Secret: "super-secret-value", Time: ts},
			body:    body,
			wantIn:  `unknown provider "slack"`,
			wantNot: "super-secret-value",
		},
		{
			name:   "unknown provider with empty secret",
			params: Params{Provider: "slack", Secret: "", Time: ts},
			body:   body,
			wantIn: `unknown provider "slack"`,
		},
		{
			name:    "stripe whsec_ secret not valid base64",
			params:  Params{Provider: "stripe", Secret: "whsec_%%not-base64%%", Time: ts},
			body:    body,
			wantIn:  "whsec_",
			wantNot: "%%not-base64%%",
		},
		{
			name:    "stripe whsec_ with empty remainder is an empty key",
			params:  Params{Provider: "stripe", Secret: "whsec_", Time: ts},
			body:    body,
			wantIn:  "empty key",
			wantNot: "t=",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compute(tc.params, tc.body)
			if err == nil {
				t.Fatalf("Compute(%+v) = %v, want error", tc.params, got)
			}
			if got != nil {
				t.Errorf("error case returned headers %v, want nil", got)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q missing %q", err, tc.wantIn)
			}
			if tc.wantNot != "" && strings.Contains(err.Error(), tc.wantNot) {
				t.Errorf("error %q leaks %q", err, tc.wantNot)
			}
		})
	}
}

func TestUnknownProviderErrorListsSupported(t *testing.T) {
	_, err := Compute(Params{Provider: "nope", Secret: "x", Time: ts}, body)
	if err == nil {
		t.Fatal("unknown provider returned nil error")
	}
	for _, p := range Supported() {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q does not list supported provider %q", err, p)
		}
	}
}

func TestSupportedProviders(t *testing.T) {
	want := map[string]bool{"stripe": true, "github": true, "shopify": true}
	got := Supported()
	if len(got) != len(want) {
		t.Fatalf("Supported() = %v, want exactly %v", got, want)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("Supported() has unexpected provider %q", p)
		}
	}
}

// The Stripe timestamp must come from Params.Time, not from the wall clock,
// or replays would not be reproducible from a fixed Params.
func TestStripeUsesParamsTime(t *testing.T) {
	early := time.Unix(1000000000, 0).UTC()
	late := time.Unix(1700000000, 0).UTC()
	a, err := Compute(Params{Provider: "stripe", Secret: bareStrpe, Time: early}, body)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Compute(Params{Provider: "stripe", Secret: bareStrpe, Time: late}, body)
	if err != nil {
		t.Fatal(err)
	}
	wantA := fmt.Sprintf("t=%d,v1=", early.Unix())
	wantB := fmt.Sprintf("t=%d,v1=", late.Unix())
	if !strings.HasPrefix(a["Stripe-Signature"], wantA) {
		t.Errorf("early signature %q missing prefix %q", a["Stripe-Signature"], wantA)
	}
	if !strings.HasPrefix(b["Stripe-Signature"], wantB) {
		t.Errorf("late signature %q missing prefix %q", b["Stripe-Signature"], wantB)
	}
	if a["Stripe-Signature"] == b["Stripe-Signature"] {
		t.Error("different Params.Time produced identical signatures")
	}
}
