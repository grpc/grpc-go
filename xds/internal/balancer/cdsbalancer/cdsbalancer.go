/*
 * Copyright 2019 gRPC authors.
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
 */

// Package cdsbalancer implements a balancer to handle CDS responses.
package cdsbalancer

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/buffer"
	xdsinternal "google.golang.org/grpc/internal/credentials/xds"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/edsbalancer"
	"google.golang.org/grpc/xds/internal/client"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
)

const (
	cdsName = "cds_experimental"
	edsName = "eds_experimental"
)

var (
	errBalancerClosed = errors.New("cdsBalancer is closed")

	// newEDSBalancer is a helper function to build a new edsBalancer and will be
	// overridden in unittests.
	newEDSBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		builder := balancer.Get(edsName)
		if builder == nil {
			return nil, fmt.Errorf("xds: no balancer builder with name %v", edsName)
		}
		// We directly pass the parent clientConn to the
		// underlying edsBalancer because the cdsBalancer does
		// not deal with subConns.
		return builder.Build(cc, opts), nil
	}
	newXDSClient  = func() (xdsClientInterface, error) { return xdsclient.New() }
	buildProvider = buildProviderFunc
)

func init() {
	balancer.Register(cdsBB{})
}

// cdsBB (short for cdsBalancerBuilder) implements the balancer.Builder
// interface to help build a cdsBalancer.
// It also implements the balancer.ConfigParser interface to help parse the
// JSON service config, to be passed to the cdsBalancer.
type cdsBB struct{}

// Build creates a new CDS balancer with the ClientConn.
func (cdsBB) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	b := &cdsBalancer{
		bOpts:       opts,
		updateCh:    buffer.NewUnbounded(),
		closed:      grpcsync.NewEvent(),
		cancelWatch: func() {}, // No-op at this point.
		xdsHI:       xdsinternal.NewHandshakeInfo(nil, nil),
	}
	b.logger = prefixLogger((b))
	b.logger.Infof("Created")

	client, err := newXDSClient()
	if err != nil {
		b.logger.Errorf("failed to create xds-client: %v", err)
		return nil
	}
	b.xdsClient = client

	var creds credentials.TransportCredentials
	switch {
	case opts.DialCreds != nil:
		creds = opts.DialCreds
	case opts.CredsBundle != nil:
		creds = opts.CredsBundle.TransportCredentials()
	}
	if xc, ok := creds.(interface{ UsesXDS() bool }); ok && xc.UsesXDS() {
		b.xdsCredsInUse = true
	}
	b.logger.Infof("xDS credentials in use: %v", b.xdsCredsInUse)

	b.ccw = &ccWrapper{
		ClientConn: cc,
		xdsHI:      b.xdsHI,
	}
	go b.run()
	return b
}

// Name returns the name of balancers built by this builder.
func (cdsBB) Name() string {
	return cdsName
}

// lbConfig represents the loadBalancingConfig section of the service config
// for the cdsBalancer.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig
	ClusterName string `json:"Cluster"`
}

// ParseConfig parses the JSON load balancer config provided into an
// internal form or returns an error if the config is invalid.
func (cdsBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var cfg lbConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, fmt.Errorf("xds: unable to unmarshal lbconfig: %s, error: %v", string(c), err)
	}
	return &cfg, nil
}

// xdsClientInterface contains methods from xdsClient.Client which are used by
// the cdsBalancer. This will be faked out in unittests.
type xdsClientInterface interface {
	WatchCluster(string, func(xdsclient.ClusterUpdate, error)) func()
	BootstrapConfig() *bootstrap.Config
	Close()
}

// ccUpdate wraps a clientConn update received from gRPC (pushed from the
// xdsResolver). A valid clusterName causes the cdsBalancer to register a CDS
// watcher with the xdsClient, while a non-nil error causes it to cancel the
// existing watch and propagate the error to the underlying edsBalancer.
type ccUpdate struct {
	clusterName string
	err         error
}

// scUpdate wraps a subConn update received from gRPC. This is directly passed
// on to the edsBalancer.
type scUpdate struct {
	subConn balancer.SubConn
	state   balancer.SubConnState
}

// watchUpdate wraps the information received from a registered CDS watcher. A
// non-nil error is propagated to the underlying edsBalancer. A valid update
// results in creating a new edsBalancer (if one doesn't already exist) and
// pushing the update to it.
type watchUpdate struct {
	cds xdsclient.ClusterUpdate
	err error
}

