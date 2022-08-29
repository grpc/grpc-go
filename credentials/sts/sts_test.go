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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/interop/grpc_testing"

	"google.golang.org/grpc/testdata"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	icredentials "google.golang.org/grpc/internal/credentials"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
)

const (
	requestedTokenType      = "urn:ietf:params:oauth:token-type:access-token"
	issuedTokenType         = "urn:ietf:params:oauth:token-type:access_token"
	actorTokenPath          = "/var/run/secrets/token.jwt"
	actorTokenType          = "urn:ietf:params:oauth:token-type:refresh_token"
	actorTokenContents      = "actorToken.jwt.contents"
	accessTokenContents     = "access_token"
	subjectTokenPath        = "/var/run/secrets/token.jwt"
	subjectTokenType        = "urn:ietf:params:oauth:token-type:id_token"
	subjectTokenContents    = "subjectToken.jwt.contents"
	serviceURI              = "http://localhost"
	exampleResource         = "https://backend.example.com/api"
	exampleAudience         = "example-backend-service"
	testScope               = "https://www.googleapis.com/auth/monitoring"
	defaultTestTimeout      = 1 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

var (
	goodOptions = Options{
		TokenExchangeServiceURI: serviceURI,
		Audience:                exampleAudience,
		RequestedTokenType:      requestedTokenType,
		SubjectTokenPath:        subjectTokenPath,
		SubjectTokenType:        subjectTokenType,
	}
	goodRequestParams = &requestParameters{
		GrantType:          tokenExchangeGrantType,
		Audience:           exampleAudience,
		Scope:              defaultCloudPlatformScope,
		RequestedTokenType: requestedTokenType,
		SubjectToken:       subjectTokenContents,
		SubjectTokenType:   subjectTokenType,
	}
	goodMetadata = map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", accessTokenContents),
	}
)

// configure a simple gRPC server to test an actual Unary response
type testServer struct {
	grpc_testing.TestServiceServer
}

func newTestServer() *testServer {
	return &testServer{}
}

