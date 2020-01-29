// +build !appengine

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

// Package keys provides functionality required to build RLS request keys.
package keys

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/metadata"
)

// BuilderMap provides a mapping from a request path to the key builder to be
// used for that path.
// The BuilderMap is constructed by parsing the RouteLookupConfig received by
// the RLS balancer as part of its ServiceConfig, and is used by the picker in
// the data path to build the RLS keys to be used for a given request.
type BuilderMap map[string]builder

func validateKeyBuilderProto(kbs []*rlspb.GrpcKeyBuilder) error {
	if len(kbs) == 0 {
		return errors.New("rls: RouteLookupConfig does not contain any GrpcKeyBuilder")
	}

	// nameProto is a struct which contains only the fields of interest from
	// the GrpcKeyBuilder_Name proto. This is to make sure that we don't use
	// the XXX_* fields (which are part of the struct corresponding to
	// GrpcKeyBuilder_Name proto in the generated pb.go files) as keys in the
	// map to determine whether or not we have this Name.
	type nameProto struct {
		service string
		method  string
	}
	seenNames := make(map[nameProto]bool)
	for _, kb := range kbs {
		names := kb.GetNames()
		if len(names) == 0 {
			return fmt.Errorf("rls: GrpcKeyBuilder in RouteLookupConfig does not contain any Name {%+v}", kbs)
		}

		for _, name := range names {
			if name.GetService() == "" {
				return fmt.Errorf("rls: GrpcKeyBuilder in RouteLookupConfig contains a Name field with no Service {%+v}", kbs)
			}
			n := nameProto{service: name.GetService(), method: name.GetMethod()}
			if seenNames[n] {
				return fmt.Errorf("rls: GrpcKeyBuilder in RouteLookupConfig contains repeated Name field {%+v}", kbs)
			}
			seenNames[n] = true
		}

		seenKeys := make(map[string]bool)
		for _, matcher := range kb.GetHeaders() {
			if matcher.GetRequiredMatch() {
				return fmt.Errorf("rls: GrpcKeyBuilder in RouteLookupConfig has required_match field set {%+v}", kbs)
			}
			key := matcher.GetKey()
			if seenKeys[key] {
				return fmt.Errorf("rls: GrpcKeyBuilder in RouteLookupConfig contains repeated Key field in headers {%+v}", kbs)
			}
			seenKeys[key] = true
		}
	}
	return nil
}

// MakeBuilderMap parses the provided RouteLookupConfig proto and builds a map
// of key builders.
//
// The following conditions are validated, and an error is returned if any of
// them is not met:
// grpc_keybuilders field
// * must have at least one entry
// * must not have two entries with the same Name
// * must not have any entry with a Name with the service field unset or empty
// * must not have any entries without a Name
// * must not have a headers entry that has required_match set
// * must not have two headers entries with the same key within one entry
func MakeBuilderMap(cfg *rlspb.RouteLookupConfig) (BuilderMap, error) {
	kbs := cfg.GetGrpcKeybuilders()
	if err := validateKeyBuilderProto(kbs); err != nil {
		return nil, err
	}

	bm := make(map[string]builder)
	for _, kb := range kbs {
		var matchers []matcher
		for _, h := range kb.GetHeaders() {
			matchers = append(matchers, matcher{key: h.GetKey(), names: h.GetNames()})
		}
		b := builder{matchers: matchers}
		for _, name := range kb.GetNames() {
			path := "/" + name.GetService() + "/" + name.GetMethod()
			bm[path] = b
		}
	}
	return bm, nil
}

// KeyMap represents the RLS keys to be used for a request.
type KeyMap struct {
	// Map is the representation of an RLS key as a Go map. This is used when
	// an actual RLS request is to be sent out on the wire, since the
	// RouteLookupRequest proto expects a Go map.
	Map map[string]string
	// Str is the representation of an RLS key as a string, sorted by keys.
	// Since the RLS keys are part of the cache key in the request cache
	// maintained by the RLS balancer, and Go maps cannot be used as keys for
	// Go maps (the cache is implemented as a map), we need a stringified
	// version of it.
	Str string
}

// RLSKey builds the RLS keys to be used for the given request, identified by
// the request path and the request headers stored in metadata.
func (bm BuilderMap) RLSKey(md metadata.MD, path string) KeyMap {
	b, ok := bm[path]
	if !ok {
		i := strings.LastIndex(path, "/")
		b, ok = bm[path[:i+1]]
		if !ok {
			return KeyMap{}
		}
	}
	return b.keys(md)
}

// builder provides the actual functionality of building RLS keys. These are
// stored in the BuilderMap.
// While processing a pick, the picker looks in the BuilderMap for the
// appropriate builder to be used for the given RPC.  For each of the matchers
// in the found builder, we iterate over the list of request headers (available
// as metadata in the context). Once a header matches one of the names in the
// matcher, we set the value of the header in the keyMap (with the key being
// the one found in the matcher) and move on to the next matcher.  If no
// KeyBuilder was found in the map, or no header match was found, an empty
// keyMap is returned.
type builder struct {
	matchers []matcher
}

// matcher helps extract a key from request headers based on a given name.
type matcher struct {
	// The key used in the keyMap sent as part of the RLS request.
	key string
	// List of header names which can supply the value for this key.
	names []string
}

func (b builder) keys(md metadata.MD) KeyMap {
	kvMap := make(map[string]string)
	for _, m := range b.matchers {
		for _, name := range m.names {
			if vals := md.Get(name); vals != nil {
				kvMap[m.key] = strings.Join(vals, ",")
				break
			}
		}
	}
	return KeyMap{Map: kvMap, Str: mapToString(kvMap)}
}

func mapToString(kv map[string]string) string {
	var keys []string
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i != 0 {
			fmt.Fprint(&sb, ",")
		}
		fmt.Fprintf(&sb, "%s=%s", k, kv[k])
	}
	return sb.String()
}
