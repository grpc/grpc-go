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
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/server"
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

	// Unexported function to retrieve transport credentials from a gRPC server.
	grpcGetServerCreds = internal.GetServerCredentials.(func(*grpc.Server) credentials.TransportCredentials)
	logger             = grpclog.Component("xds")
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
	gs            grpcServerInterface
	quit          *grpcsync.Event
	logger        *internalgrpclog.PrefixLogger
	xdsCredsInUse bool

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

	// We type assert our underlying gRPC server to the real grpc.Server here
	// before trying to retrieve the configured credentials. This approach
	// avoids performing the same type assertion in the grpc package which
	// provides the implementation for internal.GetServerCredentials, and allows
	// us to use a fake gRPC server in tests.
	if gs, ok := s.gs.(*grpc.Server); ok {
		creds := grpcGetServerCreds(gs)
		if xc, ok := creds.(interface{ UsesXDS() bool }); ok && xc.UsesXDS() {
			s.xdsCredsInUse = true
		}
	}

	s.logger.Infof("xDS credentials in use: %v", s.xdsCredsInUse)
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
// TODO: Support callback to get notified on serving state changes.
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

	cfg := s.xdsC.BootstrapConfig()
	if cfg == nil {
		return errors.New("bootstrap configuration is empty")
	}

	// If xds credentials were specified by the user, but bootstrap configs do
	// not contain any certificate provider configuration, it is better to fail
	// right now rather than failing when attempting to create certificate
	// providers after receiving an LDS response with security configuration.
	if s.xdsCredsInUse {
		if len(cfg.CertProviderConfigs) == 0 {
			return errors.New("xds: certificate_providers config missing in bootstrap file")
		}
	}

	// The server listener resource name template from the bootstrap
	// configuration contains a template for the name of the Listener resource
	// to subscribe to for a gRPC server. If the token `%s` is present in the
	// string, it will be replaced with the server's listening "IP:port" (e.g.,
	// "0.0.0.0:8080", "[::]:8080"). The absence of a template will be treated
	// as an error since we do not have any default value for this.
	if cfg.ServerListenerResourceNameTemplate == "" {
		return errors.New("missing server_listener_resource_name_template in the bootstrap configuration")
	}
	name := cfg.ServerListenerResourceNameTemplate
	if strings.Contains(cfg.ServerListenerResourceNameTemplate, "%s") {
		name = strings.Replace(cfg.ServerListenerResourceNameTemplate, "%s", lis.Addr().String(), -1)
	}

	// Create a listenerWrapper which handles all functionality required by
	// this particular instance of Serve().
	lw, goodUpdateCh := server.NewListenerWrapper(server.ListenerWrapperParams{
		Listener:             lis,
		ListenerResourceName: name,
		XDSCredsInUse:        s.xdsCredsInUse,
		XDSClient:            s.xdsC,
	})

	// Block until a good LDS response is received or the server is stopped.
	select {
	case <-s.quit.Done():
		// Since the listener has not yet been handed over to gs.Serve(), we
		// need to explicitly close the listener. Cancellation of the xDS watch
		// is handled by the listenerWrapper.
		lw.Close()
		return nil
	case <-goodUpdateCh:
	}
	return s.gs.Serve(lw)
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
