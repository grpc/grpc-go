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

package e2e

import (
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/grpc/internal/testutils/xds/bootstrap"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
)

type controlPlane struct {
	server           *e2e.ManagementServer
	nodeID           string
	bootstrapContent string
}

func newControlPlane() (*controlPlane, error) {
	// Spin up an xDS management server on a local port.
	server, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to spin up the xDS management server: %v", err)
	}

	nodeID := uuid.New().String()
	bootstrapContentBytes, err := bootstrap.Contents(bootstrap.Options{
		NodeID:                             nodeID,
		ServerURI:                          server.Address,
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
	})
	if err != nil {
		server.Stop()
		return nil, fmt.Errorf("failed to create bootstrap file: %v", err)
	}

	return &controlPlane{
		server:           server,
		nodeID:           nodeID,
		bootstrapContent: string(bootstrapContentBytes),
	}, nil
}

func (cp *controlPlane) stop() {
	cp.server.Stop()
}
