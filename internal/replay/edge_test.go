package replay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DJJW26/hookline/internal/delivery"
)

// Edge-case probes: redirect handling, timeouts, trailing-slash targets,
// duplicate header forwarding, and curl export quoting corners.

func TestDoRedirectBehavior(t *testing.T) {
	// The target says 302 → /elsewhere which returns 200. What does the
	// caller see? Document the actual behavior; report if it silently
	// follows.
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer final.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/elsewhere", http.StatusFound)
	}))
	defer redirector.Close()

	d := delivery.Delivery{Method: "POST", Path: "/here", Body: []byte("{}")}
	res, err := Do(context.Background(), redirector.URL, d)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("redirect handling: reported status %d", res.StatusCode)
	if res.StatusCode != http.StatusFound {
		t.Logf("FINDING: replay follows redirects silently (reported %d, target returned 302)", res.StatusCode)
	}
}

func TestDoHonorsShortContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	d := delivery.Delivery{Method: "GET", Path: "/slow"}
	_, err := Do(ctx, srv.URL, d)
	if err == nil {
		t.Error("hanging target with short ctx should error")
	}
}

func TestDoTrailingSlashAndPathTargets(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
	}))
	defer srv.Close()

	d := delivery.Delivery{Method: "POST", Path: "/hook", Body: []byte("{}")}
	if _, err := Do(context.Background(), srv.URL+"/", d); err != nil {
		t.Fatalf("trailing-slash target: %v", err)
	}
	if seenPath != "/hook" {
		t.Errorf("trailing slash: path = %q, want /hook", seenPath)
	}
	if _, err := Do(context.Background(), srv.URL+"/api", d); err != nil {
		t.Fatalf("target with path: %v", err)
	}
	if seenPath != "/api/hook" {
		t.Errorf("target with path: path = %q, want /api/hook", seenPath)
	}
}

func TestDoNoSchemeTargetFailsCleanly(t *testing.T) {
	d := delivery.Delivery{Method: "GET", Path: "/x"}
	if _, err := Do(context.Background(), "localhost:3000", d); err == nil {
		t.Log("FINDING: schemeless target silently works (unexpected)")
	} else if !strings.Contains(err.Error(), "replay") {
		t.Errorf("error not wrapped by replay: %v", err)
	}
}

func TestDoForwardsDuplicateHeaders(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Values("X-Multi")
	}))
	defer srv.Close()

	d := delivery.Delivery{
		Method:  "POST",
		Path:    "/d",
		Headers: map[string][]string{"X-Multi": {"one", "two"}},
		Body:    []byte("{}"),
	}
	if _, err := Do(context.Background(), srv.URL, d); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("duplicate headers forwarded %v", got)
	}
}

func TestToCurlQuotingCorners(t *testing.T) {
	d := delivery.Delivery{
		Method: "POST",
		Path:   "/q?name=O'Brien & Co",
		Headers: map[string][]string{
			"X-Note": {"it's quoted"},
		},
		Body: []byte(`{"s":"single ' and \" double"}`),
	}
	got := ToCurl("http://localhost:4040", d)
	for _, want := range []string{
		"curl -X POST",
		`'http://localhost:4040/q?name=O'\''Brien & Co'`,
		`'X-Note: it'\''s quoted'`,
		`'{"s":"single '\'' and \" double"}'`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ToCurl missing %q in:\n%s", want, got)
		}
	}
}

func TestToCurlGetWithEmptyAndOddBodies(t *testing.T) {
	get := delivery.Delivery{Method: "GET", Path: "/g"}
	if got := ToCurl("http://x", get); strings.Contains(got, "--data-binary") {
		t.Errorf("GET with no body should omit data: %s", got)
	}
	// Body that looks like a flag must stay inside quotes.
	d := delivery.Delivery{Method: "POST", Path: "/f", Body: []byte("-rm -rf")}
	got := ToCurl("http://x", d)
	if !strings.Contains(got, "'-rm -rf'") {
		t.Errorf("flag-like body not quoted: %s", got)
	}
}

func TestToCurlUnicodeBody(t *testing.T) {
	d := delivery.Delivery{Method: "POST", Path: "/u", Body: []byte(`{"привет":"✨"}`)}
	got := ToCurl("http://x", d)
	if !strings.Contains(got, `{"привет":"✨"}`) {
		t.Errorf("unicode body mangled: %s", got)
	}
}
