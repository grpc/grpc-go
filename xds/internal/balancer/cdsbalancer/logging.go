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

package cdsbalancer

import (
	"fmt"

	"google.golang.org/grpc/grpclog"
)

const (
	prefix = "[cds-lb %p] "
)

func infof(p *cdsBalancer, format string, args ...interface{}) {
	grpclog.Infof(fmt.Sprintf(prefix, p)+format, args...)
}

func warningf(p *cdsBalancer, format string, args ...interface{}) {
	grpclog.Warningf(fmt.Sprintf(prefix, p)+format, args...)
}

func errorf(p *cdsBalancer, format string, args ...interface{}) {
	grpclog.Errorf(fmt.Sprintf(prefix, p)+format, args...)
}
