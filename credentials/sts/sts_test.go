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

package sts

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
)

const (
	subjectTokenContents = "subjectToken.jwt.contents"
	actorTokenContents   = "actorToken.jwt.contents"
	accessTokenContents  = "access_token"
)

var (
	goodOptions = Options{
		TokenExchangeServiceURI: "http://localhost",
		SubjectTokenPath:        "/var/run/secrets/token.jwt",
		SubjectTokenType:        "urn:ietf:params:oauth:token-type:id_token",
	}
	goodMetadata = map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", accessTokenContents),
	}
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// A struct that implements AuthInfo interface and implements CommonAuthInfo()
// method.
type testAuthInfo struct {
	credentials.CommonAuthInfo
}

func (ta testAuthInfo) AuthType() string {
	return "testAuthInfo"
}

func createTestContext(ctx context.Context, s credentials.SecurityLevel) context.Context {
	auth := &testAuthInfo{CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: s}}
	ri := credentials.RequestInfo{
		Method:   "testInfo",
		AuthInfo: auth,
	}
	return internal.NewRequestInfoContext.(func(context.Context, credentials.RequestInfo) context.Context)(ctx, ri)
}

// fakeHTTPClient helps mock out the HTTP calls made by the credentials code
// under test. It makes the http.Request made by the credentials available
// through a channel, and makes it possible to inject various responses.
type fakeHTTPClient struct {
	reqCh *testutils.Channel
	// When no retry is involve, only these two fields need to be populated.
	firstResp *http.Response
	firstErr  error
	// To test retry scenarios with a different response upon retry.
	subsequentResp *http.Response
	subsequentErr  error

	numCalls int
}

func (fc *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	fc.numCalls++
	fc.reqCh.Send(req)
	if fc.numCalls > 1 {
		return fc.subsequentResp, fc.subsequentErr
	}
	return fc.firstResp, fc.firstErr
}

// fakeBackoff implements backoff.Strategy and pushes on a channel to indicate
// that a backoff was attempted.
type fakeBackoff struct {
	boCh *testutils.Channel
}

func (fb *fakeBackoff) Backoff(retries int) time.Duration {
	fb.boCh.Send(retries)
	return 0
}

// errReader implements the io.Reader interface and returns an error from the
// Read method.
type errReader struct{}

func (r errReader) Read(b []byte) (n int, err error) {
	return 0, errors.New("read error")
}

// We need a function to construct the response instead of simply declaring it
// as a variable since the the response body will be consumed by the
// credentials, and therefore we will need a new one everytime.
func makeGoodResponse() *http.Response {
	respJSON, _ := json.Marshal(ResponseParameters{
		AccessToken:     accessTokenContents,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       3600,
	})
	respBody := ioutil.NopCloser(bytes.NewReader(respJSON))
	return &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Body:       respBody,
	}
}

// Overrides the http.Client with a fakeClient which sends a good response.
func overrideHTTPClientGood() (*fakeHTTPClient, func()) {
	fc := &fakeHTTPClient{
		reqCh:     testutils.NewChannel(),
		firstResp: makeGoodResponse(),
	}
	origMakeHTTPDoer := makeHTTPDoer
	makeHTTPDoer = func(_ *x509.CertPool) httpDoer { return fc }
	return fc, func() { makeHTTPDoer = origMakeHTTPDoer }
}

// Overrides the subject token read to return a const which we can compare in
// our tests.
func overrideSubjectTokenGood() func() {
	origReadSubjectTokenFrom := readSubjectTokenFrom
	readSubjectTokenFrom = func(path string) ([]byte, error) {
		return []byte(subjectTokenContents), nil
	}
	return func() { readSubjectTokenFrom = origReadSubjectTokenFrom }
}

