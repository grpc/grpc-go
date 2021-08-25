/*
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
 */

package cdsbalancer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/credentials/local"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal"
	xdscredsinternal "google.golang.org/grpc/internal/credentials/xds"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/resolver"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
)

const (
	fakeProvider1Name = "fake-certificate-provider-1"
	fakeProvider2Name = "fake-certificate-provider-2"
	fakeConfig        = "my fake config"
	testSAN           = "test-san"
)

var (
	testSANMatchers = []matcher.StringMatcher{
		matcher.StringMatcherForTesting(newStringP(testSAN), nil, nil, nil, nil, true),
		matcher.StringMatcherForTesting(nil, newStringP(testSAN), nil, nil, nil, false),
		matcher.StringMatcherForTesting(nil, nil, newStringP(testSAN), nil, nil, false),
		matcher.StringMatcherForTesting(nil, nil, nil, nil, regexp.MustCompile(testSAN), false),
		matcher.StringMatcherForTesting(nil, nil, nil, newStringP(testSAN), nil, false),
	}
	fpb1, fpb2                   *fakeProviderBuilder
	bootstrapConfig              *bootstrap.Config
	cdsUpdateWithGoodSecurityCfg = xdsclient.ClusterUpdate{
		ClusterName: serviceName,
		SecurityCfg: &xdsclient.SecurityConfig{
			RootInstanceName:       "default1",
			IdentityInstanceName:   "default2",
			SubjectAltNameMatchers: testSANMatchers,
		},
	}
	cdsUpdateWithMissingSecurityCfg = xdsclient.ClusterUpdate{
		ClusterName: serviceName,
		SecurityCfg: &xdsclient.SecurityConfig{
			RootInstanceName: "not-default",
		},
	}
)

func newStringP(s string) *string {
	return &s
}

func init() {
	fpb1 = &fakeProviderBuilder{name: fakeProvider1Name}
	fpb2 = &fakeProviderBuilder{name: fakeProvider2Name}
	cfg1, _ := fpb1.ParseConfig(fakeConfig + "1111")
	cfg2, _ := fpb2.ParseConfig(fakeConfig + "2222")
	bootstrapConfig = &bootstrap.Config{
		CertProviderConfigs: map[string]*certprovider.BuildableConfig{
			"default1": cfg1,
			"default2": cfg2,
		},
	}
	certprovider.Register(fpb1)
	certprovider.Register(fpb2)
}

// fakeProviderBuilder builds new instances of fakeProvider and interprets the
// config provided to it as a string.
type fakeProviderBuilder struct {
	name string
}

func (b *fakeProviderBuilder) ParseConfig(config interface{}) (*certprovider.BuildableConfig, error) {
	s, ok := config.(string)
	if !ok {
		return nil, fmt.Errorf("providerBuilder %s received config of type %T, want string", b.name, config)
	}
	return certprovider.NewBuildableConfig(b.name, []byte(s), func(certprovider.BuildOptions) certprovider.Provider {
		return &fakeProvider{
			Distributor: certprovider.NewDistributor(),
			config:      s,
		}
	}), nil
}

func (b *fakeProviderBuilder) Name() string {
	return b.name
}

// fakeProvider is an implementation of the Provider interface which provides a
// method for tests to invoke to push new key materials.
type fakeProvider struct {
	*certprovider.Distributor
	config string
}

// Close helps implement the Provider interface.
func (p *fakeProvider) Close() {
	p.Distributor.Stop()
}

