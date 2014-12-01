/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"testing"

	"oauth2/oauth2"
)

var req = struct {
	path, query, auth string // request
	contenttype, body string // response
}{
	path:        "/token",
	query:       "grant_type=authorization_code&code=c0d3&client_id=cl13nt1d&client_secret=s3cr3t&scope=math.div",
	contenttype: "application/json",
	body: `
		{
			"access_token":"token1",
			"refresh_token":"refreshtoken1",
			"id_token":"idtoken1",
			"expires_in":3600
		}
	`,
}

var authServer *httptest.Server

func TestOAuth2(t *testing.T) {
	// Set up test auth server.
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Check request.
		if g, w := r.URL.Path, req.path; g != w {
			t.Errorf("http.Request got path %s, want %s", g, w)
		}
		want, _ := url.ParseQuery(req.query)
		for k := range want {
			if g, w := r.FormValue(k), want.Get(k); g != w {
				t.Errorf("query[%s] = %s, want %s", k, g, w)
			}
		}

		// Send response.
		w.Header().Set("Content-Type", req.contenttype)
		io.WriteString(w, req.body)
	}
	authServer = httptest.NewServer(http.HandlerFunc(handler))
	defer func() {
		authServer.Close()
	}()

	config, err := oauth2.NewConfig(&oauth2.Options{
		ClientID:       "cl13nt1d",
		ClientSecret:   "s3cr3t",
		Scopes:         []string{"math.div"},
		RedirectURL:    "redirect_url",
		AccessType:     "offline",
		ApprovalPrompt: "force",
	},
		authServer.URL+"/auth",
		authServer.URL+"/token",
	)
	if err != nil {
		t.Fatalf("Failed to create auth config: %v", err)
	}
	var tp oauth2.Transport
	tp, err = config.NewTransportWithCode("c0d3")
	if err != nil {
		t.Fatalf("Failed to create auth transport: %v", err)
	}
	m := NewOAuthManager(tp)
	tok, err := m.Token()
	if err != nil || tok != "token1" {
		t.Fatalf("auth.Token() got %q, %v; want token1, <nil<", tok, err)
	}
}
