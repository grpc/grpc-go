package grpc

import (
	"bytes"
	"errors"
	"io"
	"math"
	"time"

	"github.com/lypnol/grpc-go/codes"
	"github.com/lypnol/grpc-go/stats"
	"github.com/lypnol/grpc-go/transport"
	"golang.org/x/net/context"
	"golang.org/x/net/trace"
)

// recvRawResponse receives an RPC raw response.
// On error, it returns the error and indicates whether the call should be retried.
func recvRawResponse(ctx context.Context, dopts dialOptions, t transport.ClientTransport, c *callInfo, stream *transport.Stream, reply []byte) (err error) {
	// Try to acquire header metadata from the server if there is any.
	defer func() {
		if err != nil {
			if _, ok := err.(transport.ConnectionError); !ok {
				t.CloseStream(stream, err)
			}
		}
	}()
	c.headerMD, err = stream.Header()
	if err != nil {
		return
	}
	p := &parser{r: stream}
	var inPayload *stats.InPayload
	if dopts.copts.StatsHandler != nil {
		inPayload = &stats.InPayload{
			Client: true,
		}
	}
	for {
		if err = rawRecv(p, dopts.codec, stream, dopts.dc, reply, math.MaxInt32, inPayload); err != nil {
			if err == io.EOF {
				break
			}
			return
		}
	}
	if inPayload != nil && err == io.EOF && stream.StatusCode() == codes.OK {
		// TODO in the current implementation, inTrailer may be handled before inPayload in some cases.
		// Fix the order if necessary.
		dopts.copts.StatsHandler.HandleRPC(ctx, inPayload)
	}
	c.trailerMD = stream.Trailer()
	return nil
}

// sendRawRequest writes out various information of an RPC such as Context and Message.
func sendRawRequest(ctx context.Context, dopts dialOptions, compressor Compressor, callHdr *transport.CallHdr, t transport.ClientTransport, args []byte, opts *transport.Options) (_ *transport.Stream, err error) {
	stream, err := t.NewStream(ctx, callHdr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			// If err is connection error, t will be closed, no need to close stream here.
			if _, ok := err.(transport.ConnectionError); !ok {
				t.CloseStream(stream, err)
			}
		}
	}()
	var (
		cbuf       *bytes.Buffer
		outPayload *stats.OutPayload
	)
	if compressor != nil {
		cbuf = new(bytes.Buffer)
	}
	if dopts.copts.StatsHandler != nil {
		outPayload = &stats.OutPayload{
			Client: true,
		}
	}
	outBuf, err := encodeRaw(dopts.codec, args, compressor, cbuf, outPayload)
	if err != nil {
		return nil, Errorf(codes.Internal, "grpc: %v", err)
	}
	err = t.Write(stream, outBuf, opts)
	if err == nil && outPayload != nil {
		outPayload.SentTime = time.Now()
		dopts.copts.StatsHandler.HandleRPC(ctx, outPayload)
	}
	// t.NewStream(...) could lead to an early rejection of the RPC (e.g., the service/method
	// does not exist.) so that t.Write could get io.EOF from wait(...). Leave the following
	// recvRawResponse to get the final status.
	if err != nil && err != io.EOF {
		return nil, err
	}
	// Sent successfully.
	return stream, nil
}

// RawInvoke sends the RPC request on the wire and returns after response is received.
// The query message and the reply are in raw bytes format.
// RawInvoke is called by generated code. Also users can call Invoke directly when it
// is really needed in their use cases.
func RawInvoke(ctx context.Context, method string, args, reply []byte, cc *ClientConn, opts ...CallOption) error {
	if cc.dopts.unaryInt != nil {
		// Does not support unaryInt
		return errors.New("RawInvoke does not support unaryInt")
	}
	return rawInvoke(ctx, method, args, reply, cc, opts...)
}

