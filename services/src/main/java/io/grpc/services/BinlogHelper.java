/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.services;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.services.BinaryLogProvider.BYTEARRAY_MARSHALLER;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Charsets;
import com.google.common.base.Preconditions;
import com.google.common.base.Splitter;
import com.google.protobuf.ByteString;
import com.google.protobuf.Duration;
import com.google.protobuf.util.Durations;
import com.google.protobuf.util.Timestamps;
import com.google.re2j.Matcher;
import com.google.re2j.Pattern;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.Context;
import io.grpc.Deadline;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import io.grpc.ForwardingClientCallListener.SimpleForwardingClientCallListener;
import io.grpc.ForwardingServerCall.SimpleForwardingServerCall;
import io.grpc.ForwardingServerCallListener.SimpleForwardingServerCallListener;
import io.grpc.Grpc;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.ServerCall;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.Status;
import io.grpc.binarylog.v1.Address;
import io.grpc.binarylog.v1.Address.Type;
import io.grpc.binarylog.v1.GrpcLogEntry;
import io.grpc.binarylog.v1.GrpcLogEntry.EventType;
import io.grpc.binarylog.v1.Message;
import io.grpc.binarylog.v1.Message.Builder;
import java.net.Inet4Address;
import java.net.Inet6Address;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A binary log class that is configured for a specific {@link MethodDescriptor}.
 */
@ThreadSafe
final class BinlogHelper {
  private static final Logger logger = Logger.getLogger(BinlogHelper.class.getName());
  // Normally 'grpc-' metadata keys are set from within gRPC, and applications are not allowed
  // to set them. This key is a special well known key that set from the application layer, but
  // represents a com.google.rpc.Status and is given special first class treatment.
  // See StatusProto.java
  static final Metadata.Key<byte[]> STATUS_DETAILS_KEY =
      Metadata.Key.of(
          "grpc-status-details-bin",
          Metadata.BINARY_BYTE_MARSHALLER);

  @VisibleForTesting
  final SinkWriter writer;

  @VisibleForTesting
  BinlogHelper(SinkWriter writer) {
    this.writer = writer;
  }

  // TODO(zpencer): move proto related static helpers into this class
  static final class SinkWriterImpl extends SinkWriter {
    private final BinaryLogSink sink;
    private TimeProvider timeProvider;
    private final int maxHeaderBytes;
    private final int maxMessageBytes;

    private static final long NANOS_PER_SECOND = TimeUnit.SECONDS.toNanos(1);

    SinkWriterImpl(
        BinaryLogSink sink,
        TimeProvider timeProvider,
        int maxHeaderBytes,
        int maxMessageBytes) {
      this.sink = sink;
      this.timeProvider = timeProvider;
      this.maxHeaderBytes = maxHeaderBytes;
      this.maxMessageBytes = maxMessageBytes;
    }

    GrpcLogEntry.Builder newTimestampedBuilder() {
      long epochNanos = timeProvider.currentTimeNanos();
      return GrpcLogEntry.newBuilder().setTimestamp(Timestamps.fromNanos(epochNanos));
    }

    @Override
    void logClientHeader(
        long seq,
        String methodName,
        // not all transports have the concept of authority
        @Nullable String authority,
        @Nullable Duration timeout,
        Metadata metadata,
        GrpcLogEntry.Logger logger,
        long callId,
        // null on client side
        @Nullable SocketAddress peerAddress) {
      Preconditions.checkArgument(methodName != null, "methodName can not be null");
      Preconditions.checkArgument(
          !methodName.startsWith("/"),
          "in grpc-java method names should not have a leading '/'. However this class will "
              + "add one to be consistent with language agnostic conventions.");
      Preconditions.checkArgument(
          peerAddress == null || logger == GrpcLogEntry.Logger.LOGGER_SERVER,
          "peerSocket can only be specified for server");

      MaybeTruncated<io.grpc.binarylog.v1.Metadata.Builder> pair
          = createMetadataProto(metadata, maxHeaderBytes);
      io.grpc.binarylog.v1.ClientHeader.Builder clientHeaderBuilder
          = io.grpc.binarylog.v1.ClientHeader.newBuilder()
          .setMetadata(pair.proto)
          .setMethodName("/" + methodName);
      if (timeout != null) {
        clientHeaderBuilder.setTimeout(timeout);
      }
      if (authority != null) {
        clientHeaderBuilder.setAuthority(authority);
      }

      GrpcLogEntry.Builder entryBuilder = newTimestampedBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(EventType.EVENT_TYPE_CLIENT_HEADER)
          .setClientHeader(clientHeaderBuilder)
          .setPayloadTruncated(pair.truncated)
          .setLogger(logger)
          .setCallId(callId);
      if (peerAddress != null) {
        entryBuilder.setPeer(socketToProto(peerAddress));
      }
      sink.write(entryBuilder.build());
    }

