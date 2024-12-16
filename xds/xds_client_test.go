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

// listenerResourceType provides the resource-type specific functionality for a
// Listener resource.
//
// Implements the xdsclient.ResourceType interface.
type listenerResourceType struct{}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (listenerResourceType) Decode(opts xdsclient.DecodeOptions, resource any) (*xdsclient.DecodeResult, error) {
	return nil, nil
}

func (listenerResourceType) AllResourcesRequiredInSotW() bool {
	return true
}

func (listenerResourceType) TypeName() string {
	return "ListenerResource"
}

func (listenerResourceType) TypeURL() string {
	return version.V3ListenerURL
}

func TestLDSWatch(t *testing.T) {
	ldsName := "xdsclient-test-lds-resource"
	rdsName := "xdsclient-test-rds-resource"

	resource := e2e.DefaultClientListener(ldsName, rdsName)

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	node := clients.Node{ID: uuid.New().String()}
	defaultAthourity := clients.Authority{XDSServers: []clients.ServerConfig{{ServerURI: mgmtServer.Address}}}
	authorities := map[string]clients.Authority{"": defaultAthourity}

	serverConfig := clients.ServerConfig{ServerURI: mgmtServer.Address}
	grpcServerConfig := grpctransport.ServerConfig{Credentials: insecure.NewBundle()}
	serverConfig.Extensions = grpcServerConfig
	grpcTransportBuilder := &grpctransport.Builder{}

	listenerResourceType := listenerResourceType{}
	resourceTypes := map[string]xdsclient.ResourceType{}
	resourceTypes[listenerResourceType.TypeURL()] = listenerResourceType

	config := xdsclient.NewConfig(defaultAthourity, authorities, node, grpcTransportBuilder, resourceTypes)

	xdsClient, _ := xdsclient.New(config)

	listenerWatcher := newListenerWatcher()

	ldsCancel := xdsClient.WatchResource(version.V3ListenerURL, ldsName, listenerWatcher)

	// Configure the management server to return a single listener
	// resource, corresponding to the one we registered a watch for.
	resources := e2e.UpdateOptions{
		NodeID:         node.ID,
		Listeners:      []*v3listenerpb.Listener{resource},
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