// cdsBalancer implements a CDS based LB policy. It instantiates an EDS based
// LB policy to further resolve the serviceName received from CDS, into
// localities and endpoints. Implements the balancer.Balancer interface which
// is exposed to gRPC and implements the balancer.ClientConn interface which is
// exposed to the edsBalancer.
type cdsBalancer struct {
	ccw            *ccWrapper            // ClientConn interface passed to child LB.
	bOpts          balancer.BuildOptions // BuildOptions passed to child LB.
	updateCh       *buffer.Unbounded     // Channel for gRPC and xdsClient updates.
	xdsClient      xdsClientInterface    // xDS client to watch Cluster resource.
	cancelWatch    func()                // Cluster watch cancel func.
	edsLB          balancer.Balancer     // EDS child policy.
	clusterToWatch string
	logger         *grpclog.PrefixLogger
	closed         *grpcsync.Event

	// The certificate providers are cached here to that they can be closed when
	// a new provider is to be created.
	cachedRoot     certprovider.Provider
	cachedIdentity certprovider.Provider
	xdsHI          *xdsinternal.HandshakeInfo
	xdsCredsInUse  bool
}

// handleClientConnUpdate handles a ClientConnUpdate received from gRPC. Good
// updates lead to registration of a CDS watch. Updates with error lead to
// cancellation of existing watch and propagation of the same error to the
// edsBalancer.
func (b *cdsBalancer) handleClientConnUpdate(update *ccUpdate) {
	// We first handle errors, if any, and then proceed with handling the
	// update, only if the status quo has changed.
	if err := update.err; err != nil {
		b.handleErrorFromUpdate(err, true)
	}
	if b.clusterToWatch == update.clusterName {
		return
	}
	if update.clusterName != "" {
		cancelWatch := b.xdsClient.WatchCluster(update.clusterName, b.handleClusterUpdate)
		b.logger.Infof("Watch started on resource name %v with xds-client %p", update.clusterName, b.xdsClient)
		b.cancelWatch = func() {
			cancelWatch()
			b.logger.Infof("Watch cancelled on resource name %v with xds-client %p", update.clusterName, b.xdsClient)
		}
		b.clusterToWatch = update.clusterName
	}
}

// handleSecurityConfig processes the security configuration received from the
// management server, creates appropriate certificate provider plugins, and
// updates the HandhakeInfo which is added as an address attribute in
// NewSubConn() calls.
func (b *cdsBalancer) handleSecurityConfig(config *xdsclient.SecurityConfig) error {
	// If xdsCredentials are not in use, i.e, the user did not want to get
	// security configuration from an xDS server, we should not be acting on the
	// received security config here. Doing so poses a security threat.
	if !b.xdsCredsInUse {
		return nil
	}

	// Security config being nil is a valid case where the management server has
	// not sent any security configuration. The xdsCredentials implementation
	// handles this by delegating to its fallback credentials.
	if config == nil {
		// We need to explicitly set the fields to nil here since this might be
		// a case of switching from a good security configuration to an empty
		// one where fallback credentials are to be used.
		b.xdsHI.SetRootCertProvider(nil)
		b.xdsHI.SetIdentityCertProvider(nil)
		b.xdsHI.SetAcceptedSANs(nil)
		return nil
	}

	bc := b.xdsClient.BootstrapConfig()
	if bc == nil || bc.CertProviderConfigs == nil {
		// Bootstrap did not find any certificate provider configs, but the user
		// has specified xdsCredentials and the management server has sent down
		// security configuration.
		return errors.New("xds: certificate_providers config missing in bootstrap file")
	}
	cpc := bc.CertProviderConfigs

	// A root provider is required whether we are using TLS or mTLS.
	rootProvider, err := buildProvider(cpc, config.RootInstanceName, config.RootCertName, false, true)
	if err != nil {
		return err
	}

	// The identity provider is only present when using mTLS.
	var identityProvider certprovider.Provider
	if name, cert := config.IdentityInstanceName, config.IdentityCertName; name != "" {
		var err error
		identityProvider, err = buildProvider(cpc, name, cert, true, false)
		if err != nil {
			return err
		}
	}

	// Close the old providers and cache the new ones.
	if b.cachedRoot != nil {
		b.cachedRoot.Close()
	}
	if b.cachedIdentity != nil {
		b.cachedIdentity.Close()
	}
	b.cachedRoot = rootProvider
	b.cachedIdentity = identityProvider

	// We set all fields here, even if some of them are nil, since they
	// could have been non-nil earlier.
	b.xdsHI.SetRootCertProvider(rootProvider)
	b.xdsHI.SetIdentityCertProvider(identityProvider)
	b.xdsHI.SetAcceptedSANs(config.AcceptedSANs)
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
		return nil, fmt.Errorf("xds: failed to get security plugin instance (%+v): %v", cfg, err)
	}
	return provider, nil
}

