/*
 * Copyright 2022 gRPC authors.
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

package opencensus

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	keyClientMethod = tag.MustNewKey("grpc_client_method")
	keyClientStatus = tag.MustNewKey("grpc_client_status")
)

// Measures, which are recorded by client stats handler: Note that due to the
// nature of how stats handlers are called on gRPC's client side, the per rpc
// unit is actually per attempt throughout this definition file.
var (
	clientSentMessagesPerRPC            = stats.Int64("grpc.io/client/sent_messages_per_rpc", "Number of messages sent in the RPC (always 1 for non-streaming RPCs).", stats.UnitDimensionless)
	clientSentBytesPerRPC               = stats.Int64("grpc.io/client/sent_bytes_per_rpc", "Total bytes sent across all request messages per RPC.", stats.UnitBytes)
	clientSentCompressedBytesPerRPC     = stats.Int64("grpc.io/client/sent_compressed_message_bytes_per_rpc", "Total compressed bytes sent across all request messages per RPC.", stats.UnitBytes)
	clientReceivedMessagesPerRPC        = stats.Int64("grpc.io/client/received_messages_per_rpc", "Number of response messages received per RPC (always 1 for non-streaming RPCs).", stats.UnitDimensionless)
	clientReceivedBytesPerRPC           = stats.Int64("grpc.io/client/received_bytes_per_rpc", "Total bytes received across all response messages per RPC.", stats.UnitBytes)
	clientReceivedCompressedBytesPerRPC = stats.Int64("grpc.io/client/received_compressed_message_bytes_per_rpc", "Total compressed bytes received across all response messages per RPC.", stats.UnitBytes)
	clientRoundtripLatency              = stats.Float64("grpc.io/client/roundtrip_latency", "Time between first byte of request sent to last byte of response received, or terminal error.", stats.UnitMilliseconds)
	clientStartedRPCs                   = stats.Int64("grpc.io/client/started_rpcs", "The total number of client RPCs ever opened, including those that have not completed.", stats.UnitDimensionless)
	clientServerLatency                 = stats.Float64("grpc.io/client/server_latency", `Propagated from the server and should have the same value as "grpc.io/server/latency".`, stats.UnitMilliseconds)
	// Per call measure:
	clientAPILatency = stats.Float64("grpc.io/client/api_latency", "The end-to-end time the gRPC library takes to complete an RPC from the application’s perspective", stats.UnitMilliseconds)
)

var (
	// ClientSentMessagesPerRPCView is the distribution of sent messages per
	// RPC, keyed on method.
	ClientSentMessagesPerRPCView = &view.View{
		Measure:     clientSentMessagesPerRPC,
		Name:        "grpc.io/client/sent_messages_per_rpc",
		Description: "Distribution of sent messages per RPC, by method.",
		TagKeys:     []tag.Key{keyClientMethod},
		Aggregation: countDistribution,
	}
	// ClientReceivedMessagesPerRPCView is the distribution of received messages
	// per RPC, keyed on method.
	ClientReceivedMessagesPerRPCView = &view.View{
		Measure:     clientReceivedMessagesPerRPC,
		Name:        "grpc.io/client/received_messages_per_rpc",
		Description: "Distribution of received messages per RPC, by method.",
		TagKeys:     []tag.Key{keyClientMethod},
		Aggregation: countDistribution,
	}
	// ClientSentBytesPerRPCView is the distribution of sent bytes per RPC,
	// keyed on method.
	ClientSentBytesPerRPCView = &view.View{
		Measure:     clientSentBytesPerRPC,
		Name:        "grpc.io/client/sent_bytes_per_rpc",
		Description: "Distribution of sent bytes per RPC, by method.",
		TagKeys:     []tag.Key{keyClientMethod},
		Aggregation: bytesDistribution,
	}
	// ClientSentCompressedMessageBytesPerRPCView is the distribution of
	// compressed sent message bytes per RPC, keyed on method.
	ClientSentCompressedMessageBytesPerRPCView = &view.View{
		Measure:     clientSentCompressedBytesPerRPC,
		Name:        "grpc.io/client/sent_compressed_message_bytes_per_rpc",
		Description: "Distribution of sent compressed message bytes per RPC, by method.",
		TagKeys:     []tag.Key{keyClientMethod},
		Aggregation: bytesDistribution,
	}
	// ClientReceivedBytesPerRPCView is the distribution of received bytes per
	// RPC, keyed on method.
	ClientReceivedBytesPerRPCView = &view.View{
		Measure:     clientReceivedBytesPerRPC,
		Name:        "grpc.io/client/received_bytes_per_rpc",
		Description: "Distribution of received bytes per RPC, by method.",
		TagKeys:     []tag.Key{keyClientMethod},
		Aggregation: bytesDistribution,
	}
	// ClientReceivedCompressedMessageBytesPerRPCView is the distribution of
	// compressed received message bytes per RPC, keyed on method.
	ClientReceivedCompressedMessageBytesPerRPCView = &view.View{
		Measure:     clientReceivedCompressedBytesPerRPC,
		Name:        "grpc.io/client/received_compressed_message_bytes_per_rpc",
		Description: "Distribution of received compressed message bytes per RPC, by method.",
		TagKeys:     []tag.Key{keyClientMethod},
		Aggregation: bytesDistribution,
	}
	// ClientStartedRPCsView is the count of opened RPCs, keyed on method.
	ClientStartedRPCsView = &view.View{
		Measure:     clientStartedRPCs,
		Name:        "grpc.io/client/started_rpcs",
		Description: "Number of opened client RPCs, by method.",
		TagKeys:     []tag.Key{keyClientMethod},
		Aggregation: view.Count(),
	}
	// ClientCompletedRPCsView is the count of completed RPCs, keyed on method
	// and status.
	ClientCompletedRPCsView = &view.View{
		Measure:     clientRoundtripLatency,
		Name:        "grpc.io/client/completed_rpcs",
		Description: "Number of completed RPCs by method and status.",
		TagKeys:     []tag.Key{keyClientMethod, keyClientStatus},
		Aggregation: view.Count(),
	}
	// ClientRoundtripLatencyView is the distribution of round-trip latency in
	// milliseconds per RPC, keyed on method.
	ClientRoundtripLatencyView = &view.View{
		Measure:     clientRoundtripLatency,
		Name:        "grpc.io/client/roundtrip_latency",
		Description: "Distribution of round-trip latency, by method.",
		TagKeys:     []tag.Key{keyClientMethod},
		Aggregation: millisecondsDistribution,
	}

	// The following metric is per call:

	// ClientAPILatencyView is the distribution of client api latency for the
	// full RPC call, keyed on method and status.
	ClientAPILatencyView = &view.View{
		Measure:     clientAPILatency,
		Name:        "grpc.io/client/api_latency",
		Description: "Distribution of client api latency, by method and status",
		TagKeys:     []tag.Key{keyClientMethod, keyClientStatus},
		Aggregation: millisecondsDistribution,
	}
)

// DefaultClientViews is the set of client views which are considered the
// minimum required to monitor client side performance.
var DefaultClientViews = []*view.View{
	ClientSentBytesPerRPCView,
	ClientReceivedBytesPerRPCView,
	ClientRoundtripLatencyView,
	ClientCompletedRPCsView,
	ClientStartedRPCsView,
}
