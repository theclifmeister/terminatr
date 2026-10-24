package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// TLS says where the relay's certificate comes from. The zero value
// serves plain HTTP, for a TLS proxy in front.
type TLS struct {
	// CertFile and KeyFile are a certificate pair on disk, reloaded when
	// the files change (an ACME client renewing them).
	CertFile, KeyFile string
	// Domain gets the certificate from Let's Encrypt for this name by
	// TLS-ALPN-01 on the relay's own port (443 as the CA sees it), kept
	// in CacheDir and renewed by itself before it expires.
	Domain   string
	CacheDir string
	// Email is given to the CA for expiry notices; optional.
	Email string
	// Directory is the ACME directory URL; empty is Let's Encrypt's.
	Directory string
}

// httpTimeouts is the HTTP side's deadlines. The WebSocket clears them
// once upgraded (upgrade) and sets its own per frame.
type httpTimeouts struct {
	header    time.Duration // the request's headers
	handshake time.Duration // the TLS handshake, and writing an answer
	idle      time.Duration // between requests on one connection
}

var defaultHTTPTimeouts = httpTimeouts{header: 10 * time.Second, handshake: 5 * time.Second, idle: 30 * time.Second}

// acceptLimits is how many TCP connections the listener holds, in all
// and from one address: the WebSocket limits and some room for /healthz
// and answers that refuse.
func (c Config) acceptLimits() (all, perIP int) {
	return c.MaxConns + c.MaxConns/4, c.MaxPerIP + 4
}

// Serve runs the relay on ln until ctx ends, then closes every
// connection with "shutdown", serving TLS as t says.
func Serve(ctx context.Context, ln net.Listener, s *Server, t TLS) error {
	cfg, err := t.config(s.logf)
	if err != nil {
		return err
	}
	all, perIP := s.cfg.acceptLimits()
	ln = newLimitListener(ln, all, perIP, s.trusted)
	if cfg != nil {
		ln = tls.NewListener(ln, cfg)
	}
	errLog, stopLog := s.errorLog()
	defer stopLog()
	hs := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: s.http.header,
		ReadTimeout:       s.http.header,
		// net/http gives the TLS handshake the least of its timeouts.
		WriteTimeout:   s.http.handshake,
		IdleTimeout:    s.http.idle,
		MaxHeaderBytes: 8 << 10,
		ErrorLog:       errLog,
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err := <-errc:
		s.Shutdown()
		return err
	case <-ctx.Done():
	}
	s.Shutdown()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hs.Shutdown(sctx)
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// errorLog is net/http's log of failed connections (a TLS handshake
// that went wrong): the relay's log, with scanners' handshake errors
// counted (handshakeNoise) and without client addresses unless LogIPs.
// The func it returns logs what was counted and stops the hourly summary.
func (s *Server) errorLog() (*log.Logger, func()) {
	if s.cfg.Log == nil {
		return log.New(io.Discard, "", 0), func() {}
	}
	n := newHandshakeNoise(func(line string) { s.cfg.Log.Print(line) }, s.cfg.LogIPs, s.now)
	done, ended := make(chan struct{}), make(chan struct{})
	go func() { n.run(done); close(ended) }()
	return log.New(writerFunc(func(p []byte) (int, error) {
		n.line(strings.TrimSuffix(string(p), "\n"))
		return len(p), nil
	}), "", 0), func() { close(done); <-ended }
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// addrLike finds what may be an IP address, with or without a port.
var addrLike = regexp.MustCompile(`\[[0-9A-Fa-f:.%]+\](:[0-9]+)?|[0-9A-Fa-f:.%]*[:.][0-9A-Fa-f:.%]*`)

// hideAddrs replaces the IP addresses in a log line.
func hideAddrs(line string) string {
	return addrLike.ReplaceAllStringFunc(line, func(m string) string {
		ip := strings.TrimRight(m, ":.")
		if ap, err := netip.ParseAddrPort(ip); err == nil && ap.Addr().IsValid() {
			return "(address)" + m[len(ip):]
		}
		if a, err := netip.ParseAddr(strings.Trim(ip, "[]")); err == nil && a.IsValid() {
			return "(address)" + m[len(ip):]
		}
		return m
	})
}

// config is the listener's TLS configuration, nil for plain HTTP.
func (t TLS) config(logf func(string, ...any)) (*tls.Config, error) {
	switch {
	case t.Domain != "":
		if t.CacheDir == "" {
			return nil, errors.New("relay: an ACME certificate needs a state directory")
		}
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(t.CacheDir),
			HostPolicy: autocert.HostWhitelist(t.Domain),
			Email:      t.Email,
		}
		if t.Directory != "" {
			m.Client = &acme.Client{DirectoryURL: t.Directory}
		}
		return &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: m.GetCertificate,
			// No h2: the relay's WebSocket is HTTP/1.1. acme-tls/1 is
			// the CA's challenge connection, answered by GetCertificate.
			NextProtos: []string{"http/1.1", acme.ALPNProto},
		}, nil
	case t.CertFile != "":
		cr := &certReloader{cert: t.CertFile, key: t.KeyFile, log: logf}
		if _, err := cr.get(nil); err != nil {
			return nil, err
		}
		return &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: cr.get,
			NextProtos:     []string{"http/1.1"},
		}, nil
	}
	return nil, nil
}

// certReloader serves a certificate from files and loads it again when
// either file's size or time changes.
type certReloader struct {
	cert, key string
	log       func(string, ...any)

	mu     sync.Mutex
	stamp  [2]fileStamp
	loaded *tls.Certificate
}

type fileStamp struct {
	size int64
	mod  time.Time
}

func stampOf(path string) (fileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{fi.Size(), fi.ModTime()}, nil
}

func (r *certReloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, errA := stampOf(r.cert)
	b, errB := stampOf(r.key)
	if err := errors.Join(errA, errB); err != nil {
		if r.loaded != nil {
			return r.loaded, nil // keep serving the last good pair
		}
		return nil, err
	}
	if r.loaded != nil && r.stamp == [2]fileStamp{a, b} {
		return r.loaded, nil
	}
	c, err := tls.LoadX509KeyPair(r.cert, r.key)
	if err != nil {
		if r.loaded != nil {
			r.log("certificate reload failed, keeping the old one: %v", err)
			return r.loaded, nil
		}
		return nil, err
	}
	if r.loaded != nil {
		r.log("certificate reloaded from %s", r.cert)
	}
	r.stamp, r.loaded = [2]fileStamp{a, b}, &c
	return r.loaded, nil
}