    @Override
    void logServerHeader(
        long seq,
        Metadata metadata,
        GrpcLogEntry.Logger logger,
        long callId,
        // null on server
        @Nullable SocketAddress peerAddress) {
      Preconditions.checkArgument(
          peerAddress == null || logger == GrpcLogEntry.Logger.LOGGER_CLIENT,
          "peerSocket can only be specified for client");
      MaybeTruncated<io.grpc.binarylog.v1.Metadata.Builder> pair
          = createMetadataProto(metadata, maxHeaderBytes);

      GrpcLogEntry.Builder entryBuilder = newTimestampedBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(EventType.EVENT_TYPE_SERVER_HEADER)
          .setServerHeader(
              io.grpc.binarylog.v1.ServerHeader.newBuilder()
                  .setMetadata(pair.proto))
          .setPayloadTruncated(pair.truncated)
          .setLogger(logger)
          .setCallId(callId);
      if (peerAddress != null) {
        entryBuilder.setPeer(socketToProto(peerAddress));
      }
      sink.write(entryBuilder.build());
    }

    @Override
    void logTrailer(
        long seq,
        Status status,
        Metadata metadata,
        GrpcLogEntry.Logger logger,
        long callId,
        // null on server, can be non null on client if this is a trailer-only response
        @Nullable SocketAddress peerAddress) {
      Preconditions.checkArgument(
          peerAddress == null || logger == GrpcLogEntry.Logger.LOGGER_CLIENT,
          "peerSocket can only be specified for client");
      MaybeTruncated<io.grpc.binarylog.v1.Metadata.Builder> pair
          = createMetadataProto(metadata, maxHeaderBytes);

      io.grpc.binarylog.v1.Trailer.Builder trailerBuilder
          = io.grpc.binarylog.v1.Trailer.newBuilder()
          .setStatusCode(status.getCode().value())
          .setMetadata(pair.proto);
      String statusDescription = status.getDescription();
      if (statusDescription != null) {
        trailerBuilder.setStatusMessage(statusDescription);
      }
      byte[] statusDetailBytes = metadata.get(STATUS_DETAILS_KEY);
      if (statusDetailBytes != null) {
        trailerBuilder.setStatusDetails(ByteString.copyFrom(statusDetailBytes));
      }

      GrpcLogEntry.Builder entryBuilder = newTimestampedBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(EventType.EVENT_TYPE_SERVER_TRAILER)
          .setTrailer(trailerBuilder)
          .setPayloadTruncated(pair.truncated)
          .setLogger(logger)
          .setCallId(callId);
      if (peerAddress != null) {
        entryBuilder.setPeer(socketToProto(peerAddress));
      }
      sink.write(entryBuilder.build());
    }

    @Override
    <T> void logRpcMessage(
        long seq,
        EventType eventType,
        Marshaller<T> marshaller,
        T message,
        GrpcLogEntry.Logger logger,
        long callId) {
      Preconditions.checkArgument(
          eventType == EventType.EVENT_TYPE_CLIENT_MESSAGE
              || eventType == EventType.EVENT_TYPE_SERVER_MESSAGE,
          "event type must correspond to client message or server message");
      if (marshaller != BYTEARRAY_MARSHALLER) {
        throw new IllegalStateException("Expected the BinaryLog's ByteArrayMarshaller");
      }
      MaybeTruncated<Builder> pair = createMessageProto((byte[]) message, maxMessageBytes);
      GrpcLogEntry.Builder entryBuilder = newTimestampedBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(eventType)
          .setMessage(pair.proto)
          .setPayloadTruncated(pair.truncated)
          .setLogger(logger)
          .setCallId(callId);
      sink.write(entryBuilder.build());
    }

