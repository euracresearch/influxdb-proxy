// Copyright 2020 Eurac Research. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/influxdata/influxql"
	"golang.org/x/crypto/acme/autocert"
)

var (
	// Build version & commit, injected during build.
	version string
	commit  string

	ErrQueryEmpty        = errors.New("empty query not allowed")
	ErrQueryNotAllowed   = errors.New("query not allowed")
	ErrQueryNotSupported = errors.New("query is not supported")
	ErrMethodNotAllowed  = errors.New("method not allowed")
)

func main() {
	log.SetPrefix("proxy: ")
	log.SetFlags(0)

	var (
		listenAddr   = flag.String("listen", "localhost:8080", "HTTP listen:port address.")
		https        = flag.Bool("https", false, "Serve HTTPS.")
		domain       = flag.String("domain", "", "Domain used for getting LetsEncrypt certificate.")
		cacheDir     = flag.String("cache", ".", "Directory for storing LetsEncrypt certificates.")
		influxAddr   = flag.String("addr", "http://localhost:8086", "InfluxDB server address (protocol://host:port)")
		measurements = flag.String("measurements", "", "Comma seperated list of  allowed measurements.")
	)
	flag.Parse()

	if *measurements == "" {
		log.Fatal("at least one measurement is required")
	}

	p, err := NewProxy(*influxAddr, strings.Split(*measurements, ","))
	if err != nil {
		log.Fatal(err)
	}
	if *https && *domain != "" {
		log.Fatal(serveAutoCert(*listenAddr, p, *cacheDir, *domain))
	}

	log.Printf("listening on %s\n", *listenAddr)
	log.Fatal(http.ListenAndServe(*listenAddr, p))
}

// Proxy denotes a reverse proxy for an InfluxDB HTTP endpoint.
//
// The proxy will check incoming InfluxQL SELECT queries and will proxy them
// only if the data source (measurement), extracted from the FROM field of the
// query is allowed. All other queries will result in an error.
//
//  The proxy supports the following InfluxDB HTTP endpoints:
//  /ping
//  /query
//
type Proxy struct {
	proxy   *httputil.ReverseProxy
	sources []string // allowed data sources. (measurements)
}

// NewProxy creates a new reverse proxy for the given addr and for the allowed
// sources.
func NewProxy(addr string, sources []string) (*Proxy, error) {
	if addr == "" {
		return nil, errors.New("no -addr provided to be proxied to")
	}

	target, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}

	targetQuery := target.RawQuery
	director := func(r *http.Request) {
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.Host = target.Host
		if targetQuery == "" || r.URL.RawQuery == "" {
			r.URL.RawQuery = targetQuery + r.URL.RawQuery
		} else {
			r.URL.RawQuery = targetQuery + "&" + r.URL.RawQuery
		}
		if _, ok := r.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			r.Header.Set("User-Agent", "")
		}
	}

	return &Proxy{
		proxy:   &httputil.ReverseProxy{Director: director},
		sources: sources,
	}, nil
}

// ServeHTTP satisfies the http.Handler interface for a server.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		reportError(w, ErrMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	default:
		http.Error(w, "not found", http.StatusNotFound)
		return

	case "/ping":
		p.proxy.ServeHTTP(w, r)
		return

	case "/write":
		reportError(w, ErrQueryNotSupported, http.StatusNotImplemented)
		return

	case "/query":
		q := r.URL.Query().Get("q")
		if err := allowed(q, p.sources); err != nil {
			reportError(w, err, http.StatusNotAcceptable)
			return
		}

		p.proxy.ServeHTTP(w, r)

	case "/debug/version":
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(version))
		w.Write([]byte("\n"))
		w.Write([]byte(commit))
	}
}

// allowed checks if the query is a SELECT query and it's source (FROM) is allowed
// to be queried. If not an error will be returned.
func allowed(q string, allowed []string) error {
	if q == "" {
		return ErrQueryEmpty
	}

	stmt, err := influxql.NewParser(strings.NewReader(q)).ParseStatement()
	if err != nil {
		return fmt.Errorf("error parsing InfluxQL statement %w", err)
	}

	if !strings.HasPrefix(strings.ToLower(stmt.String()), "select") {
		return ErrQueryNotAllowed
	}

	selectStmt := stmt.(*influxql.SelectStatement)
	for _, m := range selectStmt.Sources.Measurements() {
		if !lookup(allowed, m.Name) {
			return ErrQueryNotAllowed
		}
	}

	return nil
}

// lookup takes a slice and looks for an element in it. If found it will return
// it's key, otherwise it will return -1 and a bool of false. Queries with
// regular expressions are not allowed.
func lookup(allowed []string, name string) bool {
	for _, item := range allowed {
		if item == name {
			return true
		}
	}
	return false
}

// reportError replies to the request with the specified error as encapsulated
// in a JSON object and with the given HTTP code. It does not otherwise end the request; the
// caller should ensure no further writes are done to w.
func reportError(w http.ResponseWriter, err error, code int) {
	var resp = struct {
		Error string `json:"error"`
	}{fmt.Sprintf("%v", err)}

	b, err := json.Marshal(resp)
	if err != nil {
		b = []byte("{\"error\": \"internal server error\"}")
		code = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(code)
	w.Write(b)
}

func redirectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		url := "https://" + r.Host + r.URL.String()
		http.Redirect(w, r, url, http.StatusMovedPermanently)
	})
}

func serveAutoCert(addr string, handler http.Handler, cache string, domains ...string) error {
	go func() {
		host, _, err := net.SplitHostPort(addr)
		if err != nil || host == "" {
			host = "0.0.0.0"
		}
		log.Println("Redirecting traffic from HTTP to HTTPS.")
		log.Fatal(http.ListenAndServe(host+":80", redirectHandler()))
	}()

	m := &autocert.Manager{
		Cache:      autocert.DirCache(cache),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domains...),
	}

	tlsConfig := m.TLSConfig()
	tlsConfig.MinVersion = tls.VersionTLS12
	tlsConfig.CurvePreferences = []tls.CurveID{
		tls.CurveP256,
		tls.X25519, // Go 1.8 only
	}
	tlsConfig.CipherSuites = []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305, // Go 1.8 only
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,   // Go 1.8 only
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	}

	s := &http.Server{
		Addr:      addr,
		Handler:   handler,
		TLSConfig: tlsConfig,
	}

	return s.ListenAndServeTLS("", "")
}
