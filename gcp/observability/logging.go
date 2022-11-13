/*
 *
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
 *
 */

package observability

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	gcplogging "cloud.google.com/go/logging"
	"github.com/google/uuid"

	"google.golang.org/grpc"
	binlogpb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/binarylog"
	iblog "google.golang.org/grpc/internal/binarylog"
	"google.golang.org/grpc/internal/grpcutil"
)

var lExporter loggingExporter

var newLoggingExporter = newCloudLoggingExporter

// translateMetadata translates the metadata from Binary Logging format to
// its GrpcLogEntry equivalent.
func translateMetadata(m *binlogpb.Metadata) map[string]string {
	metadata := make(map[string]string)
	for _, entry := range m.GetEntry() {
		entryKey := entry.GetKey()
		var newVal string
		if strings.HasSuffix(entryKey, "-bin") { // bin header
			newVal = base64.StdEncoding.EncodeToString(entry.GetValue())
		} else { // normal header
			newVal = string(entry.GetValue())
		}
		var oldVal string
		var ok bool
		if oldVal, ok = metadata[entryKey]; !ok {
			metadata[entryKey] = newVal
			continue
		}
		metadata[entryKey] = oldVal + "," + newVal
	}
	return metadata
}

func setPeerIfPresent(binlogEntry *binlogpb.GrpcLogEntry, grpcLogEntry *grpcLogEntry) {
	if binlogEntry.GetPeer() != nil {
		grpcLogEntry.Peer.Type = addrType(binlogEntry.GetPeer().GetType())
		grpcLogEntry.Peer.Address = binlogEntry.GetPeer().GetAddress()
		grpcLogEntry.Peer.IPPort = binlogEntry.GetPeer().GetIpPort()
	}
}

var loggerTypeToEventLogger = map[binlogpb.GrpcLogEntry_Logger]loggerType{
	binlogpb.GrpcLogEntry_LOGGER_UNKNOWN: loggerUnknown,
	binlogpb.GrpcLogEntry_LOGGER_CLIENT:  loggerClient,
	binlogpb.GrpcLogEntry_LOGGER_SERVER:  loggerServer,
}

type eventType int

const (
	// eventTypeUnknown is an unknown event type.
	eventTypeUnknown eventType = iota
	// eventTypeClientHeader is a header sent from client to server.
	eventTypeClientHeader
	// eventTypeServerHeader is a header sent from server to client.
	eventTypeServerHeader
	// eventTypeClientMessage is a message sent from client to server.
	eventTypeClientMessage
	// eventTypeServerMessage is a message sent from server to client.
	eventTypeServerMessage
	// eventTypeClientHalfClose is a signal that the loggerClient is done sending.
	eventTypeClientHalfClose
	// eventTypeServerTrailer indicated the end of a gRPC call.
	eventTypeServerTrailer
	// eventTypeCancel is a signal that the rpc is canceled.
	eventTypeCancel
)

func (t eventType) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString(`"`)
	switch t {
	case eventTypeUnknown:
		buffer.WriteString("EVENT_TYPE_UNKNOWN")
	case eventTypeClientHeader:
		buffer.WriteString("CLIENT_HEADER")
	case eventTypeServerHeader:
		buffer.WriteString("SERVER_HEADER")
	case eventTypeClientMessage:
		buffer.WriteString("CLIENT_MESSAGE")
	case eventTypeServerMessage:
		buffer.WriteString("SERVER_MESSAGE")
	case eventTypeClientHalfClose:
		buffer.WriteString("CLIENT_HALF_CLOSE")
	case eventTypeServerTrailer:
		buffer.WriteString("SERVER_TRAILER")
	case eventTypeCancel:
		buffer.WriteString("CANCEL")
	}
	buffer.WriteString(`"`)
	return buffer.Bytes(), nil
}

type loggerType int

const (
	loggerUnknown loggerType = iota
	loggerClient
	loggerServer
)

func (t loggerType) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString(`"`)
	switch t {
	case loggerUnknown:
		buffer.WriteString("LOGGER_UNKNOWN")
	case loggerClient:
		buffer.WriteString("CLIENT")
	case loggerServer:
		buffer.WriteString("SERVER")
	}
	buffer.WriteString(`"`)
	return buffer.Bytes(), nil
}

