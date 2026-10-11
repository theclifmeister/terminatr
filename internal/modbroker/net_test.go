package modbroker

import (
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

var testHosts = []string{"api.example.com", "*.svc.example.org"}

func TestCheckURL(t *testing.T) {
	cases := []struct {
		raw  string
		want string // "" refused
	}{
		{"https://api.example.com/v1/x?q=1", "https://api.example.com/v1/x?q=1"},
		{"HTTPS://API.Example.COM./v1", "https://api.example.com/v1"},
		{"https://api.example.com:443/", "https://api.example.com/"},
		{"https://a.svc.example.org/", "https://a.svc.example.org/"},
		{"https://a.b.svc.example.org/", "https://a.b.svc.example.org/"},
		{"https://svc.example.org/", ""},
		{"https://xsvc.example.org/", ""},
		{"http://api.example.com/", ""},
		{"https://api.example.com:8443/", ""},
		{"https://user:pw@api.example.com/", ""},
		{"https://api.example.com@evil.com/", ""},
		{"https://evil.com#@api.example.com/", ""},
		{"https://evil.com\\@api.example.com/", ""},
		{"https://api.example.com.evil.com/", ""},
		{"https://127.0.0.1/", ""},
		{"https://[::1]/", ""},
		{"https://0x7f000001/", ""},
		{"https://api%2eexample.com/", ""},
		{"https:api.example.com", ""},
		{"//api.example.com/", ""},
		{"https://api.example.com%00.evil.com/", ""},
		{"https://аpi.example.com/", ""}, // a Cyrillic a
		{"file:///etc/passwd", ""},
		{"https://api.example.com/" + strings.Repeat("a", MaxURL), ""},
	}
	for _, c := range cases {
		u, err := CheckURL(c.raw, testHosts)
		got := ""
		if err == nil {
			got = u.String()
		}
		if got != c.want {
			t.Errorf("CheckURL(%q) = %q, %v; want %q", c.raw, got, err, c.want)
		}
	}
}

func TestHostAllowed(t *testing.T) {
	for _, c := range []struct {
		host string
		ok   bool
	}{
		{"api.example.com", true}, {"x.api.example.com", false}, {"example.com", false},
		{"svc.example.org", false}, {"a.svc.example.org", true}, {".svc.example.org", false},
	} {
		if got := HostAllowed(c.host, testHosts); got != c.ok {
			t.Errorf("HostAllowed(%q) = %v", c.host, got)
		}
	}
}

func TestPublicAddr(t *testing.T) {
	for _, c := range []struct {
		addr string
		ok   bool
	}{
		{"93.184.216.34", true}, {"2606:2800:220:1::1", true},
		{"127.0.0.1", false}, {"10.1.2.3", false}, {"172.16.0.1", false}, {"192.168.1.1", false},
		{"169.254.169.254", false}, {"100.64.0.1", false}, {"0.0.0.0", false}, {"::1", false},
		{"::", false}, {"fd00::1", false}, {"fe80::1", false}, {"::ffff:127.0.0.1", false},
		{"::ffff:10.0.0.1", false}, {"224.0.0.1", false}, {"255.255.255.255", false},
	} {
		if got := PublicAddr(netip.MustParseAddr(c.addr)); got != c.ok {
			t.Errorf("PublicAddr(%s) = %v", c.addr, got)
		}
	}
}

// FuzzCheckURL: an accepted URL is HTTPS on 443 to an allowed host name,
// carries no user, and every reader of it (url.Parse again, net/http's
// request) sees the same host: no parser difference reaches another.
func FuzzCheckURL(f *testing.F) {
	for _, s := range []string{
		"https://api.example.com/v1", "https://a.svc.example.org/x?y#z", "https://user@api.example.com/",
		"https://api.example.com@evil.com/", "https://evil.com\\@api.example.com", "HTTPS://API.EXAMPLE.COM.:443/",
		"https://[::1]:443/", "https://api.example.com%2f@evil.com/", "https://api.example.com\t/",
		"https://api.example.com/%2e%2e/", "https://x.svc.example.org./a/../b",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		u, err := CheckURL(raw, testHosts)
		if err != nil {
			return
		}
		host := u.Hostname()
		if u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Host != host {
			t.Fatalf("CheckURL(%q) = %s: not plain https", raw, u)
		}
		if host != "api.example.com" && !(strings.HasSuffix(host, ".svc.example.org") && len(host) > len(".svc.example.org")) {
			t.Fatalf("CheckURL(%q) = %s: host %q isn't allowed", raw, u, host)
		}
		again, err := url.Parse(u.String())
		if err != nil || again.Host != host || again.User != nil || again.Scheme != "https" {
			t.Fatalf("CheckURL(%q) = %s, which parses again as %v (%v)", raw, u, again, err)
		}
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil || req.URL.Host != host || req.Host != host {
			t.Fatalf("CheckURL(%q) = %s; net/http would reach %v (%v)", raw, u, req, err)
		}
	})
}
