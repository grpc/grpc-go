package http_over_grpc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/textproto"
	"time"

	"google.golang.org/grpc/grpclog"

        pb "google.golang.org/grpc/http_over_grpc/http_over_grpc_proto"
)

var (
	// ErrResponseHeaderTimeout is an error returned when a timeout fires before the
	// response headers are received. This indicates a connectivity issue with the
	// Connect agent over the proxy.
	ErrResponseHeaderTimeout = errors.New("timeout before response headers received")
)

const (
	crlf         = "\r\n"
)

// Transport is an http.Transport that communicates via a HTTPOverGRPC.
type Transport struct {
	ctx context.Context
	// HTTPOverGRPC client.
	client pb.HTTPOverGRPCClient
}

type wrappedBody struct {
	reader io.Reader
	pr     *io.PipeReader
}

func (h *wrappedBody) Close() error {
	return h.pr.Close()
}

func (h *wrappedBody) Read(p []byte) (n int, err error) {
	return h.reader.Read(p)
}

// RoundTrip implements the http.RoundTripper interface and is used by http.Client.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := t.ctx
	if ctx == context.Background() || ctx == nil {
		ctx = req.Context()
	}
	grpclog.Infof("%s %q", req.Method, req.URL.String())

	var body []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		if body, err = ioutil.ReadAll(req.Body); err != nil {
			grpclog.Errorf("Error reading body: %v", err)
			return nil, err
		}
	}

	hReq := &pb.HTTPOverGRPCRequest{
		Method:  req.Method,
		Url:     req.URL.String(),
		Headers: fromHeader(req.Header),
		Body:    body,
	}

	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(t.doRequest(ctx, pw, hReq))
	}()

	type responseOrError struct {
		resp *http.Response
		err  error
	}
	// This needs to be buffered, otherwise the goroutine will leak.
	respCh := make(chan responseOrError, 1)
	go func() {
		resp, err := readResponse(pr, req)
		respCh <- responseOrError{resp, err}
	}()
	timeout := func(usage string) (*http.Response, error) {
		pr.Close()
		grpclog.Infof("Response header timeout: %v", usage)
		return nil, ErrResponseHeaderTimeout
	}
	select {
	case re := <-respCh:
		return re.resp, re.err
	case <-ctx.Done():
		return timeout("ctx.Done")
	}
}

// doRequest makes the HTTPOverGRPC HTTPRequest and handles the response.
func (t *Transport) doRequest(ctx context.Context, w io.Writer, req *pb.HTTPOverGRPCRequest) error {
	// gRPC deadline becomes the HTTP request timeout.
	ctx, cancel := context.WithCancel(ctx)
	deadline, ok := ctx.Deadline()
	if ok {
		grpclog.Infof("Using Transport.Timeout = %v (plus epsilon)", deadline)
		tt := deadline.Sub(time.Now())
		ctx, cancel = context.WithTimeout(ctx, tt+100*time.Millisecond)
	}
	defer cancel()

	resp, err := t.client.HTTPRequest(ctx, req)
	if err != nil {
		return err
	}

	if _, err = w.Write(resp.Body); err != nil {
		grpclog.Errorf("error writing response: %v", err)
		return err
	}

	return nil
}

// readResponse reads the HTTP response from the pipe reader and returns the
// http response.
func readResponse(pr *io.PipeReader, req *http.Request) (resp *http.Response, err error) {
	defer func() {
		if err != nil {
			pr.CloseWithError(err)
		}
	}()
	tp := textproto.NewReader(bufio.NewReader(pr))

	hs, err := getStatusAndHeader(req, tp)
	if err != nil {
		return nil, err
	}
	body := io.Reader(tp.R)

	res, err := http.ReadResponse(bufio.NewReader(io.MultiReader(hs, body)), req)
	if err != nil {
		return nil, err
	}
	// Wrap the body and pipe reader to ensure both are closed.
	res.Body = &wrappedBody{
		reader: res.Body,
		pr:     pr,
	}
	return res, nil
}

// fromHeader converts an http.Header to the repeated pb.Header proto field.
func fromHeader(hdrs http.Header) []*pb.Header {
	result := make([]*pb.Header, 0, len(hdrs))
	for k, vals := range hdrs {
		result = append(result, &pb.Header{Key: k, Values: vals})
	}
	return result
}

// getStatusAndHeader reads the http status line and response headers
func getStatusAndHeader(req *http.Request, tp *textproto.Reader) (*bytes.Buffer, error) {
	// Read the HTTP status line.
	line, err := tp.ReadLineBytes()
	if err != nil {
		grpclog.Infof("Response err on request %s %q, error: %v", req.Method, req.URL.String(), err)
		return nil, err
	}
	grpclog.Infof("Response: %q", line)
	// Read the HTTP headers.
	buf := new(bytes.Buffer)
	buf.Write(line)
	buf.WriteString(crlf)

	mimeHeader, err := tp.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	headers := http.Header(mimeHeader)
	if err := headers.Write(buf); err != nil {
		return nil, err
	}
	buf.WriteString(crlf)
	return buf, nil
}