// handleWatchUpdate handles a watch update from the xDS Client. Good updates
// lead to clientConn updates being invoked on the underlying edsBalancer.
func (b *cdsBalancer) handleWatchUpdate(update *watchUpdate) {
	if err := update.err; err != nil {
		b.logger.Warningf("Watch error from xds-client %p: %v", b.xdsClient, err)
		b.handleErrorFromUpdate(err, false)
		return
	}

	b.logger.Infof("Watch update from xds-client %p, content: %+v", b.xdsClient, update.cds)

	// Process the security config from the received update before building the
	// child policy or forwarding the update to it. We do this because the child
	// policy may try to create a new subConn inline. Processing the security
	// configuration here and setting up the handshakeInfo will make sure that
	// such attempts are handled properly.
	if err := b.handleSecurityConfig(update.cds.SecurityCfg); err != nil {
		// If the security config is invalid, for example, if the provider
		// instance is not found in the bootstrap config, we need to put the
		// channel in transient failure.
		b.logger.Warningf("Invalid security config update from xds-client %p: %v", b.xdsClient, err)
		b.handleErrorFromUpdate(err, false)
		return
	}

	client.SetMaxRequests(update.cds.ServiceName, update.cds.MaxRequests)

	// The first good update from the watch API leads to the instantiation of an
	// edsBalancer. Further updates/errors are propagated to the existing
	// edsBalancer.
	if b.edsLB == nil {
		edsLB, err := newEDSBalancer(b.ccw, b.bOpts)
		if err != nil {
			b.logger.Errorf("Failed to create child policy of type %s, %v", edsName, err)
			return
		}
		b.edsLB = edsLB
		b.logger.Infof("Created child policy %p of type %s", b.edsLB, edsName)
	}
	lbCfg := &edsbalancer.EDSConfig{EDSServiceName: update.cds.ServiceName}
	if update.cds.EnableLRS {
		// An empty string here indicates that the edsBalancer should use the
		// same xDS server for load reporting as it does for EDS
		// requests/responses.
		lbCfg.LrsLoadReportingServerName = new(string)

	}
	ccState := balancer.ClientConnState{
		BalancerConfig: lbCfg,
	}
	if err := b.edsLB.UpdateClientConnState(ccState); err != nil {
		b.logger.Errorf("xds: edsBalancer.UpdateClientConnState(%+v) returned error: %v", ccState, err)
	}
}

// run is a long-running goroutine which handles all updates from gRPC. All
// methods which are invoked directly by gRPC or xdsClient simply push an
// update onto a channel which is read and acted upon right here.
func (b *cdsBalancer) run() {
	for {
		select {
		case u := <-b.updateCh.Get():
			b.updateCh.Load()
			switch update := u.(type) {
			case *ccUpdate:
				b.handleClientConnUpdate(update)
			case *scUpdate:
				// SubConn updates are passthrough and are simply handed over to
				// the underlying edsBalancer.
				if b.edsLB == nil {
					b.logger.Errorf("xds: received scUpdate {%+v} with no edsBalancer", update)
					break
				}
				b.edsLB.UpdateSubConnState(update.subConn, update.state)
			case *watchUpdate:
				b.handleWatchUpdate(update)
			}

		// Close results in cancellation of the CDS watch and closing of the
		// underlying edsBalancer and is the only way to exit this goroutine.
		case <-b.closed.Done():
			b.cancelWatch()
			b.cancelWatch = func() {}

			if b.edsLB != nil {
				b.edsLB.Close()
				b.edsLB = nil
			}
			// This is the *ONLY* point of return from this function.
			b.logger.Infof("Shutdown")
			return
		}
	}
}

