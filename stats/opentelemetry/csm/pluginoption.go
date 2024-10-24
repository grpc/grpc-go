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

// Package csm contains utilities for Google Cloud Service Mesh observability.
package csm

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats/opentelemetry/internal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

var logger = grpclog.Component("csm-observability-plugin")

// pluginOption emits CSM Labels from the environment and metadata exchange
// for csm channels and all servers.
//
// Do not use this directly; use newPluginOption instead.
type pluginOption struct {
	// localLabels are the labels that identify the local environment a binary
	// is run in, and will be emitted from the CSM Plugin Option.
	localLabels map[string]string
	// metadataExchangeLabelsEncoded are the metadata exchange labels to be sent
	// as the value of metadata key "x-envoy-peer-metadata" in proto wire format
	// and base 64 encoded. This gets sent out from all the servers running in
	// this process and for csm channels.
	metadataExchangeLabelsEncoded string
}

// newPluginOption returns a new pluginOption with local labels and metadata
// exchange labels derived from the environment.
func newPluginOption(ctx context.Context) internal.PluginOption {
	localLabels, metadataExchangeLabelsEncoded := constructMetadataFromEnv(ctx)

	return &pluginOption{
		localLabels:                   localLabels,
		metadataExchangeLabelsEncoded: metadataExchangeLabelsEncoded,
	}
}

// NewLabelsMD returns a metadata.MD with the CSM labels as an encoded protobuf
// Struct as the value of "x-envoy-peer-metadata".
func (cpo *pluginOption) GetMetadata() metadata.MD {
	return metadata.New(map[string]string{
		metadataExchangeKey: cpo.metadataExchangeLabelsEncoded,
	})
}

// GetLabels gets the CSM peer labels from the metadata provided. It returns
// "unknown" for labels not found. Labels returned depend on the remote type.
// Additionally, local labels determined at initialization time are appended to
// labels returned, in addition to the optionalLabels provided.
func (cpo *pluginOption) GetLabels(md metadata.MD) map[string]string {
	labels := map[string]string{ // Remote labels if type is unknown (i.e. unset or error processing x-envoy-peer-metadata)
		"csm.remote_workload_type":              "unknown",
		"csm.remote_workload_canonical_service": "unknown",
	}
	// Append the local labels.
	for k, v := range cpo.localLabels {
		labels[k] = v
	}

	val := md.Get("x-envoy-peer-metadata")
	// This can't happen if corresponding csm client because of proto wire
	// format encoding, but since it is arbitrary off the wire be safe.
	if len(val) != 1 {
		logger.Warningf("length of md values of \"x-envoy-peer-metadata\" is not 1, is %v", len(val))
		return labels
	}

	protoWireFormat, err := base64.RawStdEncoding.DecodeString(val[0])
	if err != nil {
		logger.Warningf("error base 64 decoding value of \"x-envoy-peer-metadata\": %v", err)
		return labels
	}

	spb := &structpb.Struct{}
	if err := proto.Unmarshal(protoWireFormat, spb); err != nil {
		logger.Warningf("error unmarshalling value of \"x-envoy-peer-metadata\" into proto: %v", err)
		return labels
	}

	fields := spb.GetFields()

	labels["csm.remote_workload_type"] = getFromMetadata("type", fields)
	// The value of “csm.remote_workload_canonical_service” comes from
	// MetadataExchange with the key “canonical_service”. (Note that this should
	// be read even if the remote type is unknown.)
	labels["csm.remote_workload_canonical_service"] = getFromMetadata("canonical_service", fields)

	// Unset/unknown types, and types that aren't GKE or GCP return early with
	// just local labels, remote_workload_type and
	// remote_workload_canonical_service labels.
	workloadType := labels["csm.remote_workload_type"]
	if workloadType != "gcp_kubernetes_engine" && workloadType != "gcp_compute_engine" {
		return labels
	}
	// GKE and GCE labels.
	labels["csm.remote_workload_project_id"] = getFromMetadata("project_id", fields)
	labels["csm.remote_workload_location"] = getFromMetadata("location", fields)
	labels["csm.remote_workload_name"] = getFromMetadata("workload_name", fields)
	if workloadType == "gcp_compute_engine" {
		return labels
	}

	// GKE only labels.
	labels["csm.remote_workload_cluster_name"] = getFromMetadata("cluster_name", fields)
	labels["csm.remote_workload_namespace_name"] = getFromMetadata("namespace_name", fields)
	return labels
}

// getFromMetadata gets the value for the metadata key from the protobuf
// metadata. Returns "unknown" if the metadata is not found in the protobuf
// metadata, or if the value is not a string value. Returns the string value
// otherwise.
func getFromMetadata(metadataKey string, metadata map[string]*structpb.Value) string {
	if metadata != nil {
		if metadataVal, ok := metadata[metadataKey]; ok {
			if _, ok := metadataVal.GetKind().(*structpb.Value_StringValue); ok {
				return metadataVal.GetStringValue()
			}
		}
	}
	return "unknown"
}