type payload struct {
	Metadata map[string]string `json:"metadata,omitempty"`
	// Timeout is the RPC timeout value.
	Timeout time.Duration `json:"timeout,omitempty"`
	// StatusCode is the gRPC status code.
	StatusCode uint32 `json:"statusCode,omitempty"`
	// StatusMessage is the gRPC status message.
	StatusMessage string `json:"statusMessage,omitempty"`
	// StatusDetails is the value of the grpc-status-details-bin metadata key,
	// if any. This is always an encoded google.rpc.Status message.
	StatusDetails []byte `json:"statusDetails,omitempty"`
	// MessageLength is the length of the message.
	MessageLength uint32 `json:"messageLength,omitempty"`
	// Message is the message of this entry. This is populated in the case of a
	// message event.
	Message []byte `json:"message,omitempty"`
}

type addrType int

const (
	typeUnknown addrType = iota // `json:"TYPE_UNKNOWN"`
	typeIPv4                    // `json:"TYPE_IPV4"`
	typeIPv6                    // `json:"TYPE_IPV6"`
	typeUnix                    // `json:"TYPE_UNIX"`
)

func (at addrType) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString(`"`)
	switch at {
	case typeUnknown:
		buffer.WriteString("TYPE_UNKNOWN")
	case typeIPv4:
		buffer.WriteString("TYPE_IPV4")
	case typeIPv6:
		buffer.WriteString("TYPE_IPV6")
	case typeUnix:
		buffer.WriteString("TYPE_UNIX")
	}
	buffer.WriteString(`"`)
	return buffer.Bytes(), nil
}

type address struct {
	// Type is the address type of the address of the peer of the RPC.
	Type addrType `json:"type,omitempty"`
	// Address is the address of the peer of the RPC.
	Address string `json:"address,omitempty"`
	// IPPort is the ip and port in string form. It is used only for addrType
	// typeIPv4 and typeIPv6.
	IPPort uint32 `json:"ipPort,omitempty"`
}

type grpcLogEntry struct {
	// CallID is a uuid which uniquely identifies a call. Each call may have
	// several log entries. They will all have the same CallID. Nothing is
	// guaranteed about their value other than they are unique across different
	// RPCs in the same gRPC process.
	CallID string `json:"callId,omitempty"`
	// SequenceID is the entry sequence ID for this call. The first message has
	// a value of 1, to disambiguate from an unset value. The purpose of this
	// field is to detect missing entries in environments where durability or
	// ordering is not guaranteed.
	SequenceID uint64 `json:"sequenceId,omitempty"`
	// Type is the type of binary logging event being logged.
	Type eventType `json:"type,omitempty"`
	// Logger is the entity that generates the log entry.
	Logger loggerType `json:"logger,omitempty"`
	// Payload is the payload of this log entry.
	Payload payload `json:"payload,omitempty"`
	// PayloadTruncated is whether the message or metadata field is either
	// truncated or emitted due to options specified in the configuration.
	PayloadTruncated bool `json:"payloadTruncated,omitempty"`
	// Peer is information about the Peer of the RPC.
	Peer address `json:"peer,omitempty"`
	// A single process may be used to run multiple virtual servers with
	// different identities.
	// Authority is the name of such a server identify. It is typically a
	// portion of the URI in the form of <host> or <host>:<port>.
	Authority string `json:"authority,omitempty"`
	// ServiceName is the name of the service.
	ServiceName string `json:"serviceName,omitempty"`
	// MethodName is the name of the RPC method.
	MethodName string `json:"methodName,omitempty"`
}

type methodLoggerBuilder interface {
	Build(iblog.LogEntryConfig) *binlogpb.GrpcLogEntry
}

type binaryMethodLogger struct {
	callID, serviceName, methodName, authority string

	mlb      methodLoggerBuilder
	exporter loggingExporter
}