// setupWithXDSCreds performs all the setup steps required for tests which use
// xDSCredentials.
func setupWithXDSCreds(t *testing.T) (*fakeclient.Client, *cdsBalancer, *testEDSBalancer, *xdstestutils.TestClientConn, func()) {
	t.Helper()
	xdsC := fakeclient.NewClient()
	builder := balancer.Get(cdsName)
	if builder == nil {
		t.Fatalf("balancer.Get(%q) returned nil", cdsName)
	}
	// Create and pass xdsCredentials while building the CDS balancer.
	creds, err := xds.NewClientCredentials(xds.ClientOptions{
		FallbackCreds: local.NewCredentials(), // Placeholder fallback credentials.
	})
	if err != nil {
		t.Fatalf("Failed to create xDS client creds: %v", err)
	}
	// Create a new CDS balancer and pass it a fake balancer.ClientConn which we
	// can use to inspect the different calls made by the balancer.
	tcc := xdstestutils.NewTestClientConn(t)
	cdsB := builder.Build(tcc, balancer.BuildOptions{DialCreds: creds})

	// Override the creation of the EDS balancer to return a fake EDS balancer
	// implementation.
	edsB := newTestEDSBalancer()
	oldEDSBalancerBuilder := newChildBalancer
	newChildBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		edsB.parentCC = cc
		return edsB, nil
	}

	// Push a ClientConnState update to the CDS balancer with a cluster name.
	if err := cdsB.UpdateClientConnState(cdsCCS(clusterName, xdsC)); err != nil {
		t.Fatalf("cdsBalancer.UpdateClientConnState failed with error: %v", err)
	}

	// Make sure the CDS balancer registers a Cluster watch with the xDS client
	// passed via attributes in the above update.
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := xdsC.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != clusterName {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, clusterName)
	}

	return xdsC, cdsB.(*cdsBalancer), edsB, tcc, func() {
		newChildBalancer = oldEDSBalancerBuilder
		xdsC.Close()
	}
}

// makeNewSubConn invokes the NewSubConn() call on the balancer.ClientConn
// passed to the EDS balancer, and verifies that the CDS balancer forwards the
// call appropriately to its parent balancer.ClientConn with or without
// attributes bases on the value of wantFallback.
func makeNewSubConn(ctx context.Context, edsCC balancer.ClientConn, parentCC *xdstestutils.TestClientConn, wantFallback bool) (balancer.SubConn, error) {
	dummyAddr := "foo-address"
	addrs := []resolver.Address{{Addr: dummyAddr}}
	sc, err := edsCC.NewSubConn(addrs, balancer.NewSubConnOptions{})
	if err != nil {
		return nil, fmt.Errorf("NewSubConn(%+v) on parent ClientConn failed: %v", addrs, err)
	}

	select {
	case <-ctx.Done():
		return nil, errors.New("timeout when waiting for new SubConn")
	case gotAddrs := <-parentCC.NewSubConnAddrsCh:
		if len(gotAddrs) != 1 {
			return nil, fmt.Errorf("NewSubConn expected 1 address, got %d", len(gotAddrs))
		}
		if got, want := gotAddrs[0].Addr, addrs[0].Addr; got != want {
			return nil, fmt.Errorf("resolver.Address passed to parent ClientConn has address %q, want %q", got, want)
		}
		getHI := internal.GetXDSHandshakeInfoForTesting.(func(attr *attributes.Attributes) *xdscredsinternal.HandshakeInfo)
		hi := getHI(gotAddrs[0].Attributes)
		if hi == nil {
			return nil, errors.New("resolver.Address passed to parent ClientConn doesn't contain attributes")
		}
		if gotFallback := hi.UseFallbackCreds(); gotFallback != wantFallback {
			return nil, fmt.Errorf("resolver.Address HandshakeInfo uses fallback creds? %v, want %v", gotFallback, wantFallback)
		}
		if !wantFallback {
			if diff := cmp.Diff(testSANMatchers, hi.GetSANMatchersForTesting(), cmp.AllowUnexported(regexp.Regexp{})); diff != "" {
				return nil, fmt.Errorf("unexpected diff in the list of SAN matchers (-got, +want):\n%s", diff)
			}
		}
	}
	return sc, nil
}

