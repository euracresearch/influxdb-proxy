// Copyright 2020 Eurac Research. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

var (
	testProxy  *httptest.Server
	testClient *http.Client
)

func TestAllowed(t *testing.T) {
	testCases := map[string]struct {
		in      string
		allowed []string
		err     error
	}{
		"empty": {
			"",
			nil,
			ErrQueryEmpty,
		},
		"emptyQuery": {
			"",
			[]string{"a", "b"},
			ErrQueryEmpty,
		},
		"notallowed": {
			"SELECT a, c, b, d, e, mean(a) as m from m1",
			[]string{"m2", "m3"},
			ErrQueryNotAllowed,
		},
		"regex": {
			"select a, c, b, d, e FROM /.*/",
			[]string{"m1"},
			ErrQueryNotAllowed,
		},
		"nestedOK": {
			"select a, c, b, d, e FROM (SELECT * FROM (SELECT * FROM m1)) WHERE a=1",
			[]string{"m0", "m1"},
			nil,
		},
		"nestedNotOK": {
			"select a, c, b FROM (SELECT * FROM m1) GROUP BY time()",
			[]string{"m0", "m4", "m5"},
			ErrQueryNotAllowed,
		},
		"ok": {
			"select a FROM m0",
			[]string{"m0", "m1", "m2"},
			nil,
		},
		"multipleOK": {
			"select a, c, b, d, e FROM m1, m4",
			[]string{"m1", "m2", "m4", "m5"},
			nil,
		},
		"multipleNotOK": {
			"select a, c, b, d, e FROM m1, m4, m0",
			[]string{"m1", "m2", "m4", "m5"},
			ErrQueryNotAllowed,
		},
		"databaseRetentionOK": {
			"select a FROM db.rt.m1",
			[]string{"m0", "m1", "m2"},
			nil,
		},
		"databaseRetentionNotOK": {
			"select a, b FROM db.rt.m4",
			[]string{"m0", "m1", "m2"},
			ErrQueryNotAllowed,
		},
		"databaseOK": {
			"select a, b FROM db..m2",
			[]string{"m0", "m1", "m2"},
			nil,
		},
		"databaseNotOK": {
			"select a, b FROM db..m4",
			[]string{"m0", "m1", "m2"},
			ErrQueryNotAllowed,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			err := allowed(tc.in, tc.allowed)
			if err != tc.err {
				t.Fatalf("got: %v, want: %v", err, tc.err)
			}
		})
	}
}

func TestHTTPMethods(t *testing.T) {
	testCases := []struct {
		method string
		want   int
	}{
		{http.MethodHead, http.StatusMethodNotAllowed},
		{http.MethodPut, http.StatusMethodNotAllowed},
		{http.MethodPatch, http.StatusMethodNotAllowed},
		{http.MethodDelete, http.StatusMethodNotAllowed},
		{http.MethodConnect, http.StatusMethodNotAllowed},
		{http.MethodOptions, http.StatusMethodNotAllowed},
		{http.MethodTrace, http.StatusMethodNotAllowed},
		{http.MethodGet, http.StatusNotFound},
		{http.MethodPost, http.StatusNotFound},
	}

	for id, tc := range testCases {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			req, err := http.NewRequest(tc.method, testProxy.URL, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got, err := testClient.Do(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.StatusCode != tc.want {
				t.Fatalf("got %s, want %d", got.Status, tc.want)
			}
		})
	}

}

func TestDefaultEndpoint(t *testing.T) {
	want := http.StatusNotFound

	got, err := testClient.Get(testProxy.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.StatusCode != want {
		t.Fatalf("got %q, want %q", got.Status, http.StatusText(want))
	}

	got, err = http.Get(testProxy.URL + "/some/more")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.StatusCode != want {
		t.Fatalf("got %q, want %q", got.Status, http.StatusText(want))
	}
}

func TestPingEndpoint(t *testing.T) {
	want := http.StatusOK

	got, err := testClient.Get(testProxy.URL + "/ping")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.StatusCode != want {
		t.Fatalf("got %q, want %q", got.Status, http.StatusText(want))
	}
}

func TestWriteEndpoint(t *testing.T) {
	want := http.StatusNotImplemented

	got, err := testClient.Get(testProxy.URL + "/write")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.StatusCode != want {
		t.Fatalf("got %q, want %q", got.Status, http.StatusText(want))
	}
}

func TestQueryEndpoint(t *testing.T) {
	testCases := map[string]struct {
		query string
		want  int
	}{
		"noquery": {
			query: "",
			want:  http.StatusNotAcceptable,
		},
		"empty": {
			query: "?q=",
			want:  http.StatusNotAcceptable,
		},
		"parital": {
			query: "?q=select",
			want:  http.StatusNotAcceptable,
		},
		"noSelect": {
			query: "?q=drop%20database%20test",
			want:  http.StatusNotAcceptable,
		},
		"ok": {
			query: "?q=select%20*%20FROM%20test",
			want:  http.StatusOK,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got, err := testClient.Get(fmt.Sprintf("%s/query%s", testProxy.URL, tc.query))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.StatusCode != tc.want {
				t.Fatalf("got %q, want %q", got.Status, http.StatusText(tc.want))
			}
		})
	}
}

func TestMain(m *testing.M) {
	// Backend test server we proxy to.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)

	// run proxy server
	p, err := NewProxy(server.URL, []string{"test"})
	if err != nil {
		log.Fatal(err)
	}
	testProxy = httptest.NewServer(p)
	defer testProxy.Close()

	testClient = testProxy.Client()

	// call flag.Parse() here if TestMain uses flags
	os.Exit(m.Run())
}