    @Override
    void logHalfClose(long seq, GrpcLogEntry.Logger logger, long callId) {
      sink.write(
          newTimestampedBuilder()
              .setSequenceIdWithinCall(seq)
              .setType(EventType.EVENT_TYPE_CLIENT_HALF_CLOSE)
              .setLogger(logger)
              .setCallId(callId)
              .build());
    }

    @Override
    void logCancel(long seq, GrpcLogEntry.Logger logger, long callId) {
      sink.write(
          newTimestampedBuilder()
              .setSequenceIdWithinCall(seq)
              .setType(EventType.EVENT_TYPE_CANCEL)
              .setLogger(logger)
              .setCallId(callId)
              .build());
    }

    @Override
    int getMaxHeaderBytes() {
      return maxHeaderBytes;
    }

    @Override
    int getMaxMessageBytes() {
      return maxMessageBytes;
    }
  }

  abstract static class SinkWriter {
    /**
     * Logs the client header. This method logs the appropriate number of bytes
     * as determined by the binary logging configuration.
     */
    abstract void logClientHeader(
        long seq,
        String methodName,
        // not all transports have the concept of authority
        @Nullable String authority,
        @Nullable Duration timeout,
        Metadata metadata,
        GrpcLogEntry.Logger logger,
        long callId,
        // null on client side
        @Nullable SocketAddress peerAddress);

    /**
     * Logs the server header. This method logs the appropriate number of bytes
     * as determined by the binary logging configuration.
     */
    abstract void logServerHeader(
        long seq,
        Metadata metadata,
        GrpcLogEntry.Logger logger,
        long callId,
        // null on server
        @Nullable SocketAddress peerAddress);

    /**
     * Logs the server trailer. This method logs the appropriate number of bytes
     * as determined by the binary logging configuration.
     */
    abstract void logTrailer(
        long seq,
        Status status,
        Metadata metadata,
        GrpcLogEntry.Logger logger,
        long callId,
        // null on server, can be non null on client if this is a trailer-only response
        @Nullable SocketAddress peerAddress);

    /**
     * Logs the message message. The number of bytes logged is determined by the binary
     * logging configuration.
     */
    abstract <T> void logRpcMessage(
        long seq,
        EventType eventType,
        Marshaller<T> marshaller,
        T message,
        GrpcLogEntry.Logger logger,
        long callId);

    abstract void logHalfClose(long seq, GrpcLogEntry.Logger logger, long callId);

    /**
     * Logs the cancellation.
     */
    abstract void logCancel(long seq, GrpcLogEntry.Logger logger, long callId);

    /**
     * Returns the number bytes of the header this writer will log, according to configuration.
     */
    abstract int getMaxHeaderBytes();

    /**
     * Returns the number bytes of the message this writer will log, according to configuration.
     */
    abstract int getMaxMessageBytes();
  }

  static SocketAddress getPeerSocket(Attributes streamAttributes) {
    return streamAttributes.get(Grpc.TRANSPORT_ATTR_REMOTE_ADDR);
  }

  private static Deadline min(@Nullable Deadline deadline0, @Nullable Deadline deadline1) {
    if (deadline0 == null) {
      return deadline1;
    }
    if (deadline1 == null) {
      return deadline0;
    }
    return deadline0.minimum(deadline1);
  }

  interface TimeProvider {
    /** Returns the current nano time. */
    long currentTimeNanos();

    TimeProvider SYSTEM_TIME_PROVIDER = new TimeProvider() {
      @Override
      public long currentTimeNanos() {
        return TimeUnit.MILLISECONDS.toNanos(System.currentTimeMillis());
      }
    };
  }