func (bml *binaryMethodLogger) Log(c iblog.LogEntryConfig) {
	binLogEntry := bml.mlb.Build(c)

	grpcLogEntry := &grpcLogEntry{
		CallID:     bml.callID,
		SequenceID: binLogEntry.GetSequenceIdWithinCall(),
		Logger:     loggerTypeToEventLogger[binLogEntry.Logger],
	}

	switch binLogEntry.GetType() {
	case binlogpb.GrpcLogEntry_EVENT_TYPE_UNKNOWN:
		grpcLogEntry.Type = eventTypeUnknown
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_HEADER:
		grpcLogEntry.Type = eventTypeClientHeader
		if binLogEntry.GetClientHeader() != nil {
			methodName := binLogEntry.GetClientHeader().MethodName
			// Example method name: /grpc.testing.TestService/UnaryCall
			if strings.Contains(methodName, "/") {
				tokens := strings.Split(methodName, "/")
				if len(tokens) == 3 {
					// Record service name and method name for all events.
					bml.serviceName = tokens[1]
					bml.methodName = tokens[2]
				} else {
					logger.Infof("Malformed method name: %v", methodName)
				}
			}
			bml.authority = binLogEntry.GetClientHeader().GetAuthority()
			grpcLogEntry.Payload.Timeout = binLogEntry.GetClientHeader().GetTimeout().AsDuration()
			grpcLogEntry.Payload.Metadata = translateMetadata(binLogEntry.GetClientHeader().GetMetadata())
		}
		grpcLogEntry.PayloadTruncated = binLogEntry.GetPayloadTruncated()
		setPeerIfPresent(binLogEntry, grpcLogEntry)
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_HEADER:
		grpcLogEntry.Type = eventTypeServerHeader
		if binLogEntry.GetServerHeader() != nil {
			grpcLogEntry.Payload.Metadata = translateMetadata(binLogEntry.GetServerHeader().GetMetadata())
		}
		grpcLogEntry.PayloadTruncated = binLogEntry.GetPayloadTruncated()
		setPeerIfPresent(binLogEntry, grpcLogEntry)
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_MESSAGE:
		grpcLogEntry.Type = eventTypeClientMessage
		grpcLogEntry.Payload.Message = binLogEntry.GetMessage().GetData()
		grpcLogEntry.Payload.MessageLength = binLogEntry.GetMessage().GetLength()
		grpcLogEntry.PayloadTruncated = binLogEntry.GetPayloadTruncated()
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_MESSAGE:
		grpcLogEntry.Type = eventTypeServerMessage
		grpcLogEntry.Payload.Message = binLogEntry.GetMessage().GetData()
		grpcLogEntry.Payload.MessageLength = binLogEntry.GetMessage().GetLength()
		grpcLogEntry.PayloadTruncated = binLogEntry.GetPayloadTruncated()
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_HALF_CLOSE:
		grpcLogEntry.Type = eventTypeClientHalfClose
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_TRAILER:
		grpcLogEntry.Type = eventTypeServerTrailer
		grpcLogEntry.Payload.Metadata = translateMetadata(binLogEntry.GetTrailer().Metadata)
		grpcLogEntry.Payload.StatusCode = binLogEntry.GetTrailer().GetStatusCode()
		grpcLogEntry.Payload.StatusMessage = binLogEntry.GetTrailer().GetStatusMessage()
		grpcLogEntry.Payload.StatusDetails = binLogEntry.GetTrailer().GetStatusDetails()
		grpcLogEntry.PayloadTruncated = binLogEntry.GetPayloadTruncated()
		setPeerIfPresent(binLogEntry, grpcLogEntry)
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CANCEL:
		grpcLogEntry.Type = eventTypeCancel
	default:
		logger.Infof("Unknown event type: %v", binLogEntry.Type)
		return
	}
	grpcLogEntry.ServiceName = bml.serviceName
	grpcLogEntry.MethodName = bml.methodName
	grpcLogEntry.Authority = bml.authority

	gcploggingEntry := gcplogging.Entry{
		Timestamp: binLogEntry.GetTimestamp().AsTime(),
		Severity:  100,
		Payload:   grpcLogEntry,
	}

	bml.exporter.EmitGcpLoggingEntry(gcploggingEntry)
}

type eventConfig struct {
	ServiceMethod map[string]bool
	Services      map[string]bool
	MatchAll      bool

	// If true, won't log anything.
	Exclude      bool
	HeaderBytes  uint64
	MessageBytes uint64
}

type binaryLogger struct {
	EventConfigs []eventConfig
	exporter     loggingExporter
}

