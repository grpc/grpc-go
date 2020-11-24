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

package xds

import (
	"context"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
)

const serverPrefix = "[xds-server %p] "

var (
	// These new functions will be overridden in unit tests.
	newXDSClient = func() (xdsClientInterface, error) {
		return xdsclient.New()
	}
	newGRPCServer = func(opts ...grpc.ServerOption) grpcServerInterface {
		return grpc.NewServer(opts...)
	}

	logger = grpclog.Component("xds")
)

func prefixLogger(p *GRPCServer) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(serverPrefix, p))
}

// xdsClientInterface contains methods from xdsClient.Client which are used by
// the server. This is useful for overriding in unit tests.
type xdsClientInterface interface {
	WatchListener(string, func(xdsclient.ListenerUpdate, error)) func()
	BootstrapConfig() *bootstrap.Config
	Close()
}

// grpcServerInterface contains methods from grpc.Server which are used by the
// GRPCServer type here. This is useful for overriding in unit tests.
type grpcServerInterface interface {
	RegisterService(*grpc.ServiceDesc, interface{})
	Serve(net.Listener) error
	Stop()
	GracefulStop()
}

// GRPCServer wraps a gRPC server and provides server-side xDS functionality, by
// communication with a management server using xDS APIs. It implements the
// grpc.ServiceRegistrar interface and can be passed to service registration
// functions in IDL generated code.
//
// Experimental
//
// Notice: This type is EXPERIMENTAL and may be changed or removed in a
// later release.
type GRPCServer struct {
	gs     grpcServerInterface
	quit   *grpcsync.Event
	logger *internalgrpclog.PrefixLogger

	// clientMu is used only in initXDSClient(), which is called at the
	// beginning of Serve(), where we have to decide if we have to create a
	// client or use an existing one.
	clientMu sync.Mutex
	xdsC     xdsClientInterface
}

// NewGRPCServer creates an xDS-enabled gRPC server using the passed in opts.
// The underlying gRPC server has no service registered and has not started to
// accept requests yet.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a later
// release.
func NewGRPCServer(opts ...grpc.ServerOption) *GRPCServer {
	newOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(xdsUnaryInterceptor),
		grpc.ChainStreamInterceptor(xdsStreamInterceptor),
	}
	newOpts = append(newOpts, opts...)
	s := &GRPCServer{
		gs:   newGRPCServer(newOpts...),
		quit: grpcsync.NewEvent(),
	}
	s.logger = prefixLogger(s)
	s.logger.Infof("Created xds.GRPCServer")
	return s
}

// RegisterService registers a service and its implementation to the underlying
// gRPC server. It is called from the IDL generated code. This must be called
// before invoking Serve.
func (s *GRPCServer) RegisterService(sd *grpc.ServiceDesc, ss interface{}) {
	s.gs.RegisterService(sd, ss)
}

// initXDSClient creates a new xdsClient if there is no existing one available.
func (s *GRPCServer) initXDSClient() error {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()

	if s.xdsC != nil {
		return nil
	}

	client, err := newXDSClient()
	if err != nil {
		return fmt.Errorf("xds: failed to create xds-client: %v", err)
	}
	s.xdsC = client
	s.logger.Infof("Created an xdsClient")
	return nil
}

// Serve gets the underlying gRPC server to accept incoming connections on the
// listener lis, which is expected to be listening on a TCP port.
//
// A connection to the management server, to receive xDS configuration, is
// initiated here.
//
// Serve will return a non-nil error unless Stop or GracefulStop is called.
func (s *GRPCServer) Serve(lis net.Listener) error {
	s.logger.Infof("Serve() passed a net.Listener on %s", lis.Addr().String())
	if _, ok := lis.Addr().(*net.TCPAddr); !ok {
		return fmt.Errorf("xds: GRPCServer expects listener to return a net.TCPAddr. Got %T", lis.Addr())
	}

	// If this is the first time Serve() is being called, we need to initialize
	// our xdsClient. If not, we can use the existing one.
	if err := s.initXDSClient(); err != nil {
		return err
	}
	lw, err := s.newListenerWrapper(lis)
	if lw == nil {
		// Error returned can be nil (when Stop/GracefulStop() is called). So,
		// we need to check the returned listenerWrapper instead.
		return err
	}
	return s.gs.Serve(lw)
}

