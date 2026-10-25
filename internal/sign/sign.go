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

// Supported lists the providers Compute knows, alphabetically.
func Supported() []string {
	return []string{"github", "shopify", "stripe"}
}

// Compute signs body for p.Provider and returns the headers to SET
// (override, never append) on the replayed request. An unknown provider or
// an empty secret is an error: a replay that asked for signing must never
// go out carrying the captured delivery's stale signature.
func Compute(p Params, body []byte) (map[string]string, error) {
	switch p.Provider {
	case "stripe":
		return computeStripe(p, body)
	case "github":
		key, err := rawKey(p)
		if err != nil {
			return nil, err
		}
		return map[string]string{
			"X-Hub-Signature-256": "sha256=" + hexSig(key, body),
		}, nil
	case "shopify":
		key, err := rawKey(p)
		if err != nil {
			return nil, err
		}
		return map[string]string{
			"X-Shopify-Hmac-SHA256": base64.StdEncoding.EncodeToString(hmacSHA256(key, body)),
		}, nil
	default:
		return nil, fmt.Errorf("sign: unknown provider %q (supported: %s)",
			p.Provider, strings.Join(Supported(), ", "))
	}
}

// computeStripe signs the Stripe wire format: the header value is
// "t=<unix seconds>,v1=<hex>" where v1 is HMAC-SHA256 over "<t>.<body>".
// A "whsec_"-prefixed secret carries the key base64-encoded; a bare secret
// is used as raw bytes.
func computeStripe(p Params, body []byte) (map[string]string, error) {
	secret := p.Secret
	if secret == "" {
		return nil, fmt.Errorf("sign: stripe: empty secret")
	}
	key := []byte(secret)
	if rest, ok := strings.CutPrefix(secret, "whsec_"); ok {
		decoded, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return nil, fmt.Errorf("sign: stripe: decoding whsec_ secret: %w", err)
		}
		if len(decoded) == 0 {
			return nil, fmt.Errorf("sign: stripe: whsec_ secret decodes to an empty key")
		}
		key = decoded
	}
	t := p.Time.Unix()
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "%d.", t)
	mac.Write(body)
	return map[string]string{
		"Stripe-Signature": fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil))),
	}, nil
}

// rawKey returns the signing key for providers that use the secret bytes
// as-is (GitHub, Shopify), rejecting an empty secret.
func rawKey(p Params) ([]byte, error) {
	if p.Secret == "" {
		return nil, fmt.Errorf("sign: %s: empty secret", p.Provider)
	}
	return []byte(p.Secret), nil
}

// hmacSHA256 is HMAC-SHA256(key, body) in one slice.
func hmacSHA256(key, body []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return mac.Sum(nil)
}

// hexSig hex-encodes HMAC-SHA256(key, body).
func hexSig(key, body []byte) string {
	return hex.EncodeToString(hmacSHA256(key, body))
}