// handleErrorFromUpdate handles both the error from parent ClientConn (from
// resolver) and the error from xds client (from the watcher). fromParent is
// true if error is from parent ClientConn.
//
// If the error is connection error, it's passed down to the child policy.
// Nothing needs to be done in CDS (e.g. it doesn't go into fallback).
//
// If the error is resource-not-found:
// - If it's from resolver, it means LDS resources were removed. The CDS watch
// should be canceled.
// - If it's from xds client, it means CDS resource were removed. The CDS
// watcher should keep watching.
//
// In both cases, the error will be forwarded to EDS balancer. And if error is
// resource-not-found, the child EDS balancer will stop watching EDS.
func (b *cdsBalancer) handleErrorFromUpdate(err error, fromParent bool) {
	// TODO: connection errors will be sent to the eds balancers directly, and
	// also forwarded by the parent balancers/resolvers. So the eds balancer may
	// see the same error multiple times. We way want to only forward the error
	// to eds if it's not a connection error.
	//
	// This is not necessary today, because xds client never sends connection
	// errors.
	if fromParent && xdsclient.ErrType(err) == xdsclient.ErrorTypeResourceNotFound {
		b.cancelWatch()
	}
	if b.edsLB != nil {
		b.edsLB.ResolverError(err)
	} else {
		// If eds balancer was never created, fail the RPCs with
		// errors.
		b.ccw.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            base.NewErrPicker(err),
		})
	}
}

// handleClusterUpdate is the CDS watch API callback. It simply pushes the
// received information on to the update channel for run() to pick it up.
func (b *cdsBalancer) handleClusterUpdate(cu xdsclient.ClusterUpdate, err error) {
	if b.closed.HasFired() {
		b.logger.Warningf("xds: received cluster update {%+v} after cdsBalancer was closed", cu)
		return
	}
	b.updateCh.Put(&watchUpdate{cds: cu, err: err})
}

// UpdateClientConnState receives the serviceConfig (which contains the
// clusterName to watch for in CDS) and the xdsClient object from the
// xdsResolver.
func (b *cdsBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	if b.closed.HasFired() {
		b.logger.Warningf("xds: received ClientConnState {%+v} after cdsBalancer was closed", state)
		return errBalancerClosed
	}

	b.logger.Infof("Received update from resolver, balancer config: %+v", state.BalancerConfig)
	// The errors checked here should ideally never happen because the
	// ServiceConfig in this case is prepared by the xdsResolver and is not
	// something that is received on the wire.
	lbCfg, ok := state.BalancerConfig.(*lbConfig)
	if !ok {
		b.logger.Warningf("xds: unexpected LoadBalancingConfig type: %T", state.BalancerConfig)
		return balancer.ErrBadResolverState
	}
	if lbCfg.ClusterName == "" {
		b.logger.Warningf("xds: no clusterName found in LoadBalancingConfig: %+v", lbCfg)
		return balancer.ErrBadResolverState
	}
	b.updateCh.Put(&ccUpdate{clusterName: lbCfg.ClusterName})
	return nil
}

// ResolverError handles errors reported by the xdsResolver.
func (b *cdsBalancer) ResolverError(err error) {
	if b.closed.HasFired() {
		b.logger.Warningf("xds: received resolver error {%v} after cdsBalancer was closed", err)
		return
	}
	b.updateCh.Put(&ccUpdate{err: err})
}

// UpdateSubConnState handles subConn updates from gRPC.
func (b *cdsBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	if b.closed.HasFired() {
		b.logger.Warningf("xds: received subConn update {%v, %v} after cdsBalancer was closed", sc, state)
		return
	}
	b.updateCh.Put(&scUpdate{subConn: sc, state: state})
}

// Close closes the cdsBalancer and the underlying edsBalancer.
func (b *cdsBalancer) Close() {
	b.closed.Fire()
	b.xdsClient.Close()
}

// ccWrapper wraps the balancer.ClientConn that was passed in to the CDS
// balancer during creation and intercepts the NewSubConn() call from the child
// policy. Other methods of the balancer.ClientConn interface are not overridden
// and hence get the original implementation.
type ccWrapper struct {
	balancer.ClientConn

	// The certificate providers in this HandshakeInfo are updated based on the
	// received security configuration in the Cluster resource.
	xdsHI *xdsinternal.HandshakeInfo
}

// NewSubConn intercepts NewSubConn() calls from the child policy and adds an
// address attribute which provides all information required by the xdsCreds
// handshaker to perform the TLS handshake.
func (ccw *ccWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	newAddrs := make([]resolver.Address, len(addrs))
	for i, addr := range addrs {
		newAddrs[i] = xdsinternal.SetHandshakeInfo(addr, ccw.xdsHI)
	}
	return ccw.ClientConn.NewSubConn(newAddrs, opts)
}
