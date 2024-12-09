/*
 *
 * Copyright 2024 gRPC authors.
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

package xdsclient

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
	igrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/xds/clients"
	"google.golang.org/grpc/xds/clients/xdsclient/xdsresource"
)

// xdsChannelEventHandler wraps callbacks used to notify the xDS client about
// events on the xdsChannel. Methods in this interface may be invoked
// concurrently and the xDS client implementation needs to handle them in a
// thread-safe manner.
type xdsChannelEventHandler interface {
	// adsStreamFailure is called when the xdsChannel encounters an ADS stream
	// failure.
	adsStreamFailure(error)

	// adsResourceUpdate is called when the xdsChannel receives an ADS response
	// from the xDS management server. The callback is provided with the
	// following:
	//   - the resource type of the resources in the response
	//   - a map of resources in the response, keyed by resource name
	//   - the metadata associated with the response
	//   - a callback to be invoked when the updated is processed
	adsResourceUpdate(ResourceType, map[string]dataAndErrTuple, updateMetadata, func())

	// adsResourceDoesNotExist is called when the xdsChannel determines that a
	// requested ADS resource does not exist.
	adsResourceDoesNotExist(ResourceType, string)
}

// xdsChannelOpts holds the options for creating a new xdsChannel.
type xdsChannelOpts struct {
	transport          clients.Transport       // Takes ownership of this transport.
	serverConfig       *clients.ServerConfig   // Configuration of the server to connect to.
	config             *Config                 // Complete bootstrap configuration, used to decode resources.
	resourceTypes      map[string]ResourceType // Function to retrieve resource parsing functionality, based on resource type.
	eventHandler       xdsChannelEventHandler  // Callbacks for ADS stream events.
	backoff            func(int) time.Duration // Backoff function to use for stream retries. Defaults to exponential backoff, if unset.
	watchExpiryTimeout time.Duration           // Timeout for ADS resource watch expiry.
	logPrefix          string                  // Prefix to use for logging.
}

// newXDSChannel creates a new xdsChannel instance with the provided options.
// It performs basic validation on the provided options and initializes the
// xdsChannel with the necessary components.
func newXDSChannel(opts xdsChannelOpts) (*xdsChannel, error) {
	switch {
	case opts.transport == nil:
		return nil, errors.New("xdsChannel: transport is nil")
	case opts.serverConfig == nil:
		return nil, errors.New("xdsChannel: serverConfig is nil")
	case opts.config == nil:
		return nil, errors.New("xdsChannel: config is nil")
	case opts.resourceTypes == nil:
		return nil, errors.New("xdsChannel: resourceTypes is nil")
	case opts.eventHandler == nil:
		return nil, errors.New("xdsChannel: eventHandler is nil")
	}

	xc := &xdsChannel{
		transport:     opts.transport,
		serverConfig:  opts.serverConfig,
		config:        opts.config,
		resourceTypes: opts.resourceTypes,
		eventHandler:  opts.eventHandler,
		closed:        grpcsync.NewEvent(),
	}

	l := grpclog.Component("xds")
	logPrefix := opts.logPrefix + fmt.Sprintf("[xds-channel %p] ", xc)
	xc.logger = igrpclog.NewPrefixLogger(l, logPrefix)

	if opts.backoff == nil {
		opts.backoff = backoff.DefaultExponential.Backoff
	}
	xc.ads = newStreamImpl(streamOpts{
		transport:          xc.transport,
		eventHandler:       xc,
		backoff:            opts.backoff,
		nodeProto:          xc.config.Node.ToProto(),
		watchExpiryTimeout: opts.watchExpiryTimeout,
		logPrefix:          logPrefix,
	})
	return xc, nil
}

// xdsChannel represents a client channel to a management server, and is
// responsible for managing the lifecycle of the ADS and LRS streams. It invokes
// callbacks on the registered event handler for various ADS stream events.
type xdsChannel struct {
	// The following fields are initialized at creation time and are read-only
	// after that, and hence need not be guarded by a mutex.
	transport     clients.Transport       // Takes ownership of this transport (used to make streaming calls).
	ads           *streamImpl             // An ADS stream to the management server.
	serverConfig  *clients.ServerConfig   // Configuration of the server to connect to.
	config        *Config                 // Complete bootstrap configuration, used to decode resources.
	resourceTypes map[string]ResourceType // Function to retrieve resource parsing functionality, based on resource type.
	eventHandler  xdsChannelEventHandler  // Callbacks for ADS stream events.
	logger        *igrpclog.PrefixLogger  // Logger to use for logging.
	closed        *grpcsync.Event         // Fired when the channel is closed.
}

func (xc *xdsChannel) close() {
	xc.closed.Fire()
	xc.ads.stop()
	xc.transport.Close()
	xc.logger.Infof("Shutdown")
}

// subscribe adds a subscription for the given resource name of the given
// resource type on the ADS stream.
func (xc *xdsChannel) subscribe(typ ResourceType, name string) {
	if xc.closed.HasFired() {
		if xc.logger.V(2) {
			xc.logger.Infof("Attempt to subscribe to an xDS resource of type %s and name %q on a closed channel", typ.TypeName(), name)
		}
		return
	}
	xc.ads.Subscribe(typ, name)
}

// unsubscribe removes the subscription for the given resource name of the given
// resource type from the ADS stream.
func (xc *xdsChannel) unsubscribe(typ ResourceType, name string) {
	if xc.closed.HasFired() {
		if xc.logger.V(2) {
			xc.logger.Infof("Attempt to unsubscribe to an xDS resource of type %s and name %q on a closed channel", typ.TypeName(), name)
		}
		return
	}
	xc.ads.unsubscribe(typ, name)
}

// The following OnADSXxx() methods implement the streamEventHandler interface
// and are invoked by the ADS stream implementation.

// OnADSStreamError is invoked when an error occurs on the ADS stream. It
// propagates the update to the xDS client.
func (xc *xdsChannel) onADSStreamError(err error) {
	if xc.closed.HasFired() {
		if xc.logger.V(2) {
			xc.logger.Infof("Received ADS stream error on a closed xdsChannel: %v", err)
		}
		return
	}
	xc.eventHandler.adsStreamFailure(err)
}

// OnADSWatchExpiry is invoked when a watch for a resource expires. It
// propagates the update to the xDS client.
func (xc *xdsChannel) onADSWatchExpiry(typ ResourceType, name string) {
	if xc.closed.HasFired() {
		if xc.logger.V(2) {
			xc.logger.Infof("Received ADS resource watch expiry for resource %q on a closed xdsChannel", name)
		}
		return
	}
	xc.eventHandler.adsResourceDoesNotExist(typ, name)
}

// OnADSResponse is invoked when a response is received on the ADS stream. It
// decodes the resources in the response, and propagates the updates to the xDS
// client.
//
// It returns the list of resource names in the response and any errors
// encountered during decoding.
func (xc *xdsChannel) onADSResponse(resp response, onDone func()) ([]string, error) {
	if xc.closed.HasFired() {
		if xc.logger.V(2) {
			xc.logger.Infof("Received an update from the ADS stream on closed ADS stream")
		}
		return nil, errors.New("xdsChannel is closed")
	}

	// Lookup the resource parser based on the resource type.
	rType := xc.resourceTypes[resp.typeURL]
	if rType == nil {
		return nil, xdsresource.NewErrorf(xdsresource.ErrorTypeResourceTypeUnsupported, "Resource type URL %q unknown in response from server", resp.typeURL)
	}

	// Decode the resources and build the list of resource names to return.
	opts := DecodeOptions{
		Config:       xc.config,
		ServerConfig: xc.serverConfig,
	}
	updates, md, err := decodeResponse(opts, rType, resp)
	var names []string
	for name := range updates {
		names = append(names, name)
	}

	xc.eventHandler.adsResourceUpdate(rType, updates, md, onDone)
	return names, err
}

// decodeResponse decodes the resources in the given ADS response.
//
// The opts parameter provides configuration options for decoding the resources.
// The rType parameter specifies the resource type parser to use for decoding
// the resources.
//
// The returned map contains a key for each resource in the response, with the
// value being either the decoded resource data or an error if decoding failed.
// The returned metadata includes the version of the response, the timestamp of
// the update, and the status of the update (ACKed or NACKed).
//
// If there are any errors decoding the resources, the metadata will indicate
// that the update was NACKed, and the returned error will contain information
// about all errors encountered by this function.
func decodeResponse(opts DecodeOptions, rType ResourceType, resp response) (map[string]dataAndErrTuple, updateMetadata, error) {
	timestamp := time.Now()
	md := updateMetadata{
		version:   resp.version,
		timestamp: timestamp,
	}

	topLevelErrors := make([]error, 0)          // Tracks deserialization errors, where we don't have a resource name.
	perResourceErrors := make(map[string]error) // Tracks resource validation errors, where we have a resource name.
	ret := make(map[string]dataAndErrTuple)     // Return result, a map from resource name to either resource data or error.
	for _, r := range resp.resources {
		result, err := rType.Decode(opts, r)

		// Name field of the result is left unpopulated only when resource
		// deserialization fails.
		name := ""
		if result != nil {
			name = xdsresource.ParseName(result.Name).String()
		}
		if err == nil {
			ret[name] = dataAndErrTuple{resource: result.Resource}
			continue
		}
		if name == "" {
			topLevelErrors = append(topLevelErrors, err)
			continue
		}
		perResourceErrors[name] = err
		// Add place holder in the map so we know this resource name was in
		// the response.
		ret[name] = dataAndErrTuple{err: err}
	}

	if len(topLevelErrors) == 0 && len(perResourceErrors) == 0 {
		md.status = serviceStatusACKed
		return ret, md, nil
	}

	md.status = serviceStatusNACKed
	errRet := combineErrors(rType.TypeName(), topLevelErrors, perResourceErrors)
	md.errState = &updateErrorMetadata{
		version:   resp.version,
		err:       errRet,
		timestamp: timestamp,
	}
	return ret, md, errRet
}

func combineErrors(rType string, topLevelErrors []error, perResourceErrors map[string]error) error {
	var errStrB strings.Builder
	errStrB.WriteString(fmt.Sprintf("error parsing %q response: ", rType))
	if len(topLevelErrors) > 0 {
		errStrB.WriteString("top level errors: ")
		for i, err := range topLevelErrors {
			if i != 0 {
				errStrB.WriteString(";\n")
			}
			errStrB.WriteString(err.Error())
		}
	}
	if len(perResourceErrors) > 0 {
		var i int
		for name, err := range perResourceErrors {
			if i != 0 {
				errStrB.WriteString(";\n")
			}
			i++
			errStrB.WriteString(fmt.Sprintf("resource %q: %v", name, err.Error()))
		}
	}
	return errors.New(errStrB.String())
}

func (xc *xdsChannel) triggerResourceNotFoundForTesting(rType ResourceType, resourceName string) error {
	if xc.closed.HasFired() {
		return fmt.Errorf("triggerResourceNotFoundForTesting() called on a closed channel")
	}
	if xc.logger.V(2) {
		xc.logger.Infof("Triggering resource not found for type: %s, resource name: %s", rType.TypeName(), resourceName)
	}
	xc.ads.TriggerResourceNotFoundForTesting(rType, resourceName)
	return nil
}
