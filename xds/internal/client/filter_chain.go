/*
 *
 * Copyright 2021 gRPC authors.
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

package client

import (
	"errors"
	"fmt"
	"net"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/golang/protobuf/proto"

	"google.golang.org/grpc/xds/internal/version"
)

// Represents a wildcard IP prefix. Go stdlib `Contains()` method works for both
// v4 and v6 addresses when used on this wildcard address.
const emptyAddrMapKey = "0.0.0.0/0"

var (
	// Parsed wildcard IP prefix.
	_, zeroIP, _ = net.ParseCIDR("0.0.0.0/0")
)

// FilterChain captures information from within a FilterChain message in a
// Listener resource.
//
// Currently, this simply contains the security configuration found in the
// 'transport_socket' field of the filter chain. The actual set of filters
// associated with this filter chain are not captured here, since we do not
// support these filters on the server-side yet.
type FilterChain struct {
	// SecurityCfg contains transport socket security configuration.
	SecurityCfg *SecurityConfig
}

// SourceType specifies the connection source IP match type.
type SourceType int

const (
	// SourceTypeAny matches connection attempts from any source.
	SourceTypeAny SourceType = iota
	// SourceTypeSameOrLoopback matches connection attempts from the same host.
	SourceTypeSameOrLoopback
	// SourceTypeExternal matches connection attempts from a different host.
	SourceTypeExternal
)

// FilterChainManager contains all the match criteria specified through all
// filter chains in a single Listener resource. It also contains the default
// filter chain specified in the Listener resource. It provides two important
// pieces of functionality:
// 1. Validate the filter chains in an incoming Listener resource to make sure
//    that there aren't filter chains which contain the same match criteria.
// 2. As part of performing the above validation, it builds an internal data
//    structure which will if used to look up the matching filter chain at
//    connection time.
//
// The logic specified in the documentation around the xDS FilterChainMatch
// proto mentions 8 criteria to match on. gRPC does not support 4 of those, and
// we ignore filter chains which contain any of these unsupported fields at
// parsing time. Here we use the remaining 4 criteria to find a matching filter
// chain in the following order:
// Destination IP address, Source type, Source IP address, Source port.
// TODO: Ignore chains with unsupported fields *only* at connection time.
type FilterChainManager struct {
	// Destination prefix is the first match criteria that we support.
	// Therefore, this multi-stage map is indexed on destination prefixes
	// specified in the match criteria.
	// Unspecified destination prefix matches end up as a wildcard entry here
	// with a key of 0.0.0.0/0.
	dstPrefixMap map[string]*destPrefixEntry

	// At connection time, we do not have the actual destination prefix to match
	// on. We only have the real destination address of the incoming connection.
	// This means that we cannot use the above map at connection time. This list
	// contains the map entries from the above map that we can use at connection
	// time to find matching destination prefixes in O(n) time.
	//
	// TODO: Implement LC-trie to support logarithmic time lookups. If that
	// involves too much time/effort, sort this slice based on the netmask size.
	dstPrefixes []*destPrefixEntry

	def   *FilterChain // Default filter chain, if specified.
	fcCnt int          // Count of supported filter chains, for validation.
}

// destPrefixEntry is the value type of the map indexed on destination prefixes.
type destPrefixEntry struct {
	net *net.IPNet // The actual destination prefix.
	// For each specified source type in the filter chain match criteria, this
	// array points to the set of specified source prefixes.
	// Unspecified source type matches end up as a wildcard entry here with an
	// index of 0, which actually represents the source type `ANY`.
	srcTypeArr [3]*sourcePrefixes
}

// sourcePrefixes contains source prefix related information specified in the
// match criteria. These are pointed to by the array of source types.
type sourcePrefixes struct {
	// These are very similar to the 'dstPrefixMap' and 'dstPrefixes' field of
	// FilterChainManager. Go there for more info.
	srcPrefixMap map[string]*sourcePrefixEntry
	srcPrefixes  []*sourcePrefixEntry
}

// sourcePrefixEntry contains match criteria per source prefix.
type sourcePrefixEntry struct {
	net *net.IPNet // The actual source prefix.
	// Mapping from source ports specified in the match criteria to the actual
	// filter chain. Unspecified source port matches en up as a wildcard entry
	// here with a key of 0.
	srcPortMap map[int]*FilterChain
}

// NewFilterChainManager parses the received Listener resource and builds a
// FilterChainManager. Returns a non-nil error on validation failures.
//
// This function is only exported so that tests outside of this package can
// create a FilterChainManager.
func NewFilterChainManager(lis *v3listenerpb.Listener) (*FilterChainManager, error) {
	// Parse all the filter chains and build the internal data structures.
	fci := &FilterChainManager{dstPrefixMap: make(map[string]*destPrefixEntry)}
	if err := fci.addFilterChains(lis.GetFilterChains()); err != nil {
		return nil, err
	}

	// Retrieve the default filter chain. The match criteria specified on the
	// default filter chain is never used. The default filter chain simply gets
	// used when none of the other filter chains match.
	var def *FilterChain
	if dfc := lis.GetDefaultFilterChain(); dfc != nil {
		var err error
		if def, err = filterChainFromProto(dfc); err != nil {
			return nil, err
		}
	}
	fci.def = def

	// If there are no supported filter chains and no default filter chain, we
	// fail here. This will call the Listener resource to be NACK'ed.
	if fci.fcCnt == 0 && fci.def == nil {
		return nil, fmt.Errorf("no supported filter chains and no default filter chain")
	}
	return fci, nil
}

// addFilterChains parses the filter chains in fcs and adds the required
// internal data structures corresponding to the match criteria.
func (fci *FilterChainManager) addFilterChains(fcs []*v3listenerpb.FilterChain) error {
	for _, fc := range fcs {
		// Skip filter chains with unsupported match fields/criteria.
		fcm := fc.GetFilterChainMatch()
		if fcm.GetDestinationPort().GetValue() != 0 ||
			fcm.GetServerNames() != nil ||
			(fcm.GetTransportProtocol() != "" && fcm.TransportProtocol != "raw_buffer") ||
			fcm.GetApplicationProtocols() != nil {
			continue
		}

		// Extract the supported match criteria, which will be used by
		// successive addFilterChainsForXxx() functions.
		var dstPrefixes []*net.IPNet
		for _, pr := range fcm.GetPrefixRanges() {
			cidr := fmt.Sprintf("%s/%d", pr.GetAddressPrefix(), pr.GetPrefixLen().GetValue())
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				return fmt.Errorf("failed to parse destination prefix range: %+v", pr)
			}
			dstPrefixes = append(dstPrefixes, ipnet)
		}

		var srcType SourceType
		switch fcm.GetSourceType() {
		case v3listenerpb.FilterChainMatch_ANY:
			srcType = SourceTypeAny
		case v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK:
			srcType = SourceTypeSameOrLoopback
		case v3listenerpb.FilterChainMatch_EXTERNAL:
			srcType = SourceTypeExternal
		default:
			return fmt.Errorf("unsupported source type: %v", fcm.GetSourceType())
		}

		var srcPrefixes []*net.IPNet
		for _, pr := range fcm.GetSourcePrefixRanges() {
			cidr := fmt.Sprintf("%s/%d", pr.GetAddressPrefix(), pr.GetPrefixLen().GetValue())
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				return fmt.Errorf("failed to parse source prefix range: %+v", pr)
			}
			srcPrefixes = append(srcPrefixes, ipnet)
		}

		var srcPorts []int
		for _, port := range fcm.GetSourcePorts() {
			srcPorts = append(srcPorts, int(port))
		}

		// Build the internal representation of the filter chain match fields.
		if err := fci.addFilterChainsForDestPrefixes(dstPrefixes, srcType, srcPrefixes, srcPorts, fc); err != nil {
			return err
		}
		fci.fcCnt++
	}

	// Build the source and dest prefix slices used by Lookup().
	for _, dstPrefix := range fci.dstPrefixMap {
		fci.dstPrefixes = append(fci.dstPrefixes, dstPrefix)
		for _, st := range dstPrefix.srcTypeArr {
			if st == nil {
				continue
			}
			for _, srcPrefix := range st.srcPrefixMap {
				st.srcPrefixes = append(st.srcPrefixes, srcPrefix)
			}
		}
	}
	return nil
}

// addFilterChainsForDestPrefixes adds destination prefixes to the internal data
// structures and delegates control to addFilterChainsForSourceType to continue
// building the internal data structure.
func (fci *FilterChainManager) addFilterChainsForDestPrefixes(dstPrefixes []*net.IPNet, srcType SourceType, srcPrefixes []*net.IPNet, srcPorts []int, fc *v3listenerpb.FilterChain) error {
	if len(dstPrefixes) == 0 {
		// Use the wildcard IP when destination prefix is unspecified.
		if fci.dstPrefixMap[emptyAddrMapKey] == nil {
			fci.dstPrefixMap[emptyAddrMapKey] = &destPrefixEntry{net: zeroIP}
		}
		return fci.addFilterChainsForSourceType(fci.dstPrefixMap[emptyAddrMapKey], srcType, srcPrefixes, srcPorts, fc)
	}
	for _, prefix := range dstPrefixes {
		p := prefix.String()
		if fci.dstPrefixMap[p] == nil {
			fci.dstPrefixMap[p] = &destPrefixEntry{net: prefix}
		}
		if err := fci.addFilterChainsForSourceType(fci.dstPrefixMap[p], srcType, srcPrefixes, srcPorts, fc); err != nil {
			return err
		}
	}
	return nil
}

// addFilterChainsForSourceType adds source types to the internal data
// structures and delegates control to addFilterChainsForSourcePrefixes to
// continue building the internal data structure.
func (fci *FilterChainManager) addFilterChainsForSourceType(dstEntry *destPrefixEntry, srcType SourceType, srcPrefixes []*net.IPNet, srcPorts []int, fc *v3listenerpb.FilterChain) error {
	st := int(srcType)
	if dstEntry.srcTypeArr[st] == nil {
		dstEntry.srcTypeArr[st] = &sourcePrefixes{srcPrefixMap: make(map[string]*sourcePrefixEntry)}
	}
	return fci.addFilterChainsForSourcePrefixes(dstEntry.srcTypeArr[st].srcPrefixMap, srcPrefixes, srcPorts, fc)
}

// addFilterChainsForSourcePrefixes adds source prefixes to the internal data
// structures and delegates control to addFilterChainsForSourcePorts to continue
// building the internal data structure.
func (fci *FilterChainManager) addFilterChainsForSourcePrefixes(srcPrefixMap map[string]*sourcePrefixEntry, srcPrefixes []*net.IPNet, srcPorts []int, fc *v3listenerpb.FilterChain) error {
	if len(srcPrefixes) == 0 {
		// Use the wildcard IP when source prefix is unspecified.
		if srcPrefixMap[emptyAddrMapKey] == nil {
			srcPrefixMap[emptyAddrMapKey] = &sourcePrefixEntry{
				net:        zeroIP,
				srcPortMap: make(map[int]*FilterChain),
			}
		}
		return fci.addFilterChainsForSourcePorts(srcPrefixMap[emptyAddrMapKey], srcPorts, fc)
	}
	for _, prefix := range srcPrefixes {
		p := prefix.String()
		if srcPrefixMap[p] == nil {
			srcPrefixMap[p] = &sourcePrefixEntry{
				net:        prefix,
				srcPortMap: make(map[int]*FilterChain),
			}
		}
		if err := fci.addFilterChainsForSourcePorts(srcPrefixMap[p], srcPorts, fc); err != nil {
			return err
		}
	}
	return nil
}

// addFilterChainsForSourcePorts adds source ports to the internal data
// structures and completes the process of building the internal data structure.
// It is here that we determine if there are multiple filter chains with
// overlapping matching rules.
func (fci *FilterChainManager) addFilterChainsForSourcePorts(srcEntry *sourcePrefixEntry, srcPorts []int, fcProto *v3listenerpb.FilterChain) error {
	fc, err := filterChainFromProto(fcProto)
	if err != nil {
		return err
	}

	if len(srcPorts) == 0 {
		// Use the wildcard port '0', when source ports are unspecified.
		if curFC := srcEntry.srcPortMap[0]; curFC != nil {
			return errors.New("multiple filter chains with overlapping matching rules are defined")
		}
		srcEntry.srcPortMap[0] = fc
		return nil
	}
	for _, port := range srcPorts {
		if curFC := srcEntry.srcPortMap[port]; curFC != nil {
			return errors.New("multiple filter chains with overlapping matching rules are defined")
		}
		srcEntry.srcPortMap[port] = fc
	}
	return nil
}

// filterChainFromProto extracts the relevant information from the FilterChain
// proto and stores it in our internal representation. Currently, we only
// process the security configuration stored in the transport_socket field.
func filterChainFromProto(fc *v3listenerpb.FilterChain) (*FilterChain, error) {
	// If the transport_socket field is not specified, it means that the control
	// plane has not sent us any security config. This is fine and the server
	// will use the fallback credentials configured as part of the
	// xdsCredentials.
	ts := fc.GetTransportSocket()
	if ts == nil {
		return &FilterChain{}, nil
	}
	if name := ts.GetName(); name != transportSocketName {
		return nil, fmt.Errorf("transport_socket field has unexpected name: %s", name)
	}
	any := ts.GetTypedConfig()
	if any == nil || any.TypeUrl != version.V3DownstreamTLSContextURL {
		return nil, fmt.Errorf("transport_socket field has unexpected typeURL: %s", any.TypeUrl)
	}
	downstreamCtx := &v3tlspb.DownstreamTlsContext{}
	if err := proto.Unmarshal(any.GetValue(), downstreamCtx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DownstreamTlsContext in LDS response: %v", err)
	}
	if downstreamCtx.GetCommonTlsContext() == nil {
		return nil, errors.New("DownstreamTlsContext in LDS response does not contain a CommonTlsContext")
	}
	sc, err := securityConfigFromCommonTLSContext(downstreamCtx.GetCommonTlsContext())
	if err != nil {
		return nil, err
	}
	if sc.IdentityInstanceName == "" {
		return nil, errors.New("security configuration on the server-side does not contain identity certificate provider instance name")
	}
	sc.RequireClientCert = downstreamCtx.GetRequireClientCertificate().GetValue()
	if sc.RequireClientCert && sc.RootInstanceName == "" {
		return nil, errors.New("security configuration on the server-side does not contain root certificate provider instance name, but require_client_cert field is set")
	}
	return &FilterChain{SecurityCfg: sc}, nil
}

// FilterChainLookupParams wraps parameters to be passed to Lookup.
type FilterChainLookupParams struct {
	// IsUnspecified indicates whether the server is listening on a wildcard
	// address, "0.0.0.0" for IPv4 and "::" for IPv6. Only when this is set to
	// true, do we consider the destination prefixes specified in the filter
	// chain match criteria.
	IsUnspecifiedListener bool
	// DestAddr is the local address of an incoming connection.
	DestAddr net.IP
	// SourceAddr is the remote address of an incoming connection.
	SourceAddr net.IP
	// SourcePort is the remote port of an incoming connection.
	SourcePort int
}

// Lookup returns the most specific matching filter chain to be used for an
// incoming connection on the server side.
//
// Returns a non-nil error if no matching filter chain could be found or
// multiple matching filter chains were found, and in both cases, the incoming
// connection must be dropped.
func (fci *FilterChainManager) Lookup(params FilterChainLookupParams) (*FilterChain, error) {
	dstPrefixes := filterByDestinationPrefixes(fci.dstPrefixes, params.IsUnspecifiedListener, params.DestAddr)
	if len(dstPrefixes) == 0 {
		if fci.def != nil {
			return fci.def, nil
		}
		return nil, fmt.Errorf("no matching filter chain based on destination prefix match for %+v", params)
	}

	srcType := SourceTypeExternal
	if params.SourceAddr.Equal(params.DestAddr) || params.SourceAddr.IsLoopback() {
		srcType = SourceTypeSameOrLoopback
	}
	srcPrefixes := filterBySourceType(dstPrefixes, srcType)
	if len(srcPrefixes) == 0 {
		if fci.def != nil {
			return fci.def, nil
		}
		return nil, fmt.Errorf("no matching filter chain based on source type match for %+v", params)
	}
	srcPrefixEntry, err := filterBySourcePrefixes(srcPrefixes, params.SourceAddr)
	if err != nil {
		return nil, err
	}
	if fc := filterBySourcePorts(srcPrefixEntry, params.SourcePort); fc != nil {
		return fc, nil
	}
	if fci.def != nil {
		return fci.def, nil
	}
	return nil, fmt.Errorf("no matching filter chain after all match criteria for %+v", params)
}

// filterByDestinationPrefixes is the first stage of the filter chain
// matching algorithm. It takes the complete set of configured filter chain
// matchers and returns the most specific matchers based on the destination
// prefix match criteria (the prefixes which match the most number of bits).
func filterByDestinationPrefixes(dstPrefixes []*destPrefixEntry, isUnspecified bool, dstAddr net.IP) []*destPrefixEntry {
	if !isUnspecified {
		// Destination prefix matchers are considered only when the listener is
		// bound to the wildcard address.
		return dstPrefixes
	}
	var (
		matchingDstPrefixes []*destPrefixEntry
		maxSubnetMatch      int
	)
	for _, prefix := range dstPrefixes {
		if !prefix.net.Contains(dstAddr) {
			continue
		}
		matchSize, _ := prefix.net.Mask.Size()
		if matchSize < maxSubnetMatch {
			continue
		}
		if matchSize > maxSubnetMatch {
			maxSubnetMatch = matchSize
			matchingDstPrefixes = make([]*destPrefixEntry, 0, 1)
		}
		matchingDstPrefixes = append(matchingDstPrefixes, prefix)
	}
	return matchingDstPrefixes
}

// filterBySourceType is the second stage of the matching algorithm. It
// trims the filter chains based on the most specific source type match.
func filterBySourceType(dstPrefixes []*destPrefixEntry, srcType SourceType) []*sourcePrefixes {
	var (
		srcPrefixes      []*sourcePrefixes
		bestSrcTypeMatch int
	)
	for _, prefix := range dstPrefixes {
		var (
			srcPrefix *sourcePrefixes
			match     int
		)
		switch srcType {
		case SourceTypeExternal:
			match = int(SourceTypeExternal)
			srcPrefix = prefix.srcTypeArr[match]
		case SourceTypeSameOrLoopback:
			match = int(SourceTypeSameOrLoopback)
			srcPrefix = prefix.srcTypeArr[match]
		}
		if srcPrefix == nil {
			match = int(SourceTypeAny)
			srcPrefix = prefix.srcTypeArr[match]
		}
		if match < bestSrcTypeMatch {
			continue
		}
		if match > bestSrcTypeMatch {
			bestSrcTypeMatch = match
			srcPrefixes = make([]*sourcePrefixes, 0)
		}
		if srcPrefix != nil {
			// The source type array always has 3 entries, but these could be
			// nil if the appropriate source type match was not specified.
			srcPrefixes = append(srcPrefixes, srcPrefix)
		}
	}
	return srcPrefixes
}

// filterBySourcePrefixes is the third stage of the filter chain matching
// algorithm. It trims the filter chains based on the source prefix. At most one
// filter chain with the most specific match progress to the next stage.
func filterBySourcePrefixes(srcPrefixes []*sourcePrefixes, srcAddr net.IP) (*sourcePrefixEntry, error) {
	var (
		matchingSrcPrefixes []*sourcePrefixEntry
		maxSubnetMatch      int
	)
	for _, sp := range srcPrefixes {
		for _, prefix := range sp.srcPrefixes {
			if !prefix.net.Contains(srcAddr) {
				continue
			}
			matchSize, _ := prefix.net.Mask.Size()
			if matchSize < maxSubnetMatch {
				continue
			}
			if matchSize > maxSubnetMatch {
				maxSubnetMatch = matchSize
				matchingSrcPrefixes = make([]*sourcePrefixEntry, 0, 1)
			}
			matchingSrcPrefixes = append(matchingSrcPrefixes, prefix)
		}
	}
	if len(matchingSrcPrefixes) == 0 {
		// Finding no match is not an error condition. The caller will end up
		// using the default filter chain if one was configured.
		return nil, nil
	}
	// We expect at most a single matching source prefix entry at this point. If
	// we have multiple entries here, and some of their source port matchers had
	// wildcard entries, we could be left with more than one matching filter
	// chain and hence would have been flagged as an invalid configuration at
	// config validation time.
	if len(matchingSrcPrefixes) != 1 {
		return nil, errors.New("multiple matching filter chains")
	}
	return matchingSrcPrefixes[0], nil
}

// filterBySourcePorts is the last stage of the filter chain matching
// algorithm. It trims the filter chains based on the source ports.
func filterBySourcePorts(spe *sourcePrefixEntry, srcPort int) *FilterChain {
	if spe == nil {
		return nil
	}
	// A match could be a wildcard match (this happens when the match
	// criteria does not specify source ports) or a specific port match (this
	// happens when the match criteria specifies a set of ports and the source
	// port of the incoming connection matches one of the specified ports). The
	// latter is considered to be a more specific match.
	if fc := spe.srcPortMap[srcPort]; fc != nil {
		return fc
	}
	if fc := spe.srcPortMap[0]; fc != nil {
		return fc
	}
	return nil
}