// compareRequestWithRetry is run in a separate goroutine by tests to perform
// the following:
// - wait for a http request to be made by the credentials type and compare it
//   with an expected one.
// - if the credentials is expected to retry, verify that a backoff was done
//   before the retry.
// If any of the above steps fail, an error is pushed on the errCh.
func compareRequestWithRetry(errCh chan error, wantRetry bool, reqCh, boCh *testutils.Channel) {
	val, err := reqCh.Receive()
	if err != nil {
		errCh <- err
		return
	}
	req := val.(*http.Request)
	if err := compareRequest(goodOptions, req); err != nil {
		errCh <- err
		return
	}

	if wantRetry {
		_, err := boCh.Receive()
		if err != nil {
			errCh <- err
			return
		}
	}
	errCh <- nil
}

func compareRequest(opts Options, gotRequest *http.Request) error {
	reqScope := opts.Scope
	if reqScope == "" {
		reqScope = defaultCloudPlatformScope
	}
	reqParams := &RequestParameters{
		GrantType:          tokenExchangeGrantType,
		Resource:           opts.Resource,
		Audience:           opts.Audience,
		Scope:              reqScope,
		RequestedTokenType: opts.RequestedTokenType,
		SubjectToken:       subjectTokenContents,
		SubjectTokenType:   opts.SubjectTokenType,
	}
	if opts.ActorTokenPath != "" {
		reqParams.ActorToken = actorTokenContents
		reqParams.ActorTokenType = opts.ActorTokenType
	}
	jsonBody, err := json.Marshal(reqParams)
	if err != nil {
		return err
	}
	wantReq, err := http.NewRequest("POST", opts.TokenExchangeServiceURI, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create http request: %v", err)
	}
	wantReq.Header.Set("Content-Type", "application/json")

	wantR, err := httputil.DumpRequestOut(wantReq, true)
	if err != nil {
		return err
	}
	gotR, err := httputil.DumpRequestOut(gotRequest, true)
	if err != nil {
		return err
	}
	if diff := cmp.Diff(string(wantR), string(gotR)); diff != "" {
		return fmt.Errorf("sts request diff (-want +got):\n%s", diff)
	}
	return nil
}

