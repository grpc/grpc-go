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

package client

import (
	"time"
)

// Int64Range is a range for header range match.
type Int64Range struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// HeaderMatcher represents header matchers.
type HeaderMatcher struct {
	Name         string      `json:"name"`
	InvertMatch  *bool       `json:"invertMatch,omitempty"`
	ExactMatch   *string     `json:"exactMatch,omitempty"`
	RegexMatch   *string     `json:"regexMatch,omitempty"`
	PrefixMatch  *string     `json:"prefixMatch,omitempty"`
	SuffixMatch  *string     `json:"suffixMatch,omitempty"`
	RangeMatch   *Int64Range `json:"rangeMatch,omitempty"`
	PresentMatch *bool       `json:"presentMatch,omitempty"`
}

// Route represents route with matchers and action.
type Route struct {
	Path, Prefix, Regex *string
	Headers             []*HeaderMatcher
	Fraction            *uint32
	Action              map[string]uint32 // action is weighted clusters.
}

type rdsUpdate struct {
	routes []*Route
}
type rdsCallbackFunc func(rdsUpdate, error)

// watchRDS starts a listener watcher for the service..
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *Client) watchRDS(routeName string, cb rdsCallbackFunc) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		typeURL:     rdsURL,
		target:      routeName,
		rdsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(defaultWatchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
}
