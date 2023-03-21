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
	keyServerMethod = tag.MustNewKey("grpc_server_method")
	keyServerStatus = tag.MustNewKey("grpc_server_status")
)

// Measures, which are recorded by server stats handler: Note that on gRPC's
// server side, the per rpc unit is truly per rpc, as there is no concept of a
// rpc attempt server side.
var (
	serverReceivedMessagesPerRPC        = stats.Int64("grpc.io/server/received_messages_per_rpc", "Number of messages received in each RPC. Has value 1 for non-streaming RPCs.", stats.UnitDimensionless) // the collection/measurement point of this measure handles the /rpc aspect of it
	serverReceivedBytesPerRPC           = stats.Int64("grpc.io/server/received_bytes_per_rpc", "Total bytes received across all messages per RPC.", stats.UnitBytes)
	serverReceivedCompressedBytesPerRPC = stats.Int64("grpc.io/server/received_compressed_bytes_per_rpc", "Total compressed bytes received across all messages per RPC.", stats.UnitBytes)
	serverSentMessagesPerRPC            = stats.Int64("grpc.io/server/sent_messages_per_rpc", "Number of messages sent in each RPC. Has value 1 for non-streaming RPCs.", stats.UnitDimensionless)
	serverSentBytesPerRPC               = stats.Int64("grpc.io/server/sent_bytes_per_rpc", "Total bytes sent in across all response messages per RPC.", stats.UnitBytes)
	serverSentCompressedBytesPerRPC     = stats.Int64("grpc.io/server/sent_compressed_bytes_per_rpc", "Total compressed bytes sent in across all response messages per RPC.", stats.UnitBytes)
	serverStartedRPCs                   = stats.Int64("grpc.io/server/started_rpcs", "The total number of server RPCs ever opened, including those that have not completed.", stats.UnitDimensionless)
	serverLatency                       = stats.Float64("grpc.io/server/server_latency", "Time between first byte of request received to last byte of response sent, or terminal error.", stats.UnitMilliseconds)
)

var (
	// ServerSentMessagesPerRPCView is the distribution of sent messages per
	// RPC, keyed on method.
	ServerSentMessagesPerRPCView = &view.View{
		Name:        "grpc.io/server/sent_messages_per_rpc",
		Description: "Distribution of sent messages per RPC, by method.",
		TagKeys:     []tag.Key{keyServerMethod},
		Measure:     serverSentMessagesPerRPC,
		Aggregation: countDistribution,
	}
	// ServerReceivedMessagesPerRPCView is the distribution of received messages
	// per RPC, keyed on method.
	ServerReceivedMessagesPerRPCView = &view.View{
		Name:        "grpc.io/server/received_messages_per_rpc",
		Description: "Distribution of received messages per RPC, by method.",
		TagKeys:     []tag.Key{keyServerMethod},
		Measure:     serverReceivedMessagesPerRPC,
		Aggregation: countDistribution,
	}
	// ServerSentBytesPerRPCView is the distribution of received bytes per RPC,
	// keyed on method.
	ServerSentBytesPerRPCView = &view.View{
		Name:        "grpc.io/server/sent_bytes_per_rpc",
		Description: "Distribution of sent bytes per RPC, by method.",
		Measure:     serverSentBytesPerRPC,
		TagKeys:     []tag.Key{keyServerMethod},
		Aggregation: bytesDistribution,
	}
	// ServerSentCompressedMessageBytesPerRPCView is the distribution of
	// received compressed message bytes per RPC, keyed on method.
	ServerSentCompressedMessageBytesPerRPCView = &view.View{
		Name:        "grpc.io/server/sent_compressed_message_bytes_per_rpc",
		Description: "Distribution of sent compressed message bytes per RPC, by method.",
		Measure:     serverSentCompressedBytesPerRPC,
		TagKeys:     []tag.Key{keyServerMethod},
		Aggregation: bytesDistribution,
	}
	// ServerReceivedBytesPerRPCView is the distribution of sent bytes per RPC,
	// keyed on method.
	ServerReceivedBytesPerRPCView = &view.View{
		Name:        "grpc.io/server/received_bytes_per_rpc",
		Description: "Distribution of received bytes per RPC, by method.",
		Measure:     serverReceivedBytesPerRPC,
		TagKeys:     []tag.Key{keyServerMethod},
		Aggregation: bytesDistribution,
	}
	// ServerReceivedCompressedMessageBytesPerRPCView is the distribution of
	// sent compressed message bytes per RPC, keyed on method.
	ServerReceivedCompressedMessageBytesPerRPCView = &view.View{
		Name:        "grpc.io/server/received_compressed_message_bytes_per_rpc",
		Description: "Distribution of received compressed message bytes per RPC, by method.",
		Measure:     serverReceivedCompressedBytesPerRPC,
		TagKeys:     []tag.Key{keyServerMethod},
		Aggregation: bytesDistribution,
	}
	// ServerStartedRPCsView is the count of opened RPCs, keyed on method.
	ServerStartedRPCsView = &view.View{
		Measure:     serverStartedRPCs,
		Name:        "grpc.io/server/started_rpcs",
		Description: "Number of opened server RPCs, by method.",
		TagKeys:     []tag.Key{keyServerMethod},
		Aggregation: view.Count(),
	}
	// ServerCompletedRPCsView is the count of completed RPCs, keyed on
	// method and status.
	ServerCompletedRPCsView = &view.View{
		Name:        "grpc.io/server/completed_rpcs",
		Description: "Number of completed RPCs by method and status.",
		TagKeys:     []tag.Key{keyServerMethod, keyServerStatus},
		Measure:     serverLatency,
		Aggregation: view.Count(),
	}
	// ServerLatencyView is the distribution of server latency in milliseconds
	// per RPC, keyed on method.
	ServerLatencyView = &view.View{
		Name:        "grpc.io/server/server_latency",
		Description: "Distribution of server latency in milliseconds, by method.",
		TagKeys:     []tag.Key{keyServerMethod},
		Measure:     serverLatency,
		Aggregation: millisecondsDistribution,
	}
)

// DefaultServerViews is the set of server views which are considered the
// minimum required to monitor server side performance.
var DefaultServerViews = []*view.View{
	ServerReceivedBytesPerRPCView,
	ServerSentBytesPerRPCView,
	ServerLatencyView,
	ServerCompletedRPCsView,
	ServerStartedRPCsView,
}
