package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// Serve runs the relay on ln until ctx ends, then closes every
// connection with "shutdown". With certFile and keyFile it serves TLS
// and reloads the pair when the files change (an ACME client renewing
// them); with neither it serves plain HTTP for a TLS proxy in front.
func Serve(ctx context.Context, ln net.Listener, s *Server, certFile, keyFile string) error {
	if certFile != "" {
		cr := &certReloader{cert: certFile, key: keyFile, log: s.logf}
		if _, err := cr.get(nil); err != nil {
			return err
		}
		ln = tls.NewListener(ln, &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: cr.get,
			NextProtos:     []string{"http/1.1"},
		})
	}
	hs := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    8 << 10,
		ErrorLog:          s.cfg.Log,
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