func rawInvoke(ctx context.Context, method string, args, reply []byte, cc *ClientConn, opts ...CallOption) (e error) {
	c := defaultCallInfo
	if mc, ok := cc.getMethodConfig(method); ok {
		c.failFast = !mc.WaitForReady
		if mc.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, mc.Timeout)
			defer cancel()
		}
	}
	for _, o := range opts {
		if err := o.before(&c); err != nil {
			return toRPCErr(err)
		}
	}
	defer func() {
		for _, o := range opts {
			o.after(&c)
		}
	}()
	if EnableTracing {
		c.traceInfo.tr = trace.New("grpc.Sent."+methodFamily(method), method)
		defer c.traceInfo.tr.Finish()
		c.traceInfo.firstLine.client = true
		if deadline, ok := ctx.Deadline(); ok {
			c.traceInfo.firstLine.deadline = deadline.Sub(time.Now())
		}
		c.traceInfo.tr.LazyLog(&c.traceInfo.firstLine, false)
		// TODO(dsymonds): Arrange for c.traceInfo.firstLine.remoteAddr to be set.
		defer func() {
			if e != nil {
				c.traceInfo.tr.LazyLog(&fmtStringer{"%v", []interface{}{e}}, true)
				c.traceInfo.tr.SetError()
			}
		}()
	}
	sh := cc.dopts.copts.StatsHandler
	if sh != nil {
		ctx = sh.TagRPC(ctx, &stats.RPCTagInfo{FullMethodName: method})
		begin := &stats.Begin{
			Client:    true,
			BeginTime: time.Now(),
			FailFast:  c.failFast,
		}
		sh.HandleRPC(ctx, begin)
	}
	defer func() {
		if sh != nil {
			end := &stats.End{
				Client:  true,
				EndTime: time.Now(),
				Error:   e,
			}
			sh.HandleRPC(ctx, end)
		}
	}()
	topts := &transport.Options{
		Last:  true,
		Delay: false,
	}
	for {
		var (
			err    error
			t      transport.ClientTransport
			stream *transport.Stream
			// Record the put handler from Balancer.Get(...). It is called once the
			// RPC has completed or failed.
			put func()
		)
		// TODO(zhaoq): Need a formal spec of fail-fast.
		callHdr := &transport.CallHdr{
			Host:   cc.authority,
			Method: method,
		}
		if cc.dopts.cp != nil {
			callHdr.SendCompress = cc.dopts.cp.Type()
		}

		gopts := BalancerGetOptions{
			BlockingWait: !c.failFast,
		}
		t, put, err = cc.getTransport(ctx, gopts)
		if err != nil {
			// TODO(zhaoq): Probably revisit the error handling.
			if _, ok := err.(*rpcError); ok {
				return err
			}
			if err == errConnClosing || err == errConnUnavailable {
				if c.failFast {
					return Errorf(codes.Unavailable, "%v", err)
				}
				continue
			}
			// All the other errors are treated as Internal errors.
			return Errorf(codes.Internal, "%v", err)
		}
		if c.traceInfo.tr != nil {
			c.traceInfo.tr.LazyLog(&payload{sent: true, msg: args}, true)
		}
		stream, err = sendRawRequest(ctx, cc.dopts, cc.dopts.cp, callHdr, t, args, topts)
		if err != nil {
			if put != nil {
				put()
				put = nil
			}
			// Retry a non-failfast RPC when
			// i) there is a connection error; or
			// ii) the server started to drain before this RPC was initiated.
			if _, ok := err.(transport.ConnectionError); ok || err == transport.ErrStreamDrain {
				if c.failFast {
					return toRPCErr(err)
				}
				continue
			}
			return toRPCErr(err)
		}
		err = recvRawResponse(ctx, cc.dopts, t, &c, stream, reply)
		if err != nil {
			if put != nil {
				put()
				put = nil
			}
			if _, ok := err.(transport.ConnectionError); ok || err == transport.ErrStreamDrain {
				if c.failFast {
					return toRPCErr(err)
				}
				continue
			}
			return toRPCErr(err)
		}
		if c.traceInfo.tr != nil {
			c.traceInfo.tr.LazyLog(&payload{sent: false, msg: reply}, true)
		}
		t.CloseStream(stream, nil)
		if put != nil {
			put()
			put = nil
		}
		return Errorf(stream.StatusCode(), "%s", stream.StatusDesc())
	}
}
