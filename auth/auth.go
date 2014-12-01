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

/*
Package auth defines Manager type, which is used to fetch auth tokens with
various authentication protocols (e.g., oauth2.0).
*/
package auth

import (
	"sync"

	"oauth2/oauth2"
)

// Manager defines the interface to obtain necessary oauth2.0 information to
// perform RPCs.
type Manager interface {
	// Token tries to get up-to-date access token for an RPC. Non-nil error
	// is returned if it fails to get the valid token.
	Token() (string, error)
}

// oauthManager is used to obtain necessary auth information to perform RPCs.
type oauthManager struct {
	mu sync.Mutex
	t  oauth2.Transport
}

// NewOAuthManager returns a new manager that retrieves tokens from t, refreshing as
// necessary.
func NewOAuthManager(t oauth2.Transport) Manager {
	if t == nil {
		panic("auth.oauthManager got nil auth transport")
	}
	return &oauthManager{t: t}
}

// Token returns the oauth access token or non-nil error if anything goes wrong.
func (m *oauthManager) Token() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok := m.t.Token()
	if tok == nil || tok.Expired() {
		if err := m.t.RefreshToken(); err != nil {
			return "", err
		}
		tok = m.t.Token()
	}
	return tok.AccessToken, nil
}