func (bl *binaryLogger) GetMethodLogger(methodName string) iblog.MethodLogger {
	// Prevent logging from logging, traces, and metrics API calls.
	if strings.HasPrefix(methodName, "/google.logging.v2.LoggingServiceV2/") || strings.HasPrefix(methodName, "/google.monitoring.v3.MetricService/") ||
		strings.HasPrefix(methodName, "/google.devtools.cloudtrace.v2.TraceService/") {
		return nil
	}
	s, _, err := grpcutil.ParseMethod(methodName)
	if err != nil {
		logger.Infof("binarylogging: failed to parse %q: %v", methodName, err)
		return nil
	}
	for _, eventConfig := range bl.EventConfigs {
		if eventConfig.MatchAll || eventConfig.ServiceMethod[methodName] || eventConfig.Services[s] {
			if eventConfig.Exclude {
				return nil
			}

			return &binaryMethodLogger{
				exporter: bl.exporter,
				mlb:      iblog.NewTruncatingMethodLogger(eventConfig.HeaderBytes, eventConfig.MessageBytes),
				callID:   uuid.NewString(),
			}
		}
	}
	return nil
}

func registerClientRPCEvents(clientRPCEvents []clientRPCEvents, exporter loggingExporter) {
	if len(clientRPCEvents) == 0 {
		return
	}
	var eventConfigs []eventConfig
	for _, clientRPCEvent := range clientRPCEvents {
		eventConfig := eventConfig{
			Exclude:      clientRPCEvent.Exclude,
			HeaderBytes:  uint64(clientRPCEvent.MaxMetadataBytes),
			MessageBytes: uint64(clientRPCEvent.MaxMessageBytes),
		}
		for _, method := range clientRPCEvent.Methods {
			eventConfig.ServiceMethod = make(map[string]bool)
			eventConfig.Services = make(map[string]bool)
			if method == "*" {
				eventConfig.MatchAll = true
				continue
			}
			s, m, err := grpcutil.ParseMethod(method)
			if err != nil {
				continue
			}
			if m == "*" {
				eventConfig.Services[s] = true
				continue
			}
			eventConfig.ServiceMethod[method] = true
		}
		eventConfigs = append(eventConfigs, eventConfig)
	}
	clientSideLogger := &binaryLogger{
		EventConfigs: eventConfigs,
		exporter:     exporter,
	}
	internal.AddGlobalDialOptions.(func(opt ...grpc.DialOption))(internal.WithBinaryLogger.(func(bl binarylog.Logger) grpc.DialOption)(clientSideLogger))
}

func registerServerRPCEvents(serverRPCEvents []serverRPCEvents, exporter loggingExporter) {
	if len(serverRPCEvents) == 0 {
		return
	}
	var eventConfigs []eventConfig
	for _, serverRPCEvent := range serverRPCEvents {
		eventConfig := eventConfig{
			Exclude:      serverRPCEvent.Exclude,
			HeaderBytes:  uint64(serverRPCEvent.MaxMetadataBytes),
			MessageBytes: uint64(serverRPCEvent.MaxMessageBytes),
		}
		for _, method := range serverRPCEvent.Methods {
			eventConfig.ServiceMethod = make(map[string]bool)
			eventConfig.Services = make(map[string]bool)
			if method == "*" {
				eventConfig.MatchAll = true
				continue
			}
			s, m, err := grpcutil.ParseMethod(method)
			if err != nil { // Shouldn't happen, already validated at this point.
				continue
			}
			if m == "*" {
				eventConfig.Services[s] = true
				continue
			}
			eventConfig.ServiceMethod[method] = true
		}
		eventConfigs = append(eventConfigs, eventConfig)
	}
	serverSideLogger := &binaryLogger{
		EventConfigs: eventConfigs,
		exporter:     exporter,
	}
	internal.AddGlobalServerOptions.(func(opt ...grpc.ServerOption))(internal.BinaryLogger.(func(bl binarylog.Logger) grpc.ServerOption)(serverSideLogger))
}

func startLogging(ctx context.Context, config *config) error {
	if config == nil || config.CloudLogging == nil {
		return nil
	}
	var err error
	lExporter, err = newLoggingExporter(ctx, config)
	if err != nil {
		return fmt.Errorf("unable to create CloudLogging exporter: %v", err)
	}

	cl := config.CloudLogging
	registerClientRPCEvents(cl.ClientRPCEvents, lExporter)
	registerServerRPCEvents(cl.ServerRPCEvents, lExporter)
	return nil
}

func stopLogging() {
	internal.ClearGlobalDialOptions()
	internal.ClearGlobalServerOptions()
	if lExporter != nil {
		// This Close() call handles the flushing of the logging buffer.
		lExporter.Close()
	}
}
