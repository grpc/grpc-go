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
 *
 */

package csm

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/metadata"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/attribute"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var defaultTestTimeout = 5 * time.Second

// clearEnv unsets all the environment variables relevant to the csm
// pluginOption.
func clearEnv() {
	os.Unsetenv(envconfig.XDSBootstrapFileContentEnv)
	os.Unsetenv(envconfig.XDSBootstrapFileNameEnv)

	os.Unsetenv("CSM_CANONICAL_SERVICE_NAME")
	os.Unsetenv("CSM_WORKLOAD_NAME")
}

func (s) TestGetLabels(t *testing.T) {
	clearEnv()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cpo := newPluginOption(ctx)

	tests := []struct {
		name                   string
		unsetHeader            bool // Should trigger "unknown" labels
		twoValues              bool // Should trigger "unknown" labels
		metadataExchangeLabels map[string]string
		labelsWant             map[string]string
	}{
		{
			name:                   "unset-labels",
			metadataExchangeLabels: nil,
			labelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",

				"csm.remote_workload_type":              "unknown",
				"csm.remote_workload_canonical_service": "unknown",
			},
		},
		{
			name: "metadata-partially-set",
			metadataExchangeLabels: map[string]string{
				"type":        "not-gce-or-gke",
				"ignore-this": "ignore-this",
			},
			labelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",

				"csm.remote_workload_type":              "not-gce-or-gke",
				"csm.remote_workload_canonical_service": "unknown",
			},
		},
		{
			name: "google-compute-engine",
			metadataExchangeLabels: map[string]string{ // All of these labels get emitted when type is "gcp_compute_engine".
				"type":              "gcp_compute_engine",
				"canonical_service": "canonical_service_val",
				"project_id":        "unique-id",
				"location":          "us-east",
				"workload_name":     "workload_name_val",
			},
			labelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",

				"csm.remote_workload_type":              "gcp_compute_engine",
				"csm.remote_workload_canonical_service": "canonical_service_val",
				"csm.remote_workload_project_id":        "unique-id",
				"csm.remote_workload_location":          "us-east",
				"csm.remote_workload_name":              "workload_name_val",
			},
		},
		// unset should go to unknown, ignore GKE labels that are not relevant
		// to GCE.
		{
			name: "google-compute-engine-labels-partially-set-with-extra",
			metadataExchangeLabels: map[string]string{
				"type":              "gcp_compute_engine",
				"canonical_service": "canonical_service_val",
				"project_id":        "unique-id",
				"location":          "us-east",
				// "workload_name": "", unset workload name - should become "unknown"
				"namespace_name": "should-be-ignored",
				"cluster_name":   "should-be-ignored",
			},
			labelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",

				"csm.remote_workload_type":              "gcp_compute_engine",
				"csm.remote_workload_canonical_service": "canonical_service_val",
				"csm.remote_workload_project_id":        "unique-id",
				"csm.remote_workload_location":          "us-east",
				"csm.remote_workload_name":              "unknown",
			},
		},
		{
			name: "google-kubernetes-engine",
			metadataExchangeLabels: map[string]string{
				"type":              "gcp_kubernetes_engine",
				"canonical_service": "canonical_service_val",
				"project_id":        "unique-id",
				"namespace_name":    "namespace_name_val",
				"cluster_name":      "cluster_name_val",
				"location":          "us-east",
				"workload_name":     "workload_name_val",
			},
			labelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",

				"csm.remote_workload_type":              "gcp_kubernetes_engine",
				"csm.remote_workload_canonical_service": "canonical_service_val",
				"csm.remote_workload_project_id":        "unique-id",
				"csm.remote_workload_cluster_name":      "cluster_name_val",
				"csm.remote_workload_namespace_name":    "namespace_name_val",
				"csm.remote_workload_location":          "us-east",
				"csm.remote_workload_name":              "workload_name_val",
			},
		},
		{
			name: "google-kubernetes-engine-labels-partially-set",
			metadataExchangeLabels: map[string]string{
				"type":              "gcp_kubernetes_engine",
				"canonical_service": "canonical_service_val",
				"project_id":        "unique-id",
				"namespace_name":    "namespace_name_val",
				// "cluster_name": "", cluster_name unset, should become "unknown"
				"location": "us-east",
				// "workload_name": "", workload_name unset, should become "unknown"
			},
			labelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",

				"csm.remote_workload_type":              "gcp_kubernetes_engine",
				"csm.remote_workload_canonical_service": "canonical_service_val",
				"csm.remote_workload_project_id":        "unique-id",
				"csm.remote_workload_cluster_name":      "unknown",
				"csm.remote_workload_namespace_name":    "namespace_name_val",
				"csm.remote_workload_location":          "us-east",
				"csm.remote_workload_name":              "unknown",
			},
		},
		{
			name: "unset-header",
			metadataExchangeLabels: map[string]string{
				"type":              "gcp_kubernetes_engine",
				"canonical_service": "canonical_service_val",
				"project_id":        "unique-id",
				"namespace_name":    "namespace_name_val",
				"cluster_name":      "cluster_name_val",
				"location":          "us-east",
				"workload_name":     "workload_name_val",
			},
			unsetHeader: true,
			labelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",

				"csm.remote_workload_type":              "unknown",
				"csm.remote_workload_canonical_service": "unknown",
			},
		},
		{
			name: "two-header-values",
			metadataExchangeLabels: map[string]string{
				"type":              "gcp_kubernetes_engine",
				"canonical_service": "canonical_service_val",
				"project_id":        "unique-id",
				"namespace_name":    "namespace_name_val",
				"cluster_name":      "cluster_name_val",
				"location":          "us-east",
				"workload_name":     "workload_name_val",
			},
			twoValues: true,
			labelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",

				"csm.remote_workload_type":              "unknown",
				"csm.remote_workload_canonical_service": "unknown",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pbLabels := &structpb.Struct{
				Fields: map[string]*structpb.Value{},
			}
			for k, v := range test.metadataExchangeLabels {
				pbLabels.Fields[k] = structpb.NewStringValue(v)
			}
			protoWireFormat, err := proto.Marshal(pbLabels)
			if err != nil {
				t.Fatalf("Error marshaling proto: %v", err)
			}
			metadataExchangeLabelsEncoded := base64.RawStdEncoding.EncodeToString(protoWireFormat)
			md := metadata.New(map[string]string{
				metadataExchangeKey: metadataExchangeLabelsEncoded,
			})
			if test.unsetHeader {
				md.Delete(metadataExchangeKey)
			}
			if test.twoValues {
				md.Append(metadataExchangeKey, "extra-val")
			}

			labelsGot := cpo.GetLabels(md)
			if diff := cmp.Diff(labelsGot, test.labelsWant); diff != "" {
				t.Fatalf("cpo.GetLabels returned unexpected value (-got, +want): %v", diff)
			}
		})
	}
}

