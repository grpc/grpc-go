/*
 *
 * Copyright 2020 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package sts implements call credentials using STS (Security Token Service) as
// defined in https://tools.ietf.org/html/rfc8693.
//
// Experimental
//
// Notice: All APIs in this package are experimental and may be changed or
// removed in a later release.
package sts

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"

	grpcbackoff "google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
)

const (
	// HTTP request timeout set on the http.Client used to make STS requests.
	stsRequestTimeout = 5 * time.Second
	// If lifetime left in a cached token is lesser than this value, we fetch a
	// new one instead of returning the current one.
	minCachedTokenLifetime = 300 * time.Second

	tokenExchangeGrantType    = "urn:ietf:params:oauth:grant-type:token-exchange"
	defaultCloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
)

// For overriding in tests.
var (
	loadSystemCertPool   = x509.SystemCertPool
	makeBackoffStrategy  = defaultBackoffStrategy
	makeHTTPDoer         = makeHTTPClient
	readSubjectTokenFrom = ioutil.ReadFile
	readActorTokenFrom   = ioutil.ReadFile
)

// Options configures the parameters used for an STS based token exchange.
type Options struct {
	// TokenExchangeServiceURI is the address of the server which implements STS
	// token exchange functionality.
	TokenExchangeServiceURI string // Required.

	// Resource is a URI that indicates the target service or resource where the
	// client intends to use the requested security token.
	Resource string // Optional.

	// Audience is the logical name of the target service where the client
	// intends to use the requested security token
	Audience string // Optional.

	// Scope is a list of space-delimited, case-sensitive strings, that allow
	// the client to specify the desired scope of the requested security token
	// in the context of the service or resource where the token will be used.
	// If this field is left unspecified, a default value of
	// https://www.googleapis.com/auth/cloud-platform will be used.
	Scope string // Optional.

	// RequestedTokenType is an identifier, as described in
	// https://tools.ietf.org/html/rfc8693#section-3, that indicates the type of
	// the requested security token.
	RequestedTokenType string // Optional.

	// SubjectTokenPath is a filesystem path which contains the security token
	// that represents the identity of the party on behalf of whom the request
	// is being made.
	SubjectTokenPath string // Required.

	// SubjectTokenType is an identifier, as described in
	// https://tools.ietf.org/html/rfc8693#section-3, that indicates the type of
	// the security token in the "subject_token_path" parameter.
	SubjectTokenType string // Required.

	// ActorTokenPath is a  security token that represents the identity of the
	// acting party.
	ActorTokenPath string // Optional.

	// ActorTokenType is an identifier, as described in
	// https://tools.ietf.org/html/rfc8693#section-3, that indicates the type of
	// the the security token in the "actor_token_path" parameter.
	ActorTokenType string // Optional.
}

// NewCredentials returns a new PerRPCCredentials implementation, configured
// using opts, which performs token exchange using STS.
func NewCredentials(opts Options) (credentials.PerRPCCredentials, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}

	// Load the system roots to validate the certificate presented by the STS
	// endpoint during the TLS handshake.
	roots, err := loadSystemCertPool()
	if err != nil {
		return nil, err
	}

	return &callCreds{
		opts:   opts,
		client: makeHTTPDoer(roots),
		bs:     makeBackoffStrategy(),
	}, nil
}

// defaultBackoffStrategy returns an exponential backoff strategy based on the
// default exponential backoff config, but with a smaller BaseDelay (because the
// former is meant more for connection establishment).
func defaultBackoffStrategy() backoff.Strategy {
	config := grpcbackoff.DefaultConfig
	config.BaseDelay = 100 * time.Millisecond
	return backoff.Exponential{Config: config}
}

// httpDoer wraps the single method on the http.Client type that we use. This
// helps with overriding in unittests.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func makeHTTPClient(roots *x509.CertPool) httpDoer {
	return &http.Client{
		Timeout: stsRequestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: roots,
			},
		},
	}
}

// validateOptions performs the following validation checks on opts:
// - tokenExchangeServiceURI is not empty
// - tokenExchangeServiceURI is a valid URI with a http(s) scheme
// - subjectTokenPath and subjectTokenType are not empty.
func validateOptions(opts Options) error {
	if opts.TokenExchangeServiceURI == "" {
		return errors.New("empty token_exchange_service_uri in options")
	}
	u, err := url.Parse(opts.TokenExchangeServiceURI)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme is not supported: %s. Only http(s) is supported", u.Scheme)
	}

	if opts.SubjectTokenPath == "" {
		return errors.New("required field SubjectTokenPath is not specified")
	}
	if opts.SubjectTokenType == "" {
		return errors.New("required field SubjectTokenType is not specified")
	}
	return nil
}

// callCreds provides the implementation of call credentials based on an STS
// token exchange.
type callCreds struct {
	opts   Options
	client httpDoer
	bs     backoff.Strategy

	// Cached accessToken to avoid an STS token exchange for every call to
	// GetRequestMetadata.
	mu          sync.Mutex
	cachedToken *tokenInfo
}

// GetRequestMetadata returns the cached accessToken, if available and valid, or
// fetches a new one by performing an STS token exchange.
func (c *callCreds) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	if md := c.metadataFromCachedToken(); md != nil {
		return md, nil
	}
	req, err := c.constructRequest()
	if err != nil {
		return nil, err
	}

	// Send the request with exponential backoff and retry. Even though not
	// retrying here is OK, as the connection attempt will be retried by the
	// subConn, it is not very hard to perform some basic retries here.
	respBody, err := c.sendRequestWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}

	ti, err := tokenInfoFromResponse(respBody)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cachedToken = ti
	c.mu.Unlock()

	return map[string]string{
		"Authorization": fmt.Sprintf("%s %s", ti.tokenType, ti.token),
	}, nil
}

// metadataFromCachedToken returns the cached accessToken as request metadata,
// provided a cached accessToken exists and is not going to expire anytime soon.
func (c *callCreds) metadataFromCachedToken() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedToken == nil {
		return nil
	}

	now := time.Now()
	// If the cached token has not expired and the lifetime remaining on that
	// token is greater than the minimum value we are willing to accept, go
	// ahead and use it.
	if c.cachedToken.expiryTime.After(now) && c.cachedToken.expiryTime.Sub(now) > minCachedTokenLifetime {
		return map[string]string{
			"Authorization": fmt.Sprintf("%s %s", c.cachedToken.tokenType, c.cachedToken.token),
		}
	}
	return nil
}

// constructRequest creates the STS request body in JSON based on the options
// received when the credentials type was created.
// - Contents of the subjectToken are read from the file specified in
//   options. If we encounter an error here, we bail out.
// - Contents of the actorToken are read from the file specified in options.
//   If we encounter an error here, we ignore this field because this is
//   optional.
// - Most of the other fields in the request come directly from options.
func (c *callCreds) constructRequest() (*http.Request, error) {
	subToken, err := readSubjectTokenFrom(c.opts.SubjectTokenPath)
	if err != nil {
		return nil, err
	}
	reqScope := c.opts.Scope
	if reqScope == "" {
		reqScope = defaultCloudPlatformScope
	}
	reqParams := &RequestParameters{
		GrantType:          tokenExchangeGrantType,
		Resource:           c.opts.Resource,
		Audience:           c.opts.Audience,
		Scope:              reqScope,
		RequestedTokenType: c.opts.RequestedTokenType,
		SubjectToken:       string(subToken),
		SubjectTokenType:   c.opts.SubjectTokenType,
	}
	actorToken, err := readActorTokenFrom(c.opts.ActorTokenPath)
	if err == nil {
		reqParams.ActorToken = string(actorToken)
		reqParams.ActorTokenType = c.opts.ActorTokenType
	}
	jsonBody, err := json.Marshal(reqParams)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", c.opts.TokenExchangeServiceURI, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// sendRequestWithRetry sends the provided http.Request and retries with
// exponential backoff for certain types of errors. It takes care of closing the
// response body on success (returns the contents of the body) and retries.
func (c *callCreds) sendRequestWithRetry(ctx context.Context, req *http.Request) ([]byte, error) {
	retries := 0
	for {
		// Wait for one of exponential backoff or context deadline to expire.
		if retries > 0 {
			timer := time.NewTimer(c.bs.Backoff(retries))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		// http.Client returns a non-nil error only if it encounters an error
		// caused by client policy (such as CheckRedirect), or failure to speak
		// HTTP (such as a network connectivity problem). A non-2xx status code
		// doesn't cause an error.
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}

		// When the http.Client returns a non-nil error, it is the
		// responsibility of the caller to read the response body till an EOF is
		// encountered and to close it.
		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			retries++
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}
		if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
			// For 4xx errors, which are client errors, we do not retry.
			return nil, fmt.Errorf("http status %d, body: %s", resp.StatusCode, string(body))
		}
		retries++
		grpclog.Warningf("http status %d, body: %s", resp.StatusCode, string(body))
	}
}

func tokenInfoFromResponse(respBody []byte) (*tokenInfo, error) {
	respData := &ResponseParameters{}
	if err := json.Unmarshal(respBody, respData); err != nil {
		return nil, fmt.Errorf("json.Unmarshal(%v): %v", respBody, err)
	}
	if respData.AccessToken == "" {
		return nil, fmt.Errorf("empty accessToken in response (%v)", string(respBody))
	}
	return &tokenInfo{
		tokenType:  respData.TokenType,
		token:      respData.AccessToken,
		expiryTime: time.Now().Add(time.Duration(respData.ExpiresIn) * time.Second),
	}, nil
}

// RequireTransportSecurity indicates whether the credentials requires
// transport security.
func (c *callCreds) RequireTransportSecurity() bool {
	return true
}

// RequestParameters stores all STS request attributes defined in
// https://tools.ietf.org/html/rfc8693#section-2.1.
type RequestParameters struct {
	// REQUIRED. The value "urn:ietf:params:oauth:grant-type:token-exchange"
	// indicates that a token exchange is being performed.
	GrantType string `json:"grant_type"`
	// OPTIONAL. Indicates the location of the target service or resource where
	// the client intends to use the requested security token.
	Resource string `json:"resource"`
	// OPTIONAL. The logical name of the target service where the client intends
	// to use the requested security token.
	Audience string `json:"audience"`
	// OPTIONAL. A list of space-delimited, case-sensitive strings, that allow
	// the client to specify the desired scope of the requested security token
	// in the context of the service or Resource where the token will be used.
	Scope string `json:"scope"`
	// OPTIONAL. An identifier, for the type of the requested security token.
	RequestedTokenType string `json:"requested_token_type"`
	// REQUIRED. A security token that represents the identity of the party on
	// behalf of whom the request is being made.
	SubjectToken string `json:"subject_token"`
	// REQUIRED. An identifier, that indicates the type of the security token in
	// the "subject_token" parameter.
	SubjectTokenType string `json:"subject_token_type"`
	// OPTIONAL. A security token that represents the identity of the acting
	// party.
	ActorToken string `json:"actor_token"`
	// An identifier, that indicates the type of the security token in the
	// "actor_token" parameter.
	ActorTokenType string `json:"actor_token_type"`
}

// ResponseParameters stores all attributes sent as JSON in a successful STS
// response. These attributes are defined in
// https://tools.ietf.org/html/rfc8693#section-2.2.1.
type ResponseParameters struct {
	// REQUIRED. The security token issued by the authorization server
	// in response to the token exchange request.
	AccessToken string `json:"access_token"`
	// REQUIRED. An identifier, representation of the issued security token.
	IssuedTokenType string `json:"issued_token_type"`
	// REQUIRED. A case-insensitive value specifying the method of using the access
	// token issued. It provides the client with information about how to utilize the
	// access token to access protected resources.
	TokenType string `json:"token_type"`
	// RECOMMENDED. The validity lifetime, in seconds, of the token issued by the
	// authorization server.
	ExpiresIn int64 `json:"expires_in"`
	// OPTIONAL, if the Scope of the issued security token is identical to the
	// Scope requested by the client; otherwise, REQUIRED.
	Scope string `json:"scope"`
	// OPTIONAL. A refresh token will typically not be issued when the exchange is
	// of one temporary credential (the subject_token) for a different temporary
	// credential (the issued token) for use in some other context.
	RefreshToken string `json:"refresh_token"`
}

// tokenInfo wraps the information received in a successful STS response.
type tokenInfo struct {
	tokenType  string
	token      string
	expiryTime time.Time
}
