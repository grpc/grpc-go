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

// Package meshca implements a certificate provider which gets certificates signed by the MeshCa.
//
// TODO(easwars): Implement all required functionality. This is only skeleton code for now.
package meshca

import (
	"context"

	"google.golang.org/grpc/credentials/tls/certprovider"
	configpb "google.golang.org/grpc/credentials/tls/certprovider/meshca/proto"
	servicepb "istio.io/istio/security/proto/providers/google"
)

const meshcaName = "GoogleMeshCa"

var (
	req = servicepb.MeshCertificateRequest{}
	cfg = configpb.GoogleMeshCaConfig{}
)

type builder struct{}

func (b *builder) Build(config certprovider.StableConfig) certprovider.Provider {
	return &provider{
		dist: certprovider.NewDistributor(),
	}
}

func (b *builder) ParseConfig(cfg interface{}) (certprovider.StableConfig, error) {
	return &config{}, nil
}

func (b *builder) Name() string {
	return meshcaName
}

type config struct{}

func (c *config) Canonical() []byte {
	return nil
}

type provider struct {
	dist *certprovider.Distributor
}

func (p *provider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	return p.dist.KeyMaterial(ctx)
}

func (p *provider) Close() {
	p.dist.Stop()
}
