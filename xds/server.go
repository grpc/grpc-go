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
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	xdsinternal "google.golang.org/grpc/internal/credentials/xds"
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

	// Unexported function to retrieve transport credentials from a gRPC server.
	grpcGetServerCreds = internal.GetServerCredentials.(func(*grpc.Server) credentials.TransportCredentials)
	buildProvider      = buildProviderFunc
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

	// If xds credentials were specified by the user, but bootstrap configs do
	// not contain any certificate provider configuration, it is better to fail
	// right now rather than failing when attempting to create certificate
	// providers after receiving an LDS response with security configuration.
	if s.xdsCredsInUse {
		bc := s.xdsC.BootstrapConfig()
		if bc == nil || len(bc.CertProviderConfigs) == 0 {
			return errors.New("xds: certificate_providers config missing in bootstrap file")
		}
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
	lw := &listenerWrapper{
		Listener: lis,
		closed:   grpcsync.NewEvent(),
		xdsHI:    xdsinternal.NewHandshakeInfo(nil, nil),
	}

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
		s.handleListenerUpdate(listenerUpdate{
			lw:         lw,
			name:       name,
			lds:        update,
			err:        err,
			goodUpdate: goodUpdate,
		})
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

// listenerUpdate wraps the information received from a registered LDS watcher.
type listenerUpdate struct {
	lw         *listenerWrapper         // listener associated with this watch
	name       string                   // resource name being watched
	lds        xdsclient.ListenerUpdate // received update
	err        error                    // received error
	goodUpdate *grpcsync.Event          // event to fire upon a good update
}

func (s *GRPCServer) handleListenerUpdate(update listenerUpdate) {
	if update.lw.closed.HasFired() {
		s.logger.Warningf("Resource %q received update: %v with error: %v, after for listener was closed", update.name, update.lds, update.err)
		return
	}

	if update.err != nil {
		// We simply log an error here and hope we get a successful update
		// in the future. The error could be because of a timeout or an
		// actual error, like the requested resource not found. In any case,
		// it is fine for the server to hang indefinitely until Stop() is
		// called.
		s.logger.Warningf("Received error for resource %q: %+v", update.name, update.err)
		return
	}
	s.logger.Infof("Received update for resource %q: %+v", update.name, update.lds.String())

	if err := s.handleSecurityConfig(update.lds.SecurityCfg, update.lw); err != nil {
		s.logger.Warningf("Invalid security config update: %v", err)
		return
	}

	// If we got all the way here, it means the received update was a good one.
	update.goodUpdate.Fire()
}

func (s *GRPCServer) handleSecurityConfig(config *xdsclient.SecurityConfig, lw *listenerWrapper) error {
	// If xdsCredentials are not in use, i.e, the user did not want to get
	// security configuration from the control plane, we should not be acting on
	// the received security config here. Doing so poses a security threat.
	if !s.xdsCredsInUse {
		return nil
	}

	// Security config being nil is a valid case where the control plane has
	// not sent any security configuration. The xdsCredentials implementation
	// handles this by delegating to its fallback credentials.
	if config == nil {
		// We need to explicitly set the fields to nil here since this might be
		// a case of switching from a good security configuration to an empty
		// one where fallback credentials are to be used.
		lw.xdsHI.SetRootCertProvider(nil)
		lw.xdsHI.SetIdentityCertProvider(nil)
		lw.xdsHI.SetRequireClientCert(false)
		return nil
	}

	cpc := s.xdsC.BootstrapConfig().CertProviderConfigs
	// Identity provider is mandatory on the server side.
	identityProvider, err := buildProvider(cpc, config.IdentityInstanceName, config.IdentityCertName, true, false)
	if err != nil {
		return err
	}

	// A root provider is required only when doing mTLS.
	var rootProvider certprovider.Provider
	if config.RootInstanceName != "" {
		rootProvider, err = buildProvider(cpc, config.RootInstanceName, config.RootCertName, false, true)
		if err != nil {
			return err
		}
	}

	// Close the old providers and cache the new ones.
	lw.providerMu.Lock()
	if lw.cachedIdentity != nil {
		lw.cachedIdentity.Close()
	}
	if lw.cachedRoot != nil {
		lw.cachedRoot.Close()
	}
	lw.cachedRoot = rootProvider
	lw.cachedIdentity = identityProvider

	// We set all fields here, even if some of them are nil, since they
	// could have been non-nil earlier.
	lw.xdsHI.SetRootCertProvider(rootProvider)
	lw.xdsHI.SetIdentityCertProvider(identityProvider)
	lw.xdsHI.SetRequireClientCert(config.RequireClientCert)
	lw.providerMu.Unlock()

	return nil
}

func buildProviderFunc(configs map[string]*certprovider.BuildableConfig, instanceName, certName string, wantIdentity, wantRoot bool) (certprovider.Provider, error) {
	cfg, ok := configs[instanceName]
	if !ok {
		return nil, fmt.Errorf("certificate provider instance %q not found in bootstrap file", instanceName)
	}
	provider, err := cfg.Build(certprovider.BuildOptions{
		CertName:     certName,
		WantIdentity: wantIdentity,
		WantRoot:     wantRoot,
	})
	if err != nil {
		// This error is not expected since the bootstrap process parses the
		// config and makes sure that it is acceptable to the plugin. Still, it
		// is possible that the plugin parses the config successfully, but its
		// Build() method errors out.
		return nil, fmt.Errorf("failed to get security plugin instance (%+v): %v", cfg, err)
	}
	return provider, nil
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

	// A small race exists in the xdsClient code where an xDS response is
	// received while the user is calling cancel(). In this small window the
	// registered callback can be called after the watcher is canceled. We avoid
	// processing updates received in callbacks once the listener is closed, to
	// make sure that we do not process updates received during this race
	// window.
	closed *grpcsync.Event

	// The certificate providers are cached here to that they can be closed when
	// a new provider is to be created.
	providerMu     sync.Mutex
	cachedRoot     certprovider.Provider
	cachedIdentity certprovider.Provider

	// Wraps all information required by the xds handshaker.
	xdsHI *xdsinternal.HandshakeInfo
}

// Accept blocks on an Accept() on the underlying listener, and wraps the
// returned net.Conn with the configured certificate providers.
func (l *listenerWrapper) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &conn{Conn: c, xdsHI: l.xdsHI}, nil
}