// TestDetermineTargetCSM tests the helper function that determines whether a
// target is relevant to CSM or not, based off the rules outlined in design.
func (s) TestDetermineTargetCSM(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		targetCSM bool
	}{
		{
			name:      "dns:///localhost",
			target:    "normal-target-here",
			targetCSM: false,
		},
		{
			name:      "xds-no-authority",
			target:    "xds:///localhost",
			targetCSM: true,
		},
		{
			name:      "xds-traffic-director-authority",
			target:    "xds://traffic-director-global.xds.googleapis.com/localhost",
			targetCSM: true,
		},
		{
			name:      "xds-not-traffic-director-authority",
			target:    "xds://not-traffic-director-authority/localhost",
			targetCSM: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsedTarget, err := url.Parse(test.target)
			if err != nil {
				t.Fatalf("test target %v failed to parse: %v", test.target, err)
			}
			if got := determineTargetCSM(parsedTarget); got != test.targetCSM {
				t.Fatalf("cpo.determineTargetCSM(%v): got %v, want %v", test.target, got, test.targetCSM)
			}
		})
	}
}

// TestSetLabels tests the setting of labels, which snapshots the resource and
// environment. It mocks the resource and environment, and then calls into
// labels creation. It verifies to local labels created and metadata exchange
// labels emitted from the setLabels function.
func (s) TestSetLabels(t *testing.T) {
	clearEnv()
	tests := []struct {
		name                             string
		resourceKeyValues                map[string]string
		csmCanonicalServiceNamePopulated bool
		csmWorkloadNamePopulated         bool
		meshIDPopulated                  bool
		localLabelsWant                  map[string]string
		metadataExchangeLabelsWant       map[string]string
	}{
		{
			name:                             "no-type",
			csmCanonicalServiceNamePopulated: true,
			meshIDPopulated:                  true,
			resourceKeyValues:                map[string]string{},
			localLabelsWant: map[string]string{
				"csm.workload_canonical_service": "canonical_service_name_val", // env var populated so should be set.
				"csm.mesh_id":                    "mesh_id",                    // env var populated so should be set.
			},
			metadataExchangeLabelsWant: map[string]string{
				"type":              "unknown",
				"canonical_service": "canonical_service_name_val", // env var populated so should be set.
			},
		},
		{
			name:                     "gce",
			csmWorkloadNamePopulated: true,
			resourceKeyValues: map[string]string{
				"cloud.platform": "gcp_compute_engine",
				// csm workload name is an env var
				"cloud.availability_zone": "cloud_availability_zone_val",
				"cloud.region":            "should-be-ignored", // cloud.availability_zone takes precedence
				"cloud.account.id":        "cloud_account_id_val",
			},
			localLabelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",
			},
			metadataExchangeLabelsWant: map[string]string{
				"type":              "gcp_compute_engine",
				"canonical_service": "unknown",
				"workload_name":     "workload_name_val",
				"location":          "cloud_availability_zone_val",
				"project_id":        "cloud_account_id_val",
			},
		},
		{
			name: "gce-half-unset",
			resourceKeyValues: map[string]string{
				"cloud.platform": "gcp_compute_engine",
				// csm workload name is an env var
				"cloud.availability_zone": "cloud_availability_zone_val",
				"cloud.region":            "should-be-ignored", // cloud.availability_zone takes precedence
			},
			localLabelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",
			},
			metadataExchangeLabelsWant: map[string]string{
				"type":              "gcp_compute_engine",
				"canonical_service": "unknown",
				"workload_name":     "unknown",
				"location":          "cloud_availability_zone_val",
				"project_id":        "unknown",
			},
		},
		{
			name: "gke",
			resourceKeyValues: map[string]string{
				"cloud.platform": "gcp_kubernetes_engine",
				// csm workload name is an env var
				"cloud.region":       "cloud_region_val", // availability_zone isn't present, so this should become location
				"cloud.account.id":   "cloud_account_id_val",
				"k8s.namespace.name": "k8s_namespace_name_val",
				"k8s.cluster.name":   "k8s_cluster_name_val",
			},
			localLabelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",
			},
			metadataExchangeLabelsWant: map[string]string{
				"type":              "gcp_kubernetes_engine",
				"canonical_service": "unknown",
				"workload_name":     "unknown",
				"location":          "cloud_region_val",
				"project_id":        "cloud_account_id_val",
				"namespace_name":    "k8s_namespace_name_val",
				"cluster_name":      "k8s_cluster_name_val",
			},
		},
		{
			name: "gke-half-unset",
			resourceKeyValues: map[string]string{ // unset should become unknown
				"cloud.platform": "gcp_kubernetes_engine",
				// csm workload name is an env var
				"cloud.region": "cloud_region_val", // availability_zone isn't present, so this should become location
				// "cloud.account.id": "", // unset - should become unknown
				"k8s.namespace.name": "k8s_namespace_name_val",
				// "k8s.cluster.name": "", // unset - should become unknown
			},
			localLabelsWant: map[string]string{
				"csm.workload_canonical_service": "unknown",
				"csm.mesh_id":                    "unknown",
			},
			metadataExchangeLabelsWant: map[string]string{
				"type":              "gcp_kubernetes_engine",
				"canonical_service": "unknown",
				"workload_name":     "unknown",
				"location":          "cloud_region_val",
				"project_id":        "unknown",
				"namespace_name":    "k8s_namespace_name_val",
				"cluster_name":      "unknown",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			func() {
				if test.csmCanonicalServiceNamePopulated {
					os.Setenv("CSM_CANONICAL_SERVICE_NAME", "canonical_service_name_val")
					defer os.Unsetenv("CSM_CANONICAL_SERVICE_NAME")
				}
				if test.csmWorkloadNamePopulated {
					os.Setenv("CSM_WORKLOAD_NAME", "workload_name_val")
					defer os.Unsetenv("CSM_WORKLOAD_NAME")
				}
				if test.meshIDPopulated {
					os.Setenv("CSM_MESH_ID", "mesh_id")
					defer os.Unsetenv("CSM_MESH_ID")
				}
				var attributes []attribute.KeyValue
				for k, v := range test.resourceKeyValues {
					attributes = append(attributes, attribute.String(k, v))
				}
				// Return the attributes configured as part of the test in place
				// of reading from resource.
				attrSet := attribute.NewSet(attributes...)
				origGetAttrSet := getAttrSetFromResourceDetector
				getAttrSetFromResourceDetector = func(context.Context) *attribute.Set {
					return &attrSet
				}
				defer func() { getAttrSetFromResourceDetector = origGetAttrSet }()

				ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
				defer cancel()
				localLabelsGot, mdEncoded := constructMetadataFromEnv(ctx)
				if diff := cmp.Diff(localLabelsGot, test.localLabelsWant); diff != "" {
					t.Fatalf("constructMetadataFromEnv() want: %v, got %v", test.localLabelsWant, localLabelsGot)
				}

				verifyMetadataExchangeLabels(mdEncoded, test.metadataExchangeLabelsWant)
			}()
		})
	}
}

func verifyMetadataExchangeLabels(mdEncoded string, mdLabelsWant map[string]string) error {
	protoWireFormat, err := base64.RawStdEncoding.DecodeString(mdEncoded)
	if err != nil {
		return fmt.Errorf("error base 64 decoding metadata val: %v", err)
	}
	spb := &structpb.Struct{}
	if err := proto.Unmarshal(protoWireFormat, spb); err != nil {
		return fmt.Errorf("error unmarshaling proto wire format: %v", err)
	}
	fields := spb.GetFields()
	for k, v := range mdLabelsWant {
		if val, ok := fields[k]; !ok {
			if _, ok := val.GetKind().(*structpb.Value_StringValue); !ok {
				return fmt.Errorf("struct value for key %v should be string type", k)
			}
			if val.GetStringValue() != v {
				return fmt.Errorf("struct value for key %v got: %v, want %v", k, val.GetStringValue(), v)
			}
		}
	}
	if len(mdLabelsWant) != len(fields) {
		return fmt.Errorf("len(mdLabelsWant) = %v, len(mdLabelsGot) = %v", len(mdLabelsWant), len(fields))
	}
	return nil
}
