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
 */

package controller

import (
	"google.golang.org/grpc/xds/internal/xdsclient/load"
)

// ReportLoad starts reporting loads to the management server the transport is
// configured to talk to.
//
// It returns a Store for the user to report loads and a function to cancel the
// load reporting.
func (c *Controller) ReportLoad() (*load.Store, func()) {
	return c.transport.ReportLoad()
}
