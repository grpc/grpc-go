package stream

import (
	"google.golang.org/grpc"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Generic RPC stream for state of the world.
type Stream interface {
	grpc.ServerStream

	Send(*discovery.DiscoveryResponse) error
	Recv() (*discovery.DiscoveryRequest, error)
}

// Generic RPC Stream for the delta based xDS protocol.
type DeltaStream interface {
	grpc.ServerStream

	Send(*discovery.DeltaDiscoveryResponse) error
	Recv() (*discovery.DeltaDiscoveryRequest, error)
}

// StreamState will keep track of resource cache state per type on a stream.
type StreamState struct { // nolint:golint,revive
	// Indicates whether the delta stream currently has a wildcard watch
	wildcard bool

	// Provides the list of resources explicitly requested by the client
	// This list might be non-empty even when set as wildcard
	subscribedResourceNames map[string]struct{}

	// ResourceVersions contains a hash of the resource as the value and the resource name as the key.
	// This field stores the last state sent to the client.
	resourceVersions map[string]string

	// knownResourceNames contains resource names that a client has received previously (SOTW).
	knownResourceNames map[string]map[string]struct{}

	// First indicates whether the StreamState has been modified since its creation
	first bool

	// Ordered indicates whether we want an ordered ADS stream or not
	ordered bool
}

// NewStreamState initializes a stream state.
func NewStreamState(wildcard bool, initialResourceVersions map[string]string) StreamState {
	state := StreamState{
		wildcard:                wildcard,
		subscribedResourceNames: map[string]struct{}{},
		resourceVersions:        initialResourceVersions,
		first:                   true,
		knownResourceNames:      map[string]map[string]struct{}{},
		ordered:                 false, // Ordered comes from the first request since that's when we discover if they want ADS
	}

	if initialResourceVersions == nil {
		state.resourceVersions = make(map[string]string)
	}

	return state
}

// GetSubscribedResourceNames returns the list of resources currently explicitly subscribed to
// If the request is set to wildcard it may be empty
// Currently populated only when using delta-xds
func (s *StreamState) GetSubscribedResourceNames() map[string]struct{} {
	return s.subscribedResourceNames
}

// SetSubscribedResourceNames is setting the list of resources currently explicitly subscribed to
// It is decorrelated from the wildcard state of the stream
// Currently used only when using delta-xds
func (s *StreamState) SetSubscribedResourceNames(subscribedResourceNames map[string]struct{}) {
	s.subscribedResourceNames = subscribedResourceNames
}

// WatchesResources returns whether at least one of the resource provided is currently watch by the stream
// It is currently only applicable to delta-xds
// If the request is wildcard, it will always return true
// Otherwise it will compare the provided resources to the list of resources currently subscribed
func (s *StreamState) WatchesResources(resourceNames map[string]struct{}) bool {
	if s.IsWildcard() {
		return true
	}
	for resourceName := range resourceNames {
		if _, ok := s.subscribedResourceNames[resourceName]; ok {
			return true
		}
	}
	return false
}

func (s *StreamState) SetWildcard(wildcard bool) {
	s.wildcard = wildcard
}

// GetResourceVersions returns a map of current resources grouped by type URL.
func (s *StreamState) GetResourceVersions() map[string]string {
	return s.resourceVersions
}

// SetResourceVersions sets a list of resource versions by type URL and removes the flag
// of "first" since we can safely assume another request has come through the stream.
func (s *StreamState) SetResourceVersions(resourceVersions map[string]string) {
	s.first = false
	s.resourceVersions = resourceVersions
}

// IsFirst returns whether or not the state of the stream is based upon the initial request.
func (s *StreamState) IsFirst() bool {
	return s.first
}

// IsWildcard returns whether or not an xDS client requested in wildcard mode on the initial request.
func (s *StreamState) IsWildcard() bool {
	return s.wildcard
}

// GetKnownResourceNames returns the current known list of resources on a SOTW stream.
func (s *StreamState) GetKnownResourceNames(url string) map[string]struct{} {
	return s.knownResourceNames[url]
}

// SetKnownResourceNames sets a list of resource names in a stream utilizing the SOTW protocol.
func (s *StreamState) SetKnownResourceNames(url string, names map[string]struct{}) {
	s.knownResourceNames[url] = names
}

// SetKnownResourceNamesAsList is a helper function to set resource names as a slice input.
func (s *StreamState) SetKnownResourceNamesAsList(url string, names []string) {
	m := map[string]struct{}{}
	for _, name := range names {
		m[name] = struct{}{}
	}
	s.knownResourceNames[url] = m
}