func (s *testServer) UnaryCall(ctx context.Context, in *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
	resp := &grpc_testing.SimpleResponse{}
	if in.FillUsername {
		resp.Username = "foo"
	}
	return resp, nil
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// A struct that implements AuthInfo interface and added to the context passed
// to GetRequestMetadata from tests.
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
	return icredentials.NewRequestInfoContext(ctx, ri)
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
	respJSON, _ := json.Marshal(responseParameters{
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
func overrideHTTPClientGood() (*testutils.FakeHTTPClient, func()) {
	fc := &testutils.FakeHTTPClient{
		ReqChan:  testutils.NewChannel(),
		RespChan: testutils.NewChannel(),
	}
	fc.RespChan.Send(makeGoodResponse())

	origMakeHTTPDoer := makeHTTPDoer

	makeHTTPDoer = func(_ *http.Client) httpDoer { return fc }
	return fc, func() { makeHTTPDoer = origMakeHTTPDoer }
}

// Overrides the http.Client with the provided fakeClient.
func overrideHTTPClient(fc *testutils.FakeHTTPClient) func() {
	origMakeHTTPDoer := makeHTTPDoer
	makeHTTPDoer = func(_ *http.Client) httpDoer { return fc }
	return func() { makeHTTPDoer = origMakeHTTPDoer }
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

// Overrides the subject token read to always return an error.
func overrideSubjectTokenError() func() {
	origReadSubjectTokenFrom := readSubjectTokenFrom
	readSubjectTokenFrom = func(path string) ([]byte, error) {
		return nil, errors.New("error reading subject token")
	}
	return func() { readSubjectTokenFrom = origReadSubjectTokenFrom }
}

// Overrides the actor token read to return a const which we can compare in
// our tests.
func overrideActorTokenGood() func() {
	origReadActorTokenFrom := readActorTokenFrom
	readActorTokenFrom = func(path string) ([]byte, error) {
		return []byte(actorTokenContents), nil
	}
	return func() { readActorTokenFrom = origReadActorTokenFrom }
}

// Overrides the actor token read to always return an error.
func overrideActorTokenError() func() {
	origReadActorTokenFrom := readActorTokenFrom
	readActorTokenFrom = func(path string) ([]byte, error) {
		return nil, errors.New("error reading actor token")
	}
	return func() { readActorTokenFrom = origReadActorTokenFrom }
}

// compareRequest compares the http.Request received in the test with the
// expected requestParameters specified in wantReqParams.
func compareRequest(gotRequest *http.Request, wantReqParams *requestParameters) error {
	jsonBody, err := json.Marshal(wantReqParams)
	if err != nil {
		return err
	}
	wantReq, err := http.NewRequest("POST", serviceURI, bytes.NewBuffer(jsonBody))
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

// receiveAndCompareRequest waits for a request to be sent out by the
// credentials implementation using the fakeHTTPClient and compares it to an
// expected goodRequest. This is expected to be called in a separate goroutine
// by the tests. So, any errors encountered are pushed to an error channel
// which is monitored by the test.
func receiveAndCompareRequest(ReqChan *testutils.Channel, errCh chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	val, err := ReqChan.Receive(ctx)
	if err != nil {
		errCh <- err
		return
	}
	req := val.(*http.Request)
	if err := compareRequest(req, goodRequestParams); err != nil {
		errCh <- err
		return
	}
	errCh <- nil
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
	go receiveAndCompareRequest(fc.ReqChan, errCh)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	gotMetadata, err := creds.GetRequestMetadata(createTestContext(ctx, credentials.PrivacyAndIntegrity), "")
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
	gotMetadata, err = creds.GetRequestMetadata(createTestContext(ctx, credentials.PrivacyAndIntegrity), "")
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

	creds, err := NewCredentials(goodOptions)
	if err != nil {
		t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotMetadata, err := creds.GetRequestMetadata(createTestContext(ctx, credentials.IntegrityOnly), "")
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
	fc := &testutils.FakeHTTPClient{
		ReqChan:  testutils.NewChannel(),
		RespChan: testutils.NewChannel(),
	}
	defer overrideHTTPClient(fc)()

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
		go receiveAndCompareRequest(fc.ReqChan, errCh)

		respJSON, _ := json.Marshal(responseParameters{
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
		fc.RespChan.Send(resp)

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		gotMetadata, err := creds.GetRequestMetadata(createTestContext(ctx, credentials.PrivacyAndIntegrity), "")
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

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer overrideSubjectTokenGood()()

			fc := &testutils.FakeHTTPClient{
				ReqChan:  testutils.NewChannel(),
				RespChan: testutils.NewChannel(),
			}
			defer overrideHTTPClient(fc)()

			creds, err := NewCredentials(goodOptions)
			if err != nil {
				t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
			}

			errCh := make(chan error, 1)
			go receiveAndCompareRequest(fc.ReqChan, errCh)

			fc.RespChan.Send(test.response)
			if _, err := creds.GetRequestMetadata(createTestContext(ctx, credentials.PrivacyAndIntegrity), ""); err == nil {
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
	defer overrideSubjectTokenError()()
	fc, cancel := overrideHTTPClientGood()
	defer cancel()

	creds, err := NewCredentials(goodOptions)
	if err != nil {
		t.Fatalf("NewCredentials(%v) = %v", goodOptions, err)
	}

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
		defer cancel()
		if _, err := fc.ReqChan.Receive(ctx); err != context.DeadlineExceeded {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := creds.GetRequestMetadata(createTestContext(ctx, credentials.PrivacyAndIntegrity), ""); err == nil {
		t.Fatal("creds.GetRequestMetadata() succeeded when expected to fail")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
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
				TokenExchangeServiceURI: serviceURI,
			},
			wantErr: true,
		},
		{
			name: "good case",
			opts: goodOptions,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

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
				TokenExchangeServiceURI: serviceURI,
			},
			wantErrPrefix: "required field SubjectTokenPath is not specified",
		},
		{
			name: "empty subjectTokenType",
			opts: Options{
				TokenExchangeServiceURI: serviceURI,
				SubjectTokenPath:        subjectTokenPath,
			},
			wantErrPrefix: "required field SubjectTokenType is not specified",
		},
		{
			name: "good options",
			opts: goodOptions,
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

func (s) TestConstructRequest(t *testing.T) {
	tests := []struct {
		name                string
		opts                Options
		subjectTokenReadErr bool
		actorTokenReadErr   bool
		wantReqParams       *requestParameters
		wantErr             bool
	}{
		{
			name:                "subject token read failure",
			subjectTokenReadErr: true,
			opts:                goodOptions,
			wantErr:             true,
		},
		{
			name:              "actor token read failure",
			actorTokenReadErr: true,
			opts: Options{
				TokenExchangeServiceURI: serviceURI,
				Audience:                exampleAudience,
				RequestedTokenType:      requestedTokenType,
				SubjectTokenPath:        subjectTokenPath,
				SubjectTokenType:        subjectTokenType,
				ActorTokenPath:          actorTokenPath,
				ActorTokenType:          actorTokenType,
			},
			wantErr: true,
		},
		{
			name:          "default cloud platform scope",
			opts:          goodOptions,
			wantReqParams: goodRequestParams,
		},
		{
			name: "all good",
			opts: Options{
				TokenExchangeServiceURI: serviceURI,
				Resource:                exampleResource,
				Audience:                exampleAudience,
				Scope:                   testScope,
				RequestedTokenType:      requestedTokenType,
				SubjectTokenPath:        subjectTokenPath,
				SubjectTokenType:        subjectTokenType,
				ActorTokenPath:          actorTokenPath,
				ActorTokenType:          actorTokenType,
			},
			wantReqParams: &requestParameters{
				GrantType:          tokenExchangeGrantType,
				Resource:           exampleResource,
				Audience:           exampleAudience,
				Scope:              testScope,
				RequestedTokenType: requestedTokenType,
				SubjectToken:       subjectTokenContents,
				SubjectTokenType:   subjectTokenType,
				ActorToken:         actorTokenContents,
				ActorTokenType:     actorTokenType,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.subjectTokenReadErr {
				defer overrideSubjectTokenError()()
			} else {
				defer overrideSubjectTokenGood()()
			}

			if test.actorTokenReadErr {
				defer overrideActorTokenError()()
			} else {
				defer overrideActorTokenGood()()
			}

			gotRequest, err := constructRequest(ctx, test.opts)
			if (err != nil) != test.wantErr {
				t.Fatalf("constructRequest(%v) = %v, wantErr: %v", test.opts, err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if err := compareRequest(gotRequest, test.wantReqParams); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func (s) TestSendRequest(t *testing.T) {
	defer overrideSubjectTokenGood()()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	req, err := constructRequest(ctx, goodOptions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		resp    *http.Response
		respErr error
		wantErr bool
	}{
		{
			name:    "client error",
			respErr: errors.New("http.Client.Do failed"),
			wantErr: true,
		},
		{
			name: "bad response body",
			resp: &http.Response{
				Status:     "200 OK",
				StatusCode: http.StatusOK,
				Body:       ioutil.NopCloser(errReader{}),
			},
			wantErr: true,
		},
		{
			name: "nonOK status code",
			resp: &http.Response{
				Status:     "400 BadRequest",
				StatusCode: http.StatusBadRequest,
				Body:       ioutil.NopCloser(strings.NewReader("")),
			},
			wantErr: true,
		},
		{
			name: "good case",
			resp: makeGoodResponse(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &testutils.FakeHTTPClient{
				ReqChan:  testutils.NewChannel(),
				RespChan: testutils.NewChannel(),
				Err:      test.respErr,
			}
			client.RespChan.Send(test.resp)
			_, err := sendRequest(client, req)
			if (err != nil) != test.wantErr {
				t.Errorf("sendRequest(%v) = %v, wantErr: %v", req, err, test.wantErr)
			}
		})
	}
}

func (s) TestTokenInfoFromResponse(t *testing.T) {
	noAccessToken, _ := json.Marshal(responseParameters{
		IssuedTokenType: issuedTokenType,
		TokenType:       "Bearer",
		ExpiresIn:       3600,
	})
	goodResponse, _ := json.Marshal(responseParameters{
		IssuedTokenType: requestedTokenType,
		AccessToken:     accessTokenContents,
		TokenType:       "Bearer",
		ExpiresIn:       3600,
	})

	tests := []struct {
		name          string
		respBody      []byte
		wantTokenInfo *tokenInfo
		wantErr       bool
	}{
		{
			name:     "bad JSON",
			respBody: []byte("not JSON"),
			wantErr:  true,
		},
		{
			name:     "empty response",
			respBody: []byte(""),
			wantErr:  true,
		},
		{
			name:     "non-empty response with no access token",
			respBody: noAccessToken,
			wantErr:  true,
		},
		{
			name:     "good response",
			respBody: goodResponse,
			wantTokenInfo: &tokenInfo{
				tokenType: "Bearer",
				token:     accessTokenContents,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotTokenInfo, err := tokenInfoFromResponse(test.respBody)
			if (err != nil) != test.wantErr {
				t.Fatalf("tokenInfoFromResponse(%+v) = %v, wantErr: %v", test.respBody, err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			// Can't do a cmp.Equal on the whole struct since the expiryField
			// is populated based on time.Now().
			if gotTokenInfo.tokenType != test.wantTokenInfo.tokenType || gotTokenInfo.token != test.wantTokenInfo.token {
				t.Errorf("tokenInfoFromResponse(%+v) = %+v, want: %+v", test.respBody, gotTokenInfo, test.wantTokenInfo)
			}
		})
	}
}

func (s) TestTLS(t *testing.T) {

	// start STS HTTPS server
	stsServer := httptest.NewUnstartedServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					// validate the inbound request
					reqParams := &requestParameters{}
					err := json.NewDecoder(r.Body).Decode(reqParams)
					if err != nil {
						fmt.Printf("Could Not parse STS Request application/json payload: (%+v)", err)
						http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
						return
					}

					// generate a static response
					respParams := &responseParameters{
						AccessToken:     accessTokenContents,
						IssuedTokenType: requestedTokenType,
						TokenType:       "Bearer",
						ExpiresIn:       int64(60),
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(respParams)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			},
		),
	)

	// load server cert used by the test STS and gRPC server
	// certificates use SAN wildcard   DNS:*.test.example.com
	cer, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("error loading server certificate: (%+v)", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cer},
	}

	stsListener, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("error creating listener for STS Server (%+v)", err)
	}

	stsServer.Listener = stsListener
	stsServer.TLS = tlsConfig
	stsServer.StartTLS()
	defer stsServer.Close()

	// setup grpcServer
	creds := credentials.NewTLS(tlsConfig)
	grpcListener, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("error creating listener for gRPC Test Server (%+v)", err)
	}

	grpcServer := grpc.NewServer(grpc.Creds(creds))
	grpc_testing.RegisterTestServiceServer(grpcServer, newTestServer())

	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			t.Errorf("gRPC Server Serve() failed: (%+v)", err)
		}
	}()
	defer grpcServer.GracefulStop()

	/// run tests

	serverCA, err := ioutil.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
	if err != nil {
		t.Fatalf("did not read tlsCA: %v", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(serverCA)

	validServerTLSConfig := &tls.Config{
		ServerName: "grpc.test.example.com",
		RootCAs:    caCertPool,
	}

	validSTSClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName: "sts.test.example.com",
				RootCAs:    caCertPool,
			},
		}}

	invalidSTSClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName: "bar.example.com",
				RootCAs:    caCertPool,
			},
		}}

	defaultSTSClient := http.DefaultClient

	tests := []struct {
		name          string
		grpcTLSConfig *tls.Config
		stsClient     http.Client
		wantErr       bool
	}{
		{
			name:          "Test STS Server with custom RootCAs",
			grpcTLSConfig: validServerTLSConfig,
			stsClient:     *validSTSClient,
			wantErr:       false,
		},
		{
			name:          "Test STS Server with default",
			grpcTLSConfig: validServerTLSConfig,
			stsClient:     *defaultSTSClient,
			wantErr:       true,
		},
		{
			name:          "Test STS Server with incorrect SNI",
			grpcTLSConfig: validServerTLSConfig,
			stsClient:     *invalidSTSClient,
			wantErr:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			stsCreds, err := NewCredentials(Options{
				TokenExchangeServiceURI: fmt.Sprintf("%s/token", stsServer.URL),
				Resource:                exampleResource,
				Audience:                exampleAudience,
				Scope:                   testScope,
				SubjectTokenPath:        testdata.Path("x509/server_ca_cert.pem"), // just read any file data and pretend its the token
				SubjectTokenType:        subjectTokenType,
				RequestedTokenType:      requestedTokenType,
				HttpClient:              test.stsClient,
			})
			if err != nil {
				t.Fatalf("error creating STS Credentials: (%+v)", err)
			}

			creds := credentials.NewTLS(test.grpcTLSConfig)
			conn, err := grpc.Dial(grpcListener.Addr().String(), grpc.WithTransportCredentials(creds), grpc.WithPerRPCCredentials(stsCreds))
			if err != nil {
				t.Fatalf("unexpected error on Dial (%+v) for test %s", err, test.name)
			}
			defer conn.Close()

			c := grpc_testing.NewTestServiceClient(conn)
			ctx := context.Background()

			rpcResp, err := c.UnaryCall(ctx, &grpc_testing.SimpleRequest{
				FillUsername: true,
			})
			// got error but did not expect one
			if (err != nil) && !test.wantErr {
				t.Fatalf("unexpected Error (%+v) for test %s", err, test.name)
			}
			// did not get error but expected one
			if err == nil && test.wantErr {
				t.Fatalf("expected Error for test %s but got nil", test.name)
			}
			// got an error and expected one
			if err != nil && test.wantErr {
				return
			}
			t.Logf("SimpleResponse Username %s", rpcResp.Username)
		})
	}
}