// TestSecurityConfigWithoutXDSCreds tests the case where xdsCredentials are not
// in use, but the CDS balancer receives a Cluster update with security
// configuration. Verifies that no certificate providers are created, and that
// the address attributes added as part of the intercepted NewSubConn() method
// indicate the use of fallback credentials.
func (s) TestSecurityConfigWithoutXDSCreds(t *testing.T) {
	// This creates a CDS balancer, pushes a ClientConnState update with a fake
	// xdsClient, and makes sure that the CDS balancer registers a watch on the
	// provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithWatch(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Override the provider builder function to push on a channel. We do not
	// expect this function to be called as part of this test.
	providerCh := testutils.NewChannel()
	origBuildProvider := buildProvider
	buildProvider = func(c map[string]*certprovider.BuildableConfig, id, cert string, wi, wr bool) (certprovider.Provider, error) {
		p, err := origBuildProvider(c, id, cert, wi, wr)
		providerCh.Send(nil)
		return p, err
	}
	defer func() { buildProvider = origBuildProvider }()

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newChildBalancer function as part of test setup.
	cdsUpdate := xdsclient.ClusterUpdate{ClusterName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false, nil)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Make a NewSubConn and verify that the HandshakeInfo does not contain any
	// certificate providers, forcing the credentials implementation to use
	// fallback creds.
	if _, err := makeNewSubConn(ctx, edsB.parentCC, tcc, true); err != nil {
		t.Fatal(err)
	}

	// Again, since xdsCredentials are not in use, no certificate providers
	// should have been initialized by the CDS balancer.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := providerCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cds balancer created certificate providers when not using xds credentials")
	}
}

// TestNoSecurityConfigWithXDSCreds tests the case where xdsCredentials are in
// use, but the CDS balancer receives a Cluster update without security
// configuration. Verifies that no certificate providers are created, and that
// the address attributes added as part of the intercepted NewSubConn() method
// indicate the use of fallback credentials.
func (s) TestNoSecurityConfigWithXDSCreds(t *testing.T) {
	// This creates a CDS balancer which uses xdsCredentials, pushes a
	// ClientConnState update with a fake xdsClient, and makes sure that the CDS
	// balancer registers a watch on the provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithXDSCreds(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Override the provider builder function to push on a channel. We do not
	// expect this function to be called as part of this test.
	providerCh := testutils.NewChannel()
	origBuildProvider := buildProvider
	buildProvider = func(c map[string]*certprovider.BuildableConfig, id, cert string, wi, wr bool) (certprovider.Provider, error) {
		p, err := origBuildProvider(c, id, cert, wi, wr)
		providerCh.Send(nil)
		return p, err
	}
	defer func() { buildProvider = origBuildProvider }()

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newChildBalancer function as part of test setup. No security config is
	// passed to the CDS balancer as part of this update.
	cdsUpdate := xdsclient.ClusterUpdate{ClusterName: serviceName}
	wantCCS := edsCCS(serviceName, nil, false, nil)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Make a NewSubConn and verify that the HandshakeInfo does not contain any
	// certificate providers, forcing the credentials implementation to use
	// fallback creds.
	if _, err := makeNewSubConn(ctx, edsB.parentCC, tcc, true); err != nil {
		t.Fatal(err)
	}

	// Again, since no security configuration was received, no certificate
	// providers should have been initialized by the CDS balancer.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := providerCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cds balancer created certificate providers when not using xds credentials")
	}
}

