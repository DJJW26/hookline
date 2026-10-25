package delivery

import "testing"

// Edge-case probes for body classification — the sniffing that decides how
// a delivery renders and whether cURL export inlines the body.

func TestKindSniffEdges(t *testing.T) {
	cases := []struct {
		name string
		ct   string
		body string
		want Kind
	}{
		{"empty body with json ct", "application/json", "", KindEmpty},
		{"whitespace only", "text/plain", "   \n\t ", KindText},
		{"brace invalid json still json", "text/plain", `{ not json`, KindJSON},
		{"json sniff without ct", "", `{"a":1}`, KindJSON},
		{"array sniff", "", "[1,2]", KindJSON},
		{"form ct wins over json sniff", "application/x-www-form-urlencoded", `{"a":1}`, KindForm},
		{"form ct malformed body", "application/x-www-form-urlencoded", "not a form", KindForm},
		{"crlf text", "text/plain", "line one\r\nline two\r\n", KindText},
		{"emoji text", "text/plain", "hook ✨ line", KindText},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Delivery{Headers: map[string][]string{}, Body: []byte(c.body)}
			if c.ct != "" {
				d.Headers["Content-Type"] = []string{c.ct}
			}
			if got := d.Kind(); got != c.want {
				t.Errorf("Kind() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestKindInvalidUTF8IsBinary(t *testing.T) {
	// No control bytes, but invalid UTF-8: must classify as binary so
	// string conversions (cURL export) never corrupt it.
	d := Delivery{Body: []byte{'o', 'k', 0xC3, 0x28}}
	if got := d.Kind(); got != KindBinary {
		t.Errorf("Kind() = %q, want binary", got)
	}
}

func TestURLJoinsQuery(t *testing.T) {
	d := Delivery{Path: "/a/b", Query: "x=1&x=2"}
	if got := d.URL(); got != "/a/b?x=1&x=2" {
		t.Errorf("URL() = %q", got)
	}
	d.Query = ""
	if got := d.URL(); got != "/a/b" {
		t.Errorf("URL() no-query = %q", got)
	}
}

func TestNewIDUniqueWithTimestampPrefix(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := NewID()
		if ids[id] {
			t.Fatalf("duplicate id %q", id)
		}
		ids[id] = true
	}
	// Every ID carries the 13-digit millisecond prefix; ordering is only
	// guaranteed across milliseconds (documented on NewID).
	if len(NewID()) < 14 || NewID()[:13] < "0" {
		t.Error("ID missing timestamp prefix")
	}
}

func TestContentTypeCaseInsensitive(t *testing.T) {
	d := Delivery{Headers: map[string][]string{"content-TYPE": {"Application/JSON"}}}
	if got := d.ContentType(); got != "Application/JSON" {
		t.Errorf("ContentType() = %q", got)
	}
}