// Close closes the underlying listener. It also cancels the xDS watch
// registered in Serve() and closes any certificate provider instances created
// based on security configuration received in the LDS response.
func (l *listenerWrapper) Close() error {
	l.closed.Fire()
	l.Listener.Close()
	if l.cancelWatch != nil {
		l.cancelWatch()
	}

	l.providerMu.Lock()
	if l.cachedIdentity != nil {
		l.cachedIdentity.Close()
		l.cachedIdentity = nil
	}
	if l.cachedRoot != nil {
		l.cachedRoot.Close()
		l.cachedRoot = nil
	}
	l.providerMu.Unlock()

	return nil
}

// conn is a thin wrapper around a net.Conn returned by Accept().
type conn struct {
	net.Conn

	// This is the same HandshakeInfo as stored in the listenerWrapper that
	// created this conn. The former updates the HandshakeInfo whenever it
	// receives new security configuration.
	xdsHI *xdsinternal.HandshakeInfo

	// The connection deadline as configured by the grpc.Server on the rawConn
	// that is returned by a call to Accept(). This is set to the connection
	// timeout value configured by the user (or to a default value) before
	// initiating the transport credential handshake, and set to zero after
	// completing the HTTP2 handshake.
	deadlineMu sync.Mutex
	deadline   time.Time
}

// SetDeadline makes a copy of the passed in deadline and forwards the call to
// the underlying rawConn.
func (c *conn) SetDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.deadline = t
	c.deadlineMu.Unlock()
	return c.Conn.SetDeadline(t)
}

// GetDeadline returns the configured deadline. This will be invoked by the
// ServerHandshake() method of the XdsCredentials, which needs a deadline to
// pass to the certificate provider.
func (c *conn) GetDeadline() time.Time {
	c.deadlineMu.Lock()
	t := c.deadline
	c.deadlineMu.Unlock()
	return t
}

// XDSHandshakeInfo returns a pointer to the HandshakeInfo stored in conn. This
// will be invoked by the ServerHandshake() method of the XdsCredentials.
func (c *conn) XDSHandshakeInfo() *xdsinternal.HandshakeInfo {
	return c.xdsHI
}