// TestSecurityConfigNotFoundInBootstrap tests the case where the security
// config returned by the xDS server cannot be resolved based on the contents of
// the bootstrap config. Verifies that the balancer puts the channel in a failed
// state, and returns an error picker.
func (s) TestSecurityConfigNotFoundInBootstrap(t *testing.T) {
	// We test two cases here:
	// 0: Bootstrap contains security config. But received plugin instance name
	//    is not found in the bootstrap config.
	// 1: Bootstrap contains no security config.
	for i := 0; i < 2; i++ {
		// This creates a CDS balancer which uses xdsCredentials, pushes a
		// ClientConnState update with a fake xdsClient, and makes sure that the CDS
		// balancer registers a watch on the provided xdsClient.
		xdsC, cdsB, edsB, tcc, cancel := setupWithXDSCreds(t)
		defer func() {
			cancel()
			cdsB.Close()
		}()

		if i == 0 {
			// Set the bootstrap config used by the fake client.
			xdsC.SetBootstrapConfig(bootstrapConfig)
		}

		// Here we invoke the watch callback registered on the fake xdsClient. A bad
		// security config is passed here. So, we expect the CDS balancer to not
		// create an EDS balancer and instead reject this update and put the channel
		// in a bad state.
		xdsC.InvokeWatchClusterCallback(cdsUpdateWithMissingSecurityCfg, nil)

		// The CDS balancer has not yet created an EDS balancer. So, this bad
		// watcher update should not be forwarded forwarded to our fake EDS balancer
		// as an error.
		sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
		defer sCancel()
		if err := edsB.waitForResolverError(sCtx, nil); err != context.DeadlineExceeded {
			t.Fatal("eds balancer shouldn't get error (shouldn't be built yet)")
		}

		// Make sure the CDS balancer reports an error picker.
		ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer ctxCancel()
		if err := tcc.WaitForErrPicker(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCertproviderStoreError tests the case where the certprovider.Store
// returns an error when the CDS balancer attempts to create a provider.
func (s) TestCertproviderStoreError(t *testing.T) {
	// This creates a CDS balancer which uses xdsCredentials, pushes a
	// ClientConnState update with a fake xdsClient, and makes sure that the CDS
	// balancer registers a watch on the provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithXDSCreds(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Override the provider builder function to return an error.
	origBuildProvider := buildProvider
	buildProvider = func(c map[string]*certprovider.BuildableConfig, id, cert string, wi, wr bool) (certprovider.Provider, error) {
		return nil, errors.New("certprovider store error")
	}
	defer func() { buildProvider = origBuildProvider }()

	// Set the bootstrap config used by the fake client.
	xdsC.SetBootstrapConfig(bootstrapConfig)

	// Here we invoke the watch callback registered on the fake xdsClient. Even
	// though the received update is good, the certprovider.Store is configured
	// to return an error. So, CDS balancer should reject this config and report
	// an error.
	xdsC.InvokeWatchClusterCallback(cdsUpdateWithGoodSecurityCfg, nil)

	// The CDS balancer has not yet created an EDS balancer. So, this bad
	// watcher update should not be forwarded forwarded to our fake EDS balancer
	// as an error.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsB.waitForResolverError(sCtx, nil); err != context.DeadlineExceeded {
		t.Fatal("eds balancer shouldn't get error (shouldn't be built yet)")
	}

	// Make sure the CDS balancer reports an error picker.
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := tcc.WaitForErrPicker(ctx); err != nil {
		t.Fatal(err)
	}
}

func (s) TestSecurityConfigUpdate_BadToGood(t *testing.T) {
	// This creates a CDS balancer which uses xdsCredentials, pushes a
	// ClientConnState update with a fake xdsClient, and makes sure that the CDS
	// balancer registers a watch on the provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithXDSCreds(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Set the bootstrap config used by the fake client.
	xdsC.SetBootstrapConfig(bootstrapConfig)

	// Here we invoke the watch callback registered on the fake xdsClient. A bad
	// security config is passed here. So, we expect the CDS balancer to not
	// create an EDS balancer and instead reject this update and put the channel
	// in a bad state.
	xdsC.InvokeWatchClusterCallback(cdsUpdateWithMissingSecurityCfg, nil)

	// The CDS balancer has not yet created an EDS balancer. So, this bad
	// watcher update should not be forwarded forwarded to our fake EDS balancer
	// as an error.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if err := edsB.waitForResolverError(sCtx, nil); err != context.DeadlineExceeded {
		t.Fatal("eds balancer shouldn't get error (shouldn't be built yet)")
	}

	// Make sure the CDS balancer reports an error picker.
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := tcc.WaitForErrPicker(ctx); err != nil {
		t.Fatal(err)
	}

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newChildBalancer function as part of test setup.
	wantCCS := edsCCS(serviceName, nil, false, nil)
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdateWithGoodSecurityCfg, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Make a NewSubConn and verify that attributes are added.
	if _, err := makeNewSubConn(ctx, edsB.parentCC, tcc, false); err != nil {
		t.Fatal(err)
	}
}

// TestGoodSecurityConfig tests the case where the CDS balancer receives
// security configuration as part of the Cluster resource which can be
// successfully resolved using the bootstrap file contents. Verifies that
// certificate providers are created, and that the NewSubConn() call adds
// appropriate address attributes.
func (s) TestGoodSecurityConfig(t *testing.T) {
	// This creates a CDS balancer which uses xdsCredentials, pushes a
	// ClientConnState update with a fake xdsClient, and makes sure that the CDS
	// balancer registers a watch on the provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithXDSCreds(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Set the bootstrap config used by the fake client.
	xdsC.SetBootstrapConfig(bootstrapConfig)

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newChildBalancer function as part of test setup.
	wantCCS := edsCCS(serviceName, nil, false, nil)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdateWithGoodSecurityCfg, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Make a NewSubConn and verify that attributes are added.
	sc, err := makeNewSubConn(ctx, edsB.parentCC, tcc, false)
	if err != nil {
		t.Fatal(err)
	}

	// Invoke UpdateAddresses and verify that attributes are added.
	dummyAddr := "bar-address"
	addrs := []resolver.Address{{Addr: dummyAddr}}
	edsB.parentCC.UpdateAddresses(sc, addrs)
	select {
	case <-ctx.Done():
		t.Fatal("timeout when waiting for addresses to be updated on the subConn")
	case gotAddrs := <-tcc.UpdateAddressesAddrsCh:
		if len(gotAddrs) != 1 {
			t.Fatalf("UpdateAddresses expected 1 address, got %d", len(gotAddrs))
		}
		if got, want := gotAddrs[0].Addr, addrs[0].Addr; got != want {
			t.Fatalf("resolver.Address passed to parent ClientConn through UpdateAddresses() has address %q, want %q", got, want)
		}
		getHI := internal.GetXDSHandshakeInfoForTesting.(func(attr *attributes.Attributes) *xdscredsinternal.HandshakeInfo)
		hi := getHI(gotAddrs[0].Attributes)
		if hi == nil {
			t.Fatal("resolver.Address passed to parent ClientConn through UpdateAddresses() doesn't contain attributes")
		}
	}
}

func (s) TestSecurityConfigUpdate_GoodToFallback(t *testing.T) {
	// This creates a CDS balancer which uses xdsCredentials, pushes a
	// ClientConnState update with a fake xdsClient, and makes sure that the CDS
	// balancer registers a watch on the provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithXDSCreds(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Set the bootstrap config used by the fake client.
	xdsC.SetBootstrapConfig(bootstrapConfig)

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newChildBalancer function as part of test setup.
	wantCCS := edsCCS(serviceName, nil, false, nil)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdateWithGoodSecurityCfg, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Make a NewSubConn and verify that attributes are added.
	if _, err := makeNewSubConn(ctx, edsB.parentCC, tcc, false); err != nil {
		t.Fatal(err)
	}

	// Here we invoke the watch callback registered on the fake xdsClient with
	// an update which contains bad security config. So, we expect the CDS
	// balancer to forward this error to the EDS balancer and eventually the
	// channel needs to be put in a bad state.
	cdsUpdate := xdsclient.ClusterUpdate{ClusterName: serviceName}
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Make a NewSubConn and verify that fallback creds are used.
	if _, err := makeNewSubConn(ctx, edsB.parentCC, tcc, true); err != nil {
		t.Fatal(err)
	}
}

// TestSecurityConfigUpdate_GoodToBad tests the case where the first security
// config returned by the xDS server is successful, but the second update cannot
// be resolved based on the contents of the bootstrap config. Verifies that the
// error is forwarded to the EDS balancer (which was created as part of the
// first successful update).
func (s) TestSecurityConfigUpdate_GoodToBad(t *testing.T) {
	// This creates a CDS balancer which uses xdsCredentials, pushes a
	// ClientConnState update with a fake xdsClient, and makes sure that the CDS
	// balancer registers a watch on the provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithXDSCreds(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Set the bootstrap config used by the fake client.
	xdsC.SetBootstrapConfig(bootstrapConfig)

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newChildBalancer function as part of test setup.
	wantCCS := edsCCS(serviceName, nil, false, nil)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdateWithGoodSecurityCfg, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// Make a NewSubConn and verify that attributes are added.
	if _, err := makeNewSubConn(ctx, edsB.parentCC, tcc, false); err != nil {
		t.Fatal(err)
	}

	// Here we invoke the watch callback registered on the fake xdsClient with
	// an update which contains bad security config. So, we expect the CDS
	// balancer to forward this error to the EDS balancer and eventually the
	// channel needs to be put in a bad state.
	xdsC.InvokeWatchClusterCallback(cdsUpdateWithMissingSecurityCfg, nil)

	// We manually check that an error is forwarded to the EDS balancer instead
	// of using one of the helper methods on the testEDSBalancer, because all we
	// care here is whether an error is sent to it or not. We don't care about
	// the exact error.
	gotErr, err := edsB.resolverErrCh.Receive(ctx)
	if err != nil {
		t.Fatal("timeout waiting for CDS balancer to forward error to EDS balancer upon receipt of bad security config")
	}
	if gotErr == nil {
		t.Fatal("CDS balancer did not forward error to EDS balancer upon receipt of bad security config")
	}

	// Since the error being pushed here is not a resource-not-found-error, the
	// registered watch should not be cancelled.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := xdsC.WaitForCancelClusterWatch(sCtx); err != context.DeadlineExceeded {
		t.Fatal("cluster watch cancelled for a non-resource-not-found-error")
	}
}