  public ClientInterceptor getClientInterceptor(final long callId) {
    return new ClientInterceptor() {
      boolean trailersOnlyResponse = true;
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          final MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
        final AtomicLong seq = new AtomicLong(1);
        final String methodName = method.getFullMethodName();
        final String authority = next.authority();
        // The timeout should reflect the time remaining when the call is started, so do not
        // compute remaining time here.
        final Deadline deadline = min(callOptions.getDeadline(), Context.current().getDeadline());

        return new SimpleForwardingClientCall<ReqT, RespT>(next.newCall(method, callOptions)) {
          @Override
          public void start(final Listener<RespT> responseListener, Metadata headers) {
            final Duration timeout = deadline == null ? null
                : Durations.fromNanos(deadline.timeRemaining(TimeUnit.NANOSECONDS));
            writer.logClientHeader(
                seq.getAndIncrement(),
                methodName,
                authority,
                timeout,
                headers,
                GrpcLogEntry.Logger.LOGGER_CLIENT,
                callId,
                /*peerAddress=*/ null);
            ClientCall.Listener<RespT> wListener =
                new SimpleForwardingClientCallListener<RespT>(responseListener) {
                  @Override
                  public void onMessage(RespT message) {
                    writer.logRpcMessage(
                        seq.getAndIncrement(),
                        EventType.EVENT_TYPE_SERVER_MESSAGE,
                        method.getResponseMarshaller(),
                        message,
                        GrpcLogEntry.Logger.LOGGER_CLIENT,
                        callId);
                    super.onMessage(message);
                  }

                  @Override
                  public void onHeaders(Metadata headers) {
                    trailersOnlyResponse = false;
                    writer.logServerHeader(
                        seq.getAndIncrement(),
                        headers,
                        GrpcLogEntry.Logger.LOGGER_CLIENT,
                        callId,
                        getPeerSocket(getAttributes()));
                    super.onHeaders(headers);
                  }

                  @Override
                  public void onClose(Status status, Metadata trailers) {
                    SocketAddress peer = trailersOnlyResponse
                        ? getPeerSocket(getAttributes()) : null;
                    writer.logTrailer(
                        seq.getAndIncrement(),
                        status,
                        trailers,
                        GrpcLogEntry.Logger.LOGGER_CLIENT,
                        callId,
                        peer);
                    super.onClose(status, trailers);
                  }
                };
            super.start(wListener, headers);
          }

          @Override
          public void sendMessage(ReqT message) {
            writer.logRpcMessage(
                seq.getAndIncrement(),
                EventType.EVENT_TYPE_CLIENT_MESSAGE,
                method.getRequestMarshaller(),
                message,
                GrpcLogEntry.Logger.LOGGER_CLIENT,
                callId);
            super.sendMessage(message);
          }

          @Override
          public void halfClose() {
            writer.logHalfClose(
                seq.getAndIncrement(),
                GrpcLogEntry.Logger.LOGGER_CLIENT,
                callId);
          }

          @Override
          public void cancel(String message, Throwable cause) {
            writer.logCancel(
                seq.getAndIncrement(),
                GrpcLogEntry.Logger.LOGGER_CLIENT,
                callId);
            super.cancel(message, cause);
          }
        };
      }
    };
  }

  public ServerInterceptor getServerInterceptor(final long callId) {
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> Listener<ReqT> interceptCall(
          final ServerCall<ReqT, RespT> call,
          Metadata headers,
          ServerCallHandler<ReqT, RespT> next) {
        final AtomicLong seq = new AtomicLong(1);
        SocketAddress peer = getPeerSocket(call.getAttributes());
        String methodName = call.getMethodDescriptor().getFullMethodName();
        String authority = call.getAuthority();
        Deadline deadline = Context.current().getDeadline();
        final Duration timeout = deadline == null ? null
            : Durations.fromNanos(deadline.timeRemaining(TimeUnit.NANOSECONDS));

        writer.logClientHeader(
            seq.getAndIncrement(),
            methodName,
            authority,
            timeout,
            headers,
            GrpcLogEntry.Logger.LOGGER_SERVER,
            callId,
            peer);
        ServerCall<ReqT, RespT> wCall = new SimpleForwardingServerCall<ReqT, RespT>(call) {
          @Override
          public void sendMessage(RespT message) {
            writer.logRpcMessage(
                seq.getAndIncrement(),
                EventType.EVENT_TYPE_SERVER_MESSAGE,
                call.getMethodDescriptor().getResponseMarshaller(),
                message,
                GrpcLogEntry.Logger.LOGGER_SERVER,
                callId);
            super.sendMessage(message);
          }

          @Override
          public void sendHeaders(Metadata headers) {
            writer.logServerHeader(
                seq.getAndIncrement(),
                headers,
                GrpcLogEntry.Logger.LOGGER_SERVER,
                callId,
                /*peerAddress=*/ null);
            super.sendHeaders(headers);
          }

          @Override
          public void close(Status status, Metadata trailers) {
            writer.logTrailer(
                seq.getAndIncrement(),
                status,
                trailers,
                GrpcLogEntry.Logger.LOGGER_SERVER,
                callId,
                /*peerAddress=*/ null);
            super.close(status, trailers);
          }
        };

        return new SimpleForwardingServerCallListener<ReqT>(next.startCall(wCall, headers)) {
          @Override
          public void onMessage(ReqT message) {
            writer.logRpcMessage(
                seq.getAndIncrement(),
                EventType.EVENT_TYPE_CLIENT_MESSAGE,
                call.getMethodDescriptor().getRequestMarshaller(),
                message,
                GrpcLogEntry.Logger.LOGGER_SERVER,
                callId);
            super.onMessage(message);
          }

          @Override
          public void onHalfClose() {
            writer.logHalfClose(
                seq.getAndIncrement(),
                GrpcLogEntry.Logger.LOGGER_SERVER,
                callId);
            super.onHalfClose();
          }

          @Override
          public void onCancel() {
            writer.logCancel(
                seq.getAndIncrement(),
                GrpcLogEntry.Logger.LOGGER_SERVER,
                callId);
          }
        };
      }
    };
  }

  interface Factory {
    @Nullable
    BinlogHelper getLog(String fullMethodName);
  }

  static final class FactoryImpl implements Factory {
    // '*' for global, 'service/*' for service glob, or 'service/method' for fully qualified.
    private static final Pattern logPatternRe = Pattern.compile("[^{]+");
    // A curly brace wrapped expression. Will be further matched with the more specified REs below.
    private static final Pattern logOptionsRe = Pattern.compile("\\{[^}]+}");
    private static final Pattern configRe = Pattern.compile(
        String.format("^(%s)(%s)?$", logPatternRe.pattern(), logOptionsRe.pattern()));
    // Regexes to extract per-binlog options
    // The form: {m:256}
    private static final Pattern msgRe = Pattern.compile("\\{m(?::(\\d+))?}");
    // The form: {h:256}
    private static final Pattern headerRe = Pattern.compile("\\{h(?::(\\d+))?}");
    // The form: {h:256,m:256}
    private static final Pattern bothRe = Pattern.compile("\\{h(?::(\\d+))?;m(?::(\\d+))?}");

    private final BinlogHelper globalLog;
    private final Map<String, BinlogHelper> perServiceLogs;
    private final Map<String, BinlogHelper> perMethodLogs;
    private final Set<String> blacklistedMethods;

    /**
     * Accepts a string in the format specified by the binary log spec.
     */
    @VisibleForTesting
    FactoryImpl(BinaryLogSink sink, String configurationString) {
      checkNotNull(sink, "sink");
      BinlogHelper globalLog = null;
      Map<String, BinlogHelper> perServiceLogs = new HashMap<String, BinlogHelper>();
      Map<String, BinlogHelper> perMethodLogs = new HashMap<String, BinlogHelper>();
      Set<String> blacklistedMethods = new HashSet<String>();
      if (configurationString != null && configurationString.length() > 0) {
        for (String configuration : Splitter.on(',').split(configurationString)) {
          Matcher configMatcher = configRe.matcher(configuration);
          if (!configMatcher.matches()) {
            throw new IllegalArgumentException("Illegal log config pattern: " + configuration);
          }
          String methodOrSvc = configMatcher.group(1);
          String binlogOptionStr = configMatcher.group(2);
          if (methodOrSvc.equals("*")) {
            // parse config for "*"
            checkState(
                globalLog == null,
                "Duplicate entry, this is fatal: " + configuration);
            globalLog = createBinaryLog(sink, binlogOptionStr);
            logger.log(Level.INFO, "Global binlog: {0}", binlogOptionStr);
          } else if (isServiceGlob(methodOrSvc)) {
            // parse config for a service, e.g. "service/*"
            String service = MethodDescriptor.extractFullServiceName(methodOrSvc);
            checkState(
                !perServiceLogs.containsKey(service),
                "Duplicate entry, this is fatal: " + configuration);
            perServiceLogs.put(service, createBinaryLog(sink, binlogOptionStr));
            logger.log(
                Level.INFO,
                "Service binlog: service={0} config={1}",
                new Object[] {service, binlogOptionStr});
          } else if (methodOrSvc.startsWith("-")) {
            // parse config for a method, e.g. "-service/method"
            String blacklistedMethod = methodOrSvc.substring(1);
            if (blacklistedMethod.length() == 0) {
              continue;
            }
            checkState(
                !blacklistedMethods.contains(blacklistedMethod),
                "Duplicate entry, this is fatal: " + configuration);
            checkState(
                !perMethodLogs.containsKey(blacklistedMethod),
                "Duplicate entry, this is fatal: " + configuration);
            blacklistedMethods.add(blacklistedMethod);
          } else {
            // parse config for a fully qualified method, e.g "serice/method"
            checkState(
                !perMethodLogs.containsKey(methodOrSvc),
                "Duplicate entry, this is fatal: " + configuration);
            checkState(
                !blacklistedMethods.contains(methodOrSvc),
                "Duplicate entry, this method was blacklisted: " + configuration);
            perMethodLogs.put(methodOrSvc, createBinaryLog(sink, binlogOptionStr));
            logger.log(
                Level.INFO,
                "Method binlog: method={0} config={1}",
                new Object[] {methodOrSvc, binlogOptionStr});
          }
        }
      }
      this.globalLog = globalLog;
      this.perServiceLogs = Collections.unmodifiableMap(perServiceLogs);
      this.perMethodLogs = Collections.unmodifiableMap(perMethodLogs);
      this.blacklistedMethods = Collections.unmodifiableSet(blacklistedMethods);
    }

    /**
     * Accepts a full method name and returns the log that should be used.
     */
    @Override
    public BinlogHelper getLog(String fullMethodName) {
      if (blacklistedMethods.contains(fullMethodName)) {
        return null;
      }
      BinlogHelper methodLog = perMethodLogs.get(fullMethodName);
      if (methodLog != null) {
        return methodLog;
      }
      BinlogHelper serviceLog = perServiceLogs.get(
          MethodDescriptor.extractFullServiceName(fullMethodName));
      if (serviceLog != null) {
        return serviceLog;
      }
      return globalLog;
    }

    /**
     * Returns a binlog with the correct header and message limits or {@code null} if the input
     * is malformed. The input should be a string that is in one of these forms:
     *
     * <p>{@code {h(:\d+)?}, {m(:\d+)?}, {h(:\d+)?,m(:\d+)?}}
     *
     * <p>If the {@code logConfig} is null, the returned binlog will have a limit of
     * Integer.MAX_VALUE.
     */
    @VisibleForTesting
    @Nullable
    static BinlogHelper createBinaryLog(BinaryLogSink sink, @Nullable String logConfig) {
      if (logConfig == null) {
        return new BinlogHelper(
            new SinkWriterImpl(
                sink, TimeProvider.SYSTEM_TIME_PROVIDER, Integer.MAX_VALUE, Integer.MAX_VALUE));
      }
      try {
        Matcher headerMatcher;
        Matcher msgMatcher;
        Matcher bothMatcher;
        final int maxHeaderBytes;
        final int maxMsgBytes;
        if ((headerMatcher = headerRe.matcher(logConfig)).matches()) {
          String maxHeaderStr = headerMatcher.group(1);
          maxHeaderBytes =
              maxHeaderStr != null ? Integer.parseInt(maxHeaderStr) : Integer.MAX_VALUE;
          maxMsgBytes = 0;
        } else if ((msgMatcher = msgRe.matcher(logConfig)).matches()) {
          maxHeaderBytes = 0;
          String maxMsgStr = msgMatcher.group(1);
          maxMsgBytes = maxMsgStr != null ? Integer.parseInt(maxMsgStr) : Integer.MAX_VALUE;
        } else if ((bothMatcher = bothRe.matcher(logConfig)).matches()) {
          String maxHeaderStr = bothMatcher.group(1);
          String maxMsgStr = bothMatcher.group(2);
          maxHeaderBytes =
              maxHeaderStr != null ? Integer.parseInt(maxHeaderStr) : Integer.MAX_VALUE;
          maxMsgBytes = maxMsgStr != null ? Integer.parseInt(maxMsgStr) : Integer.MAX_VALUE;
        } else {
          throw new IllegalArgumentException("Illegal log config pattern");
        }
        return new BinlogHelper(
            new SinkWriterImpl(
                sink, TimeProvider.SYSTEM_TIME_PROVIDER, maxHeaderBytes, maxMsgBytes));
      } catch (NumberFormatException e) {
        throw new IllegalArgumentException("Illegal log config pattern");
      }
    }

    /**
     * Returns true if the input string is a glob of the form: {@code <package-service>/*}.
     */
    static boolean isServiceGlob(String input) {
      return input.endsWith("/*");
    }
  }

  @VisibleForTesting
  static Address socketToProto(SocketAddress address) {
    checkNotNull(address, "address");

    Address.Builder builder = Address.newBuilder();
    if (address instanceof InetSocketAddress) {
      InetAddress inetAddress = ((InetSocketAddress) address).getAddress();
      if (inetAddress instanceof Inet4Address) {
        builder.setType(Type.TYPE_IPV4)
            .setAddress(InetAddressUtil.toAddrString(inetAddress));
      } else if (inetAddress instanceof Inet6Address) {
        builder.setType(Type.TYPE_IPV6)
            .setAddress(InetAddressUtil.toAddrString(inetAddress));
      } else {
        logger.log(Level.SEVERE, "unknown type of InetSocketAddress: {}", address);
        builder.setAddress(address.toString());
      }
      builder.setIpPort(((InetSocketAddress) address).getPort());
    } else if (address.getClass().getName().equals("io.netty.channel.unix.DomainSocketAddress")) {
      // To avoid a compile time dependency on grpc-netty, we check against the runtime class name.
      builder.setType(Type.TYPE_UNIX)
          .setAddress(address.toString());
    } else {
      builder.setType(Type.TYPE_UNKNOWN).setAddress(address.toString());
    }
    return builder.build();
  }

  private static final Set<String> NEVER_INCLUDED_METADATA = new HashSet<String>(
      Collections.singletonList(
          // grpc-status-details-bin is already logged in a field of the binlog proto
          STATUS_DETAILS_KEY.name()));
  private static final Set<String> ALWAYS_INCLUDED_METADATA = new HashSet<String>(
      Collections.singletonList(
          "grpc-trace-bin"));

  static final class MaybeTruncated<T> {
    T proto;
    boolean truncated;

    private MaybeTruncated(T proto, boolean truncated) {
      this.proto = proto;
      this.truncated = truncated;
    }
  }

  @VisibleForTesting
  static MaybeTruncated<io.grpc.binarylog.v1.Metadata.Builder> createMetadataProto(
      Metadata metadata, int maxHeaderBytes) {
    checkNotNull(metadata, "metadata");
    checkArgument(maxHeaderBytes >= 0, "maxHeaderBytes must be non negative");
    io.grpc.binarylog.v1.Metadata.Builder metaBuilder = io.grpc.binarylog.v1.Metadata.newBuilder();
    // This code is tightly coupled with Metadata's implementation
    byte[][] serialized = InternalMetadata.serialize(metadata);
    boolean truncated = false;
    if (serialized != null) {
      int curBytes = 0;
      for (int i = 0; i < serialized.length; i += 2) {
        String key = new String(serialized[i], Charsets.UTF_8);
        byte[] value = serialized[i + 1];
        if (NEVER_INCLUDED_METADATA.contains(key)) {
          continue;
        }
        boolean forceInclude = ALWAYS_INCLUDED_METADATA.contains(key);
        int bytesAfterAdd = curBytes + key.length() + value.length;
        if (!forceInclude && bytesAfterAdd > maxHeaderBytes) {
          truncated = true;
          continue;
        }
        metaBuilder.addEntryBuilder()
            .setKey(key)
            .setValue(ByteString.copyFrom(value));
        if (!forceInclude) {
          // force included keys do not count towards the size limit
          curBytes = bytesAfterAdd;
        }
      }
    }
    return new MaybeTruncated<io.grpc.binarylog.v1.Metadata.Builder>(metaBuilder, truncated);
  }

  @VisibleForTesting
  static MaybeTruncated<Message.Builder> createMessageProto(
      byte[] message, int maxMessageBytes) {
    checkNotNull(message, "message");
    checkArgument(maxMessageBytes >= 0, "maxMessageBytes must be non negative");
    Message.Builder msgBuilder = Message
        .newBuilder()
        .setLength(message.length);
    if (maxMessageBytes > 0) {
      int desiredBytes = Math.min(maxMessageBytes, message.length);
      msgBuilder.setData(ByteString.copyFrom(message, 0, desiredBytes));
    }
    return new MaybeTruncated<Message.Builder>(msgBuilder, maxMessageBytes < message.length);
  }
}