// TestGetRequestMetadataSuccess verifies the successful case of sending an
// token exchange request and processing the response.
func (s) TestGetRequestMetadataSuccess(t *testing.T) {
	defer overrideSubjectTokenGood()()
	fc, cancel := overrideHTTPClientGood()
	defer cancel()

	creds, err := NewCredentials(goodOptions)
	if err != nil {
		t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
	}

	errCh := make(chan error, 1)
	go compareRequestWithRetry(errCh, false, fc.reqCh, nil)

	gotMetadata, err := creds.GetRequestMetadata(createTestContext(context.Background(), credentials.PrivacyAndIntegrity), "")
	if err != nil {
		t.Fatalf("creds.GetRequestMetadata() = %v", err)
	}
	if !cmp.Equal(gotMetadata, goodMetadata) {
		t.Fatalf("creds.GetRequestMetadata() = %v, want %v", gotMetadata, goodMetadata)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	// Make another call to get request metadata and this should return contents
	// from the cache. This will fail if the credentials tries to send a fresh
	// request here since we have not configured our fakeClient to return any
	// response on retries.
	gotMetadata, err = creds.GetRequestMetadata(createTestContext(context.Background(), credentials.PrivacyAndIntegrity), "")
	if err != nil {
		t.Fatalf("creds.GetRequestMetadata() = %v", err)
	}
	if !cmp.Equal(gotMetadata, goodMetadata) {
		t.Fatalf("creds.GetRequestMetadata() = %v, want %v", gotMetadata, goodMetadata)
	}
}

// TestGetRequestMetadataBadSecurityLevel verifies the case where the
// securityLevel specified in the context passed to GetRequestMetadata is not
// sufficient.
func (s) TestGetRequestMetadataBadSecurityLevel(t *testing.T) {
	defer overrideSubjectTokenGood()()
	fc, cancel := overrideHTTPClientGood()
	defer cancel()

	creds, err := NewCredentials(goodOptions)
	if err != nil {
		t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
	}

	errCh := make(chan error, 1)
	go compareRequestWithRetry(errCh, false, fc.reqCh, nil)

	gotMetadata, err := creds.GetRequestMetadata(createTestContext(context.Background(), credentials.IntegrityOnly), "")
	if err == nil {
		t.Fatalf("creds.GetRequestMetadata() succeeded with metadata %v, expected to fail", gotMetadata)
	}
}

// TestGetRequestMetadataCacheExpiry verifies the case where the cached access
// token has expired, and the credentials implementation will have to send a
// fresh token exchange request.
func (s) TestGetRequestMetadataCacheExpiry(t *testing.T) {
	const expiresInSecs = 1
	defer overrideSubjectTokenGood()()
	respJSON, _ := json.Marshal(ResponseParameters{
		AccessToken:     accessTokenContents,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       expiresInSecs,
	})
	respBody := ioutil.NopCloser(bytes.NewReader(respJSON))
	resp := &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Body:       respBody,
	}
	fc := &fakeHTTPClient{
		reqCh:          testutils.NewChannel(),
		firstResp:      resp,
		subsequentResp: makeGoodResponse(),
	}
	origMakeHTTPDoer := makeHTTPDoer
	makeHTTPDoer = func(_ *x509.CertPool) httpDoer { return fc }
	defer func() { makeHTTPDoer = origMakeHTTPDoer }()

	creds, err := NewCredentials(goodOptions)
	if err != nil {
		t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
	}

	// The fakeClient is configured to return an access_token with a one second
	// expiry. So, in the second iteration, the credentials will find the cache
	// entry, but that would have expired, and therefore we expect it to send
	// out a fresh request.
	for i := 0; i < 2; i++ {
		errCh := make(chan error, 1)
		go compareRequestWithRetry(errCh, false, fc.reqCh, nil)

		gotMetadata, err := creds.GetRequestMetadata(createTestContext(context.Background(), credentials.PrivacyAndIntegrity), "")
		if err != nil {
			t.Fatalf("creds.GetRequestMetadata() = %v", err)
		}
		if !cmp.Equal(gotMetadata, goodMetadata) {
			t.Fatalf("creds.GetRequestMetadata() = %v, want %v", gotMetadata, goodMetadata)
		}
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		time.Sleep(expiresInSecs * time.Second)
	}
}