// TestSecurityConfigUpdate_GoodToGood tests the case where the CDS balancer
// receives two different but successful updates with security configuration.
// Verifies that appropriate providers are created, and that address attributes
// are added.
func (s) TestSecurityConfigUpdate_GoodToGood(t *testing.T) {
	// This creates a CDS balancer which uses xdsCredentials, pushes a
	// ClientConnState update with a fake xdsClient, and makes sure that the CDS
	// balancer registers a watch on the provided xdsClient.
	xdsC, cdsB, edsB, tcc, cancel := setupWithXDSCreds(t)
	defer func() {
		cancel()
		cdsB.Close()
	}()

	// Override the provider builder function to push on a channel.
	providerCh := testutils.NewChannel()
	origBuildProvider := buildProvider
	buildProvider = func(c map[string]*certprovider.BuildableConfig, id, cert string, wi, wr bool) (certprovider.Provider, error) {
		p, err := origBuildProvider(c, id, cert, wi, wr)
		providerCh.Send(nil)
		return p, err
	}
	defer func() { buildProvider = origBuildProvider }()

	// Set the bootstrap config used by the fake client.
	xdsC.SetBootstrapConfig(bootstrapConfig)

	// Here we invoke the watch callback registered on the fake xdsClient. This
	// will trigger the watch handler on the CDS balancer, which will attempt to
	// create a new EDS balancer. The fake EDS balancer created above will be
	// returned to the CDS balancer, because we have overridden the
	// newChildBalancer function as part of test setup.
	cdsUpdate := xdsclient.ClusterUpdate{
		ClusterName: serviceName,
		SecurityCfg: &xdsclient.SecurityConfig{
			RootInstanceName:       "default1",
			SubjectAltNameMatchers: testSANMatchers,
		},
	}
	wantCCS := edsCCS(serviceName, nil, false, nil)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// We specified only the root provider. So, expect only one provider here.
	if _, err := providerCh.Receive(ctx); err != nil {
		t.Fatalf("Failed to create certificate provider upon receipt of security config")
	}

	// Make a NewSubConn and verify that attributes are added.
	if _, err := makeNewSubConn(ctx, edsB.parentCC, tcc, false); err != nil {
		t.Fatal(err)
	}

	// Push another update with a new security configuration.
	cdsUpdate = xdsclient.ClusterUpdate{
		ClusterName: serviceName,
		SecurityCfg: &xdsclient.SecurityConfig{
			RootInstanceName:       "default2",
			SubjectAltNameMatchers: testSANMatchers,
		},
	}
	if err := invokeWatchCbAndWait(ctx, xdsC, cdsWatchInfo{cdsUpdate, nil}, wantCCS, edsB); err != nil {
		t.Fatal(err)
	}

	// We specified only the root provider. So, expect only one provider here.
	if _, err := providerCh.Receive(ctx); err != nil {
		t.Fatalf("Failed to create certificate provider upon receipt of security config")
	}

	// Make a NewSubConn and verify that attributes are added.
	if _, err := makeNewSubConn(ctx, edsB.parentCC, tcc, false); err != nil {
		t.Fatal(err)
	}

	// The HandshakeInfo type does not expose its internals. So, we cannot
	// verify that the HandshakeInfo carried by the attributes have actually
	// been changed. This will be covered in e2e/interop tests.
	// TODO(easwars): Remove this TODO once appropriate e2e/intertop tests have
	// been added.
}
