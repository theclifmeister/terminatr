package modbroker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Fetch limits.
const (
	MaxURL          = 8 << 10
	MaxRequestBody  = 4 << 20
	MaxResponseBody = 8 << 20
	maxRedirects    = 5
	defaultFetch    = 30 * time.Second
	maxFetch        = 60 * time.Second
)

// hostName is a lowercase DNS name of letters, digits and dashes.
var hostName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// HostAllowed reports whether host, a lowercase DNS name, is one of
// hosts: the name itself, or for "*.example.com" any name under
// example.com (not example.com itself).
func HostAllowed(host string, hosts []string) bool {
	for _, h := range hosts {
		h = strings.ToLower(h)
		if suffix, ok := strings.CutPrefix(h, "*"); ok {
			if strings.HasPrefix(suffix, ".") && len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
				return true
			}
		} else if host == h {
			return true
		}
	}
	return false
}

// CheckURL returns raw as the URL a fetch may go to, rebuilt from what
// was checked, or the refusal: HTTPS only, on its own port, no user or
// password in it, and a host name (never an address) that hosts allows.
func CheckURL(raw string, hosts []string) (*url.URL, error) {
	if len(raw) > MaxURL {
		return nil, BadParams("the URL is over %d bytes", MaxURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, BadParams("%q isn't a URL", raw)
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Opaque != "" {
		return nil, Denied("%s: only https:// URLs are fetched", clip(raw))
	}
	if u.User != nil {
		return nil, Denied("%s: no user or password in the URL", clip(raw))
	}
	if p := u.Port(); p != "" && p != "443" {
		return nil, Denied("%s: only the HTTPS port, 443", clip(raw))
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if !hostName.MatchString(host) {
		return nil, Denied("%s: %q isn't a host name the mod may reach", clip(raw), u.Hostname())
	}
	if !HostAllowed(host, hosts) {
		return nil, Denied("%s isn't one of the mod's net hosts", host)
	}
	out := &url.URL{Scheme: "https", Host: host, Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
	return out, nil
}

func clip(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

// PublicAddr reports whether a fetch may connect to a: never a loopback,
// private, link-local (cloud metadata), shared (CGNAT), multicast or
// unspecified address, so a host name that resolves to one (or is made
// to) reaches nothing on the user's machine or network.
func PublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	switch {
	case !a.IsValid(), a.IsLoopback(), a.IsPrivate(), a.IsUnspecified(), a.IsMulticast(),
		a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast(), a.IsInterfaceLocalMulticast():
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// NetOptions change how fetches reach the network, for tests.
type NetOptions struct {
	// Addr says whether an address may be connected to (PublicAddr).
	Addr func(netip.Addr) bool
	// Resolve replaces a host:port address before it is looked up.
	Resolve func(address string) string
	// TLS is the client's TLS configuration (a test server's roots).
	TLS *tls.Config
	// Dial connects to a checked address instead of the network.
	Dial func(ctx context.Context, network string, ap netip.AddrPort) (net.Conn, error)
}

// fetcher is the broker's HTTPS client.
type fetcher struct {
	o NetOptions
	t *http.Transport
}

func newFetcher(o *NetOptions) *fetcher {
	f := &fetcher{}
	if o != nil {
		f.o = *o
	}
	if f.o.Addr == nil {
		f.o.Addr = PublicAddr
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	f.t = &http.Transport{
		// The environment's proxy isn't used: the dial check must see the
		// address the request goes to.
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			ap, err := f.resolve(ctx, address)
			if err != nil {
				return nil, err
			}
			if f.o.Dial != nil {
				return f.o.Dial(ctx, network, ap)
			}
			return d.DialContext(ctx, network, ap.String())
		},
		TLSClientConfig:        f.o.TLS,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  maxFetch,
		MaxResponseHeaderBytes: 64 << 10,
		ForceAttemptHTTP2:      true,
		IdleConnTimeout:        30 * time.Second,
	}
	return f
}

// resolve looks host:port up and returns its first address a fetch may
// connect to. The dial goes to that address, so what was checked is what
// is reached (no second lookup to rebind).
func (f *fetcher) resolve(ctx context.Context, address string) (netip.AddrPort, error) {
	if f.o.Resolve != nil {
		address = f.o.Resolve(address)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return netip.AddrPort{}, err
	}
	pn, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("port %q: %v", port, err)
	}
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else if addrs, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host); err != nil {
		return netip.AddrPort{}, err
	}
	for _, a := range addrs {
		if f.o.Addr(a) {
			return netip.AddrPortFrom(a.Unmap(), uint16(pn)), nil
		}
	}
	return netip.AddrPort{}, fmt.Errorf("%s has no public address", host)
}

var fetchMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}

// FetchResult is what http.fetch answers.
type FetchResult struct {
	Status  int               `json:"status"`
	URL     string            `json:"url"` // after redirects
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// FetchOptions are http.fetch's options.
type FetchOptions struct {
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	TimeoutMS int               `json:"timeout_ms"`
}

// http.fetch {url, options: {method, headers, body, timeout_ms}} (or the
// options flat): HTTPS to the mod's net hosts, redirects only to them.
func (b *Broker) fetch(ctx context.Context, c *Call) (any, error) {
	var a struct {
		URL string `json:"url"`
		FetchOptions
		Options *FetchOptions `json:"options"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	if a.Options != nil {
		a.FetchOptions = *a.Options
	}
	var hosts []string
	if c.Mod.Manifest != nil {
		hosts = c.Mod.Manifest.Permissions.Net
	}
	u, err := CheckURL(a.URL, hosts)
	if err != nil {
		return nil, err
	}
	meth := strings.ToUpper(a.Method)
	if meth == "" {
		meth = "GET"
	}
	if !slices.Contains(fetchMethods, meth) {
		return nil, BadParams("http.fetch: method %q isn't one of %s", a.Method, strings.Join(fetchMethods, ", "))
	}
	if len(a.Body) > MaxRequestBody {
		return nil, BadParams("http.fetch: the body is over %d bytes", MaxRequestBody)
	}
	timeout := defaultFetch
	if a.TimeoutMS > 0 {
		timeout = min(time.Duration(a.TimeoutMS)*time.Millisecond, maxFetch)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	req, err := http.NewRequestWithContext(ctx, meth, u.String(), body)
	if err != nil {
		return nil, BadParams("http.fetch: %v", err)
	}
	for k, v := range a.Headers {
		if strings.EqualFold(k, "Host") || !validHeader(k, v) {
			return nil, BadParams("http.fetch: header %q isn't allowed", k)
		}
		req.Header.Set(k, v)
	}
	cl := &http.Client{Transport: b.net.t, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return Denied("http.fetch: over %d redirects", maxRedirects)
		}
		if _, err := CheckURL(r.URL.String(), hosts); err != nil {
			return Denied("http.fetch: a redirect to %s: %v", r.URL.Host, err)
		}
		return nil
	}}
	c.Audit("http.fetch %s %s", meth, u.Redacted())
	resp, err := cl.Do(req)
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			return nil, e
		}
		return nil, Failed("http.fetch %s: %v", u.Host, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBody+1))
	if err != nil {
		return nil, Failed("http.fetch %s: %v", u.Host, err)
	}
	if len(data) > MaxResponseBody {
		return nil, Failed("http.fetch %s: the answer is over %d bytes", u.Host, MaxResponseBody)
	}
	res := FetchResult{Status: resp.StatusCode, URL: resp.Request.URL.String(), Headers: map[string]string{}, Body: string(data)}
	for k := range resp.Header {
		res.Headers[strings.ToLower(k)] = resp.Header.Get(k)
	}
	return res, nil
}

// validHeader: a token name and a value without line breaks.
func validHeader(k, v string) bool {
	if k == "" || len(k)+len(v) > 8<<10 || strings.ContainsAny(v, "\r\n\x00") {
		return false
	}
	for _, r := range k {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune(`"(),/:;<=>?@[\]{}`, r) {
			return false
		}
	}
	return true
}