// newListenerWrapper creates and returns a listenerWrapper, which is a thin
// wrapper around the passed in listener lis, that can be passed to
// grpcServer.Serve().
//
// It then registers a watch for a Listener resource and blocks until a good
// response is received or the server is stopped by a call to
// Stop/GracefulStop().
func (s *GRPCServer) newListenerWrapper(lis net.Listener) (*listenerWrapper, error) {
	lw := &listenerWrapper{Listener: lis}

	// This is used to notify that a good update has been received and that
	// Serve() can be invoked on the underlying gRPC server. Using a
	// grpcsync.Event instead of a vanilla channel simplifies the update handler
	// as it need not keep track of whether the received update is the first one
	// or not.
	goodUpdate := grpcsync.NewEvent()

	// The resource_name in the LDS request sent by the xDS-enabled gRPC server
	// is of the following format:
	// "/path/to/resource?udpa.resource.listening_address=IP:Port". The
	// `/path/to/resource` part of the name is sourced from the bootstrap config
	// field `grpc_server_resource_name_id`. If this field is not specified in
	// the bootstrap file, we will use a default of `grpc/server`.
	path := "grpc/server"
	if cfg := s.xdsC.BootstrapConfig(); cfg != nil && cfg.ServerResourceNameID != "" {
		path = cfg.ServerResourceNameID
	}
	name := fmt.Sprintf("%s?udpa.resource.listening_address=%s", path, lis.Addr().String())

	// Register an LDS watch using our xdsClient, and specify the listening
	// address as the resource name.
	cancelWatch := s.xdsC.WatchListener(name, func(update xdsclient.ListenerUpdate, err error) {
		if err != nil {
			// We simply log an error here and hope we get a successful update
			// in the future. The error could be because of a timeout or an
			// actual error, like the requested resource not found. In any case,
			// it is fine for the server to hang indefinitely until Stop() is
			// called.
			s.logger.Warningf("Received error for resource %q: %+v", name, err)
			return
		}

		s.logger.Infof("Received update for resource %q: %+v", name, update)

		// TODO(easwars): Handle security configuration, create appropriate
		// certificate providers and update the listenerWrapper before firing
		// the event. Errors encountered during any of these steps should result
		// in an early exit, and the update event should not fire.
		goodUpdate.Fire()
	})

	s.logger.Infof("Watch started on resource name %v", name)
	lw.cancelWatch = func() {
		cancelWatch()
		s.logger.Infof("Watch cancelled on resource name %v", name)
	}

	// Block until a good LDS response is received or the server is stopped.
	select {
	case <-s.quit.Done():
		// Since the listener has not yet been handed over to gs.Serve(), we
		// need to explicitly close the listener. Cancellation of the xDS watch
		// is handled by the listenerWrapper.
		lw.Close()
		return nil, nil
	case <-goodUpdate.Done():
	}
	return lw, nil
}

// Stop stops the underlying gRPC server. It immediately closes all open
// connections. It cancels all active RPCs on the server side and the
// corresponding pending RPCs on the client side will get notified by connection
// errors.
func (s *GRPCServer) Stop() {
	s.quit.Fire()
	s.gs.Stop()
	if s.xdsC != nil {
		s.xdsC.Close()
	}
}

// GracefulStop stops the underlying gRPC server gracefully. It stops the server
// from accepting new connections and RPCs and blocks until all the pending RPCs
// are finished.
func (s *GRPCServer) GracefulStop() {
	s.quit.Fire()
	s.gs.GracefulStop()
	if s.xdsC != nil {
		s.xdsC.Close()
	}
}

// xdsUnaryInterceptor is the unary interceptor added to the gRPC server to
// perform any xDS specific functionality on unary RPCs.
//
// This is a no-op at this point.
func xdsUnaryInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	return handler(ctx, req)
}

// xdsStreamInterceptor is the stream interceptor added to the gRPC server to
// perform any xDS specific functionality on streaming RPCs.
//
// This is a no-op at this point.
func xdsStreamInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return handler(srv, ss)
}

// listenerWrapper wraps the net.Listener associated with the listening address
// passed to Serve(). It also contains all other state associated with this
// particular invocation of Serve().
type listenerWrapper struct {
	net.Listener
	cancelWatch func()

	// TODO(easwars): Add fields for certificate providers.
}

// Accept blocks on an Accept() on the underlying listener, and wraps the
// returned net.Conn with the configured certificate providers.
func (l *listenerWrapper) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &conn{Conn: c}, nil
}

// Close closes the underlying listener. It also cancels the xDS watch
// registered in Serve() and closes any certificate provider instances created
// based on security configuration received in the LDS response.
func (l *listenerWrapper) Close() error {
	l.Listener.Close()
	if l.cancelWatch != nil {
		l.cancelWatch()
	}
	return nil
}

// conn is a thin wrapper around a net.Conn returned by Accept().
type conn struct {
	net.Conn

	// TODO(easwars): Add fields for certificate providers.
}
