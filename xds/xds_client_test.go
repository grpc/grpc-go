package xds

import (
	"context"
	"testing"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/clients"
	"google.golang.org/grpc/xds/clients/grpctransport"
	"google.golang.org/grpc/xds/clients/xdsclient"
	"google.golang.org/grpc/xds/clients/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/clients/xdsclient/xdsresource/version"
)

type listenerUpdate struct {
	update any
	err    error
}

type listenerWatcher struct {
	updateCh *testutils.Channel
}

func newListenerWatcher() *listenerWatcher {
	return &listenerWatcher{updateCh: testutils.NewChannel()}
}

func (lw *listenerWatcher) OnUpdate(update xdsclient.ResourceData, onDone xdsclient.OnResourceProcessed) {
	lw.updateCh.Send(listenerUpdate{update: update.Raw()})
	onDone()
}

func (lw *listenerWatcher) OnError(err error, onDone xdsclient.OnResourceProcessed) {
	// When used with a go-control-plane management server that continuously
	// resends resources which are NACKed by the xDS client, using a `Replace()`
	// here and in OnResourceDoesNotExist() simplifies tests which will have
	// access to the most recently received error.
	lw.updateCh.Replace(listenerUpdate{err: err})
	onDone()
}

func (lw *listenerWatcher) OnResourceDoesNotExist(onDone xdsclient.OnResourceProcessed) {
	lw.updateCh.Replace(listenerUpdate{err: xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "Listener not found in received response")})
	onDone()
}

// ldsResourceType provides the resource-type specific functionality for a
// Listener resource.
//
// Implements the xdsclient.ResourceType interface.
type ldsResourceType struct{}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (ldsResourceType) Decode(opts xdsclient.DecodeOptions, resource any) (*xdsclient.DecodeResult, error) {
	return nil, nil
}

func (ldsResourceType) AllResourcesRequiredInSotW() bool {
	return true
}

func (ldsResourceType) TypeName() string {
	return "ldsResource"
}

func (ldsResourceType) TypeURL() string {
	return version.V3ListenerURL
}

// TestLDSWatch covers the case where a single watcher is registered for a
// single listener resource using the generic xDS client.
func TestLDSWatch(t *testing.T) {
	// Start an xDS management server which xDS client will connect to.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create node and authorities for xDS client.
	//
	// defaultAuthority represents the fallback authority used when a requested
	// xDS resource name doesn't explicitly specify an authority.
	//
	// authorities allows you to define multiple xDS management servers and
	// associate them with different parts of your application's configuration.
	// When a resource name explicitly includes an authority, the client uses
	// the corresponding configuration from this map.
	node := clients.Node{ID: uuid.New().String()}
	defaultAthourity := clients.Authority{XDSServers: []clients.ServerConfig{{ServerURI: mgmtServer.Address}}}
	authorities := map[string]clients.Authority{"": defaultAthourity}

	// Create gRPC transport builder for xDS client with server config to
	// mangement server uri and credentials as part of extension.
	serverConfig := clients.ServerConfig{ServerURI: mgmtServer.Address}
	grpcServerConfig := grpctransport.ServerConfig{Credentials: insecure.NewBundle()}
	serverConfig.Extensions = grpcServerConfig
	grpcTransportBuilder := &grpctransport.Builder{}

	// Create map of resource type implementations for the xDS client to refer.
	// to.
	ldsResourceType := ldsResourceType{}
	resourceTypes := map[string]xdsclient.ResourceType{}
	resourceTypes[ldsResourceType.TypeURL()] = ldsResourceType

	// Create xDS client config with all the above parameters.
	config := xdsclient.NewConfig(defaultAthourity, authorities, node, grpcTransportBuilder, resourceTypes)

	// Create xDS client usign the config.
	xdsClient, _ := xdsclient.New(config)

	// Name of the listener resource to be watcher using the xDS client
	ldsName := "xdsclient-test-lds-resource"
	rdsName := "xdsclient-test-rds-resource"

	// Register new listener watcher for the above lds resource.
	listenerWatcher := newListenerWatcher()
	ldsCancel := xdsClient.WatchResource(version.V3ListenerURL, ldsName, listenerWatcher)

	// Configure the management server to return a single listener
	// resource, corresponding to the one we registered a watch for.
	resources := e2e.UpdateOptions{
		NodeID:         node.ID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Cancel the watch and update the resource corresponding to the original
	// watch.  Ensure that the cancelled watch callback is not invoked.
	ldsCancel()
}
