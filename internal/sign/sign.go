// Package sign recomputes provider webhook signatures so a replayed
// delivery verifies instead of being rejected as tampered: Stripe, GitHub
// and Shopify all check an HMAC-SHA256 over the raw body, which a replay
// (especially an edited one) invalidates. Compute returns the headers to
// override on the replayed request. Standard library only.
package sign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// Params names the provider, the signing secret and the moment to sign at.
// Time should be the replay's send time (the caller passes time.Now());
// providers reject signatures whose timestamp drifts too far from receipt.
type Params struct {
	Provider string
	Secret   string
	Time     time.Time
}

var providers = map[string]func(Params, []byte) (map[string]string, error){
	"github":  computeGitHub,
	"shopify": computeShopify,
	"stripe":  computeStripe,
}

// Supported lists the providers Compute knows, alphabetically.
func Supported() []string {
	return slices.Sorted(maps.Keys(providers))
}

// Compute signs body for p.Provider and returns the headers to SET
// (override, never append) on the replayed request. An unknown provider or
// an empty secret is an error: a replay that asked for signing must never
// go out carrying the captured delivery's stale signature.
func Compute(p Params, body []byte) (map[string]string, error) {
	compute, ok := providers[p.Provider]
	if !ok {
		return nil, fmt.Errorf("sign: unknown provider %q (supported: %s)",
			p.Provider, strings.Join(Supported(), ", "))
	}
	if p.Secret == "" {
		return nil, fmt.Errorf("sign: %s: empty secret", p.Provider)
	}
	return compute(p, body)
}

func computeGitHub(p Params, body []byte) (map[string]string, error) {
	return map[string]string{
		"X-Hub-Signature-256": "sha256=" + hex.EncodeToString(hmacSHA256([]byte(p.Secret), body)),
	}, nil
}

func computeShopify(p Params, body []byte) (map[string]string, error) {
	return map[string]string{
		"X-Shopify-Hmac-SHA256": base64.StdEncoding.EncodeToString(hmacSHA256([]byte(p.Secret), body)),
	}, nil
}

// computeStripe signs the Stripe wire format: the header value is
// "t=<unix seconds>,v1=<hex>" where v1 is HMAC-SHA256 over "<t>.<body>".
// The full endpoint secret, including any "whsec_" prefix, is the HMAC key.
func computeStripe(p Params, body []byte) (map[string]string, error) {
	t := p.Time.Unix()
	mac := hmac.New(sha256.New, []byte(p.Secret))
	fmt.Fprintf(mac, "%d.", t)
	mac.Write(body)
	return map[string]string{
		"Stripe-Signature": fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil))),
	}, nil
}

// hmacSHA256 is HMAC-SHA256(key, body) in one slice.
func hmacSHA256(key, body []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return mac.Sum(nil)
}
