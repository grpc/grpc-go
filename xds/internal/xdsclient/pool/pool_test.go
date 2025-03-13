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

package pool_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils/stats"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func init() {
	bs, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[{
			 "server_uri": %q,
			 "channel_creds": [{"type": "insecure"}]
		 }]`, "non-existent-management-server")),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, uuid.New().String())),
		CertificateProviders: map[string]json.RawMessage{
			"cert-provider-instance": json.RawMessage("{}"),
		},
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to create bootstrap configuration: %v", err))
	}

	envconfig.XDSBootstrapFileContent = string(bs)
}

// TestDefaultPool_LazyLoadBootstrapConfig verifies that the DefaultPool
// lazily loads the bootstrap configuration from environment variables when
// an xDS client is created for the first time.
func (s) TestDefaultPool_LazyLoadBootstrapConfig(t *testing.T) {
	defer func() {
		xdsclient.DefaultPool.UnsetBootstrapConfigForTesting()
	}()

	if cfg := xdsclient.DefaultPool.BootstrapConfigForTesting(); cfg != nil {
		t.Fatalf("DefaultPool.BootstrapConfigForTesting() = %v, want nil", cfg)
	}

	_, closeFunc, err := xdsclient.DefaultPool.NewClient(t.Name(), &stats.NoopMetricsRecorder{})
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer closeFunc()

	if xdsclient.DefaultPool.BootstrapConfigForTesting() == nil {
		t.Fatalf("DefaultPool.BootstrapConfigForTesting() = nil, want non-nil")
	}
}