// getFromResource gets the value for the resource key from the attribute set.
// Returns "unknown" if the resourceKey is not found in the attribute set or is
// not a string value, the string value otherwise.
func getFromResource(resourceKey attribute.Key, set *attribute.Set) string {
	if set != nil {
		if resourceVal, ok := set.Value(resourceKey); ok && resourceVal.Type() == attribute.STRING {
			return resourceVal.AsString()
		}
	}
	return "unknown"
}

// getEnv returns "unknown" if environment variable is unset, the environment
// variable otherwise.
func getEnv(name string) string {
	if val, ok := os.LookupEnv(name); ok {
		return val
	}
	return "unknown"
}

var (
	// This function will be overridden in unit tests.
	getAttrSetFromResourceDetector = func(ctx context.Context) *attribute.Set {
		r, err := resource.New(ctx, resource.WithFromEnv(), resource.WithDetectors(gcp.NewDetector()))
		if err != nil {
			logger.Warningf("error reading OpenTelemetry resource: %v", err)
		}
		if r != nil {
			// It's possible for resource.New to return partial data alongside
			// an error. In this case, use partial data and set "unknown" for
			// labels missing.
			return r.Set()
		}
		return nil
	}
)

// constructMetadataFromEnv creates local labels and labels to send to the peer
// using metadata exchange based off resource detection and environment
// variables.
//
// Returns local labels, and base 64 encoded protobuf.Struct containing metadata
// exchange labels.
func constructMetadataFromEnv(ctx context.Context) (map[string]string, string) {
	set := getAttrSetFromResourceDetector(ctx)

	labels := make(map[string]string)
	labels["type"] = getFromResource("cloud.platform", set)
	labels["canonical_service"] = getEnv("CSM_CANONICAL_SERVICE_NAME")

	// If type is not GCE or GKE only metadata exchange labels are "type" and
	// "canonical_service".
	cloudPlatformVal := labels["type"]
	if cloudPlatformVal != "gcp_kubernetes_engine" && cloudPlatformVal != "gcp_compute_engine" {
		return initializeLocalAndMetadataLabels(labels)
	}

	// GCE and GKE labels:
	labels["workload_name"] = getEnv("CSM_WORKLOAD_NAME")

	locationVal := "unknown"
	if resourceVal, ok := set.Value("cloud.availability_zone"); ok && resourceVal.Type() == attribute.STRING {
		locationVal = resourceVal.AsString()
	} else if resourceVal, ok = set.Value("cloud.region"); ok && resourceVal.Type() == attribute.STRING {
		locationVal = resourceVal.AsString()
	}
	labels["location"] = locationVal

	labels["project_id"] = getFromResource("cloud.account.id", set)
	if cloudPlatformVal == "gcp_compute_engine" {
		return initializeLocalAndMetadataLabels(labels)
	}

	// GKE specific labels:
	labels["namespace_name"] = getFromResource("k8s.namespace.name", set)
	labels["cluster_name"] = getFromResource("k8s.cluster.name", set)
	return initializeLocalAndMetadataLabels(labels)
}

// initializeLocalAndMetadataLabels csm local labels for a CSM Plugin Option to
// record. It also builds out a base 64 encoded protobuf.Struct containing the
// metadata exchange labels to be sent as part of metadata exchange from a CSM
// Plugin Option.
func initializeLocalAndMetadataLabels(labels map[string]string) (map[string]string, string) {
	// The value of “csm.workload_canonical_service” comes from
	// “CSM_CANONICAL_SERVICE_NAME” env var, “unknown” if unset.
	val := labels["canonical_service"]
	localLabels := make(map[string]string)
	localLabels["csm.workload_canonical_service"] = val
	localLabels["csm.mesh_id"] = getEnv("CSM_MESH_ID")

	// Metadata exchange labels - can go ahead and encode into proto, and then
	// base64.
	pbLabels := &structpb.Struct{
		Fields: map[string]*structpb.Value{},
	}
	for k, v := range labels {
		pbLabels.Fields[k] = structpb.NewStringValue(v)
	}
	protoWireFormat, err := proto.Marshal(pbLabels)
	metadataExchangeLabelsEncoded := ""
	if err == nil {
		metadataExchangeLabelsEncoded = base64.RawStdEncoding.EncodeToString(protoWireFormat)
	}
	// else - This behavior triggers server side to reply (if sent from a gRPC
	// Client within this binary) with the metadata exchange labels. Even if
	// client side has a problem marshaling proto into wire format, it can
	// still use server labels so send an empty string as the value of
	// x-envoy-peer-metadata. The presence of this metadata exchange header
	// will cause server side to respond with metadata exchange labels.

	return localLabels, metadataExchangeLabelsEncoded
}

// metadataExchangeKey is the key for HTTP metadata exchange.
const metadataExchangeKey = "x-envoy-peer-metadata"

func determineTargetCSM(parsedTarget *url.URL) bool {
	// On the client-side, the channel target is used to determine if a channel is a
	// CSM channel or not. CSM channels need to have an “xds” scheme and a
	// "traffic-director-global.xds.googleapis.com" authority. In the cases where no
	// authority is mentioned, the authority is assumed to be CSM. MetadataExchange
	// is performed only for CSM channels. Non-metadata exchange labels are detected
	// as described below.
	return parsedTarget.Scheme == "xds" && (parsedTarget.Host == "" || parsedTarget.Host == "traffic-director-global.xds.googleapis.com")
}