// TestGetRequestMetadataBadResponses verifies the scenario where the token
// exchange server returns bad responses.
func (s) TestGetRequestMetadataBadResponses(t *testing.T) {
	tests := []struct {
		name     string
		response *http.Response
	}{
		{
			name: "bad JSON",
			response: &http.Response{
				Status:     "200 OK",
				StatusCode: http.StatusOK,
				Body:       ioutil.NopCloser(strings.NewReader("not JSON")),
			},
		},
		{
			name: "no access token",
			response: &http.Response{
				Status:     "200 OK",
				StatusCode: http.StatusOK,
				Body:       ioutil.NopCloser(strings.NewReader("{}")),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer overrideSubjectTokenGood()()

			fc := &fakeHTTPClient{
				reqCh: testutils.NewChannel(),
				firstResp: &http.Response{
					Status:     "200 OK",
					StatusCode: http.StatusOK,
					Body:       ioutil.NopCloser(strings.NewReader("not JSON")),
				},
			}
			origMakeHTTPDoer := makeHTTPDoer
			makeHTTPDoer = func(_ *x509.CertPool) httpDoer { return fc }
			defer func() { makeHTTPDoer = origMakeHTTPDoer }()

			creds, err := NewCredentials(goodOptions)
			if err != nil {
				t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
			}

			errCh := make(chan error, 1)
			go compareRequestWithRetry(errCh, false, fc.reqCh, nil)
			if _, err := creds.GetRequestMetadata(createTestContext(context.Background(), credentials.PrivacyAndIntegrity), ""); err == nil {
				t.Fatal("creds.GetRequestMetadata() succeeded when expected to fail")
			}
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestGetRequestMetadataBadSubjectTokenRead verifies the scenario where the
// attempt to read the subjectToken fails.
func (s) TestGetRequestMetadataBadSubjectTokenRead(t *testing.T) {
	origReadSubjectTokenFrom := readSubjectTokenFrom
	readSubjectTokenFrom = func(path string) ([]byte, error) {
		return nil, errors.New("failed to read subject token")
	}
	defer func() { readSubjectTokenFrom = origReadSubjectTokenFrom }()

	fc, cancel := overrideHTTPClientGood()
	defer cancel()

	creds, err := NewCredentials(goodOptions)
	if err != nil {
		t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
	}

	errCh := make(chan error, 1)
	go func() {
		if _, err := fc.reqCh.Receive(); err != testutils.ErrRecvTimeout {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	if _, err := creds.GetRequestMetadata(createTestContext(context.Background(), credentials.PrivacyAndIntegrity), ""); err == nil {
		t.Fatal("creds.GetRequestMetadata() succeeded when expected to fail")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

// TestGetRequestMetadataRetry verifies various retry scenarios.
func (s) TestGetRequestMetadataRetry(t *testing.T) {
	tests := []struct {
		name           string
		firstResp      *http.Response
		firstErr       error
		subsequentResp *http.Response
		subsequentErr  error
		wantRetry      bool
		wantErr        bool
		wantMetadata   map[string]string
	}{
		{
			name:     "httpClient.Do error",
			firstErr: errors.New("httpClient.Do() failed"),
			wantErr:  true,
		},
		{
			name: "bad response body first time",
			firstResp: &http.Response{
				Status:     "200 OK",
				StatusCode: http.StatusOK,
				Body:       ioutil.NopCloser(errReader{}),
			},
			subsequentResp: makeGoodResponse(),
			wantRetry:      true,
			wantMetadata:   goodMetadata,
		},
		{
			name: "http client error status code",
			firstResp: &http.Response{
				Status:     "400 BadRequest",
				StatusCode: http.StatusBadRequest,
				Body:       ioutil.NopCloser(&bytes.Reader{}),
			},
			wantErr: true,
		},
		{
			name: "server error first time",
			firstResp: &http.Response{
				Status:     "400 BadRequest",
				StatusCode: http.StatusInternalServerError,
				Body:       ioutil.NopCloser(&bytes.Reader{}),
			},
			subsequentResp: makeGoodResponse(),
			wantRetry:      true,
			wantMetadata:   goodMetadata,
		},
	}

	// The test body performs the following steps:
	// 1. Overrides the function to read subjectToken file and returns arbitrary
	//    data and nil error.
	// 2. Overrides the function to return a http.Client and returns a fake
	//    client which is configured with response/error values to be returned.
	// 3. Overrides the function to create the backoff strategy and returns a
	//    fake implementation which notifies the test through a channel when
	//    backoff is attempted.
	// 4. Creates a new credentials type and invokes the GetRequestMetadata
	//    method on it.
	// 5. Spawn a goroutine which verifies that the credentials sent out the
	//    expected http.Request, and performed a backoff when it encountered
	//    certain errors.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer overrideSubjectTokenGood()()

			fc := &fakeHTTPClient{
				reqCh:          testutils.NewChannel(),
				firstResp:      test.firstResp,
				firstErr:       test.firstErr,
				subsequentResp: test.subsequentResp,
				subsequentErr:  test.subsequentErr,
			}
			origMakeHTTPDoer := makeHTTPDoer
			makeHTTPDoer = func(_ *x509.CertPool) httpDoer { return fc }

			origBackoff := makeBackoffStrategy
			fb := &fakeBackoff{boCh: testutils.NewChannel()}
			makeBackoffStrategy = func() backoff.Strategy { return fb }

			defer func() {
				makeHTTPDoer = origMakeHTTPDoer
				makeBackoffStrategy = origBackoff
			}()

			creds, err := NewCredentials(goodOptions)
			if err != nil {
				t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
			}

			errCh := make(chan error, 1)
			go compareRequestWithRetry(errCh, test.wantRetry, fc.reqCh, fb.boCh)

			gotMetadata, err := creds.GetRequestMetadata(createTestContext(context.Background(), credentials.PrivacyAndIntegrity), "")
			if (err != nil) != test.wantErr {
				t.Fatalf("creds.GetRequestMetadata() = %v, want %v", err, test.wantErr)
			}
			if !cmp.Equal(gotMetadata, test.wantMetadata, cmpopts.EquateEmpty()) {
				t.Fatalf("creds.GetRequestMetadata() = %v, want %v", gotMetadata, test.wantMetadata)
			}
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func (s) TestNewCredentials(t *testing.T) {
	tests := []struct {
		name           string
		opts           Options
		errSystemRoots bool
		wantErr        bool
	}{
		{
			name: "invalid options - empty subjectTokenPath",
			opts: Options{
				TokenExchangeServiceURI: "http://localhost",
			},
			wantErr: true,
		},
		{
			name:           "invalid system root certs",
			opts:           goodOptions,
			errSystemRoots: true,
			wantErr:        true,
		},
		{
			name: "good case",
			opts: goodOptions,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.errSystemRoots {
				oldSystemRoots := loadSystemCertPool
				loadSystemCertPool = func() (*x509.CertPool, error) {
					return nil, errors.New("failed to load system cert pool")
				}
				defer func() {
					loadSystemCertPool = oldSystemRoots
				}()
			}

			creds, err := NewCredentials(test.opts)
			if (err != nil) != test.wantErr {
				t.Fatalf("NewCredentials(%v) = %v, want %v", test.opts, err, test.wantErr)
			}
			if err == nil {
				if !creds.RequireTransportSecurity() {
					t.Errorf("creds.RequireTransportSecurity() returned false")
				}
			}
		})
	}
}

func (s) TestValidateOptions(t *testing.T) {
	tests := []struct {
		name          string
		opts          Options
		wantErrPrefix string
	}{
		{
			name:          "empty token exchange service URI",
			opts:          Options{},
			wantErrPrefix: "empty token_exchange_service_uri in options",
		},
		{
			name: "invalid URI",
			opts: Options{
				TokenExchangeServiceURI: "\tI'm a bad URI\n",
			},
			wantErrPrefix: "invalid control character in URL",
		},
		{
			name: "unsupported scheme",
			opts: Options{
				TokenExchangeServiceURI: "unix:///path/to/socket",
			},
			wantErrPrefix: "scheme is not supported",
		},
		{
			name: "empty subjectTokenPath",
			opts: Options{
				TokenExchangeServiceURI: "http://localhost",
			},
			wantErrPrefix: "required field SubjectTokenPath is not specified",
		},
		{
			name: "empty subjectTokenType",
			opts: Options{
				TokenExchangeServiceURI: "http://localhost",
				SubjectTokenPath:        "/var/run/secrets/token.jwt",
			},
			wantErrPrefix: "required field SubjectTokenType is not specified",
		},
		{
			name: "good options",
			opts: Options{
				TokenExchangeServiceURI: "http://localhost",
				SubjectTokenPath:        "/var/run/secrets/token.jwt",
				SubjectTokenType:        "urn:ietf:params:oauth:token-type:id_token",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOptions(test.opts)
			if (err != nil) != (test.wantErrPrefix != "") {
				t.Errorf("validateOptions(%v) = %v, want %v", test.opts, err, test.wantErrPrefix)
			}
			if err != nil && !strings.Contains(err.Error(), test.wantErrPrefix) {
				t.Errorf("validateOptions(%v) = %v, want %v", test.opts, err, test.wantErrPrefix)
			}
		})
	}
}
