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
import static io.grpc.services.BinaryLogProvider.BYTEARRAY_MARSHALLER;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Splitter;
import com.google.protobuf.ByteString;
import com.google.protobuf.Duration;
import com.google.protobuf.util.Durations;
import com.google.re2j.Matcher;
import com.google.re2j.Pattern;
import io.grpc.Attributes;
import io.grpc.BinaryLog.CallId;
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
import io.grpc.binarylog.v1alpha.GrpcLogEntry;
import io.grpc.binarylog.v1alpha.GrpcLogEntry.Type;
import io.grpc.binarylog.v1alpha.Message;
import io.grpc.binarylog.v1alpha.Metadata.Builder;
import io.grpc.binarylog.v1alpha.Peer;
import io.grpc.binarylog.v1alpha.Peer.PeerType;
import io.grpc.binarylog.v1alpha.Uint128;
import io.grpc.internal.GrpcUtil;
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
import java.util.concurrent.atomic.AtomicInteger;
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
  private static final boolean SERVER = true;
  private static final boolean CLIENT = false;
  // Normally 'grpc-' metadata keys are set from within gRPC, and applications are not allowed
  // to set them. This key is a special well known key that set from the application layer, but
  // represents a com.google.rpc.Status and is given special first class treatment.
  // See StatusProto.java
  static final Metadata.Key<byte[]> STATUS_DETAILS_KEY =
      Metadata.Key.of(
          "grpc-status-details-bin",
          Metadata.BINARY_BYTE_MARSHALLER);

  @VisibleForTesting
  static final SocketAddress DUMMY_SOCKET = new DummySocketAddress();
  @VisibleForTesting
  static final boolean DUMMY_IS_COMPRESSED = false;

  @VisibleForTesting
  final SinkWriter writer;

  @VisibleForTesting
  BinlogHelper(SinkWriter writer) {
    this.writer = writer;
  }

  // TODO(zpencer): move proto related static helpers into this class
  static final class SinkWriterImpl extends SinkWriter {
    private final BinaryLogSink sink;
    private final int maxHeaderBytes;
    private final int maxMessageBytes;

    SinkWriterImpl(BinaryLogSink sink, int maxHeaderBytes, int maxMessageBytes) {
      this.sink = sink;
      this.maxHeaderBytes = maxHeaderBytes;
      this.maxMessageBytes = maxMessageBytes;
    }

    @Override
    void logSendInitialMetadata(
        int seq,
        @Nullable String methodName, // null on server
        @Nullable Duration timeout, // null on server
        Metadata metadata,
        boolean isServer,
        CallId callId) {
      Preconditions.checkArgument(methodName == null || !isServer);
      Preconditions.checkArgument(timeout == null || !isServer);
      // Java does not include the leading '/'. To be consistent with the rest of gRPC we must
      // include the '/' in the fully qualified name for binlogs.
      Preconditions.checkArgument(methodName == null || !methodName.startsWith("/"));
      GrpcLogEntry.Builder entryBuilder = GrpcLogEntry.newBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(Type.SEND_INITIAL_METADATA)
          .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
          .setCallId(callIdToProto(callId));
      addMetadataToProto(entryBuilder, metadata, maxHeaderBytes);
      if (methodName != null) {
        entryBuilder.setMethodName("/" + methodName);
      }
      if (timeout != null) {
        entryBuilder.setTimeout(timeout);
      }
      sink.write(entryBuilder.build());
    }

    @Override
    void logRecvInitialMetadata(
        int seq,
        @Nullable String methodName, // null on client
        @Nullable Duration timeout,  // null on client
        Metadata metadata,
        boolean isServer,
        CallId callId,
        SocketAddress peerSocket) {
      Preconditions.checkArgument(methodName == null || isServer);
      Preconditions.checkArgument(timeout == null || isServer);
      // Java does not include the leading '/'. To be consistent with the rest of gRPC we must
      // include the '/' in the fully qualified name for binlogs.
      Preconditions.checkArgument(methodName == null || !methodName.startsWith("/"));
      GrpcLogEntry.Builder entryBuilder = GrpcLogEntry.newBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(Type.RECV_INITIAL_METADATA)
          .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
          .setCallId(callIdToProto(callId))
          .setPeer(socketToProto(peerSocket));
      addMetadataToProto(entryBuilder, metadata, maxHeaderBytes);
      if (methodName != null) {
        entryBuilder.setMethodName("/" + methodName);
      }
      if (timeout != null) {
        entryBuilder.setTimeout(timeout);
      }
      sink.write(entryBuilder.build());
    }

    @Override
    void logTrailingMetadata(
        int seq, Status status, Metadata metadata, boolean isServer, CallId callId) {
      GrpcLogEntry.Builder entryBuilder = GrpcLogEntry.newBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(isServer ? Type.SEND_TRAILING_METADATA : Type.RECV_TRAILING_METADATA)
          .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
          .setCallId(callIdToProto(callId))
          .setStatusCode(status.getCode().value());
      String statusDescription = status.getDescription();
      if (statusDescription != null) {
        entryBuilder.setStatusMessage(statusDescription);
      }
      byte[] statusDetailBytes = metadata.get(STATUS_DETAILS_KEY);
      if (statusDetailBytes != null) {
        entryBuilder.setStatusDetails(ByteString.copyFrom(statusDetailBytes));
      }

      addMetadataToProto(entryBuilder, metadata, maxHeaderBytes);
      sink.write(entryBuilder.build());
    }

    @Override
    <T> void logOutboundMessage(
        int seq,
        Marshaller<T> marshaller,
        T message,
        boolean compressed,
        boolean isServer,
        CallId callId) {
      if (marshaller != BYTEARRAY_MARSHALLER) {
        throw new IllegalStateException("Expected the BinaryLog's ByteArrayMarshaller");
      }
      GrpcLogEntry.Builder entryBuilder = GrpcLogEntry.newBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(Type.SEND_MESSAGE)
          .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
          .setCallId(callIdToProto(callId));
      messageToProto(entryBuilder, (byte[]) message, compressed, maxMessageBytes);
      sink.write(entryBuilder.build());
    }

    @Override
    <T> void logInboundMessage(
        int seq,
        Marshaller<T> marshaller,
        T message,
        boolean compressed,
        boolean isServer,
        CallId callId) {
      if (marshaller != BYTEARRAY_MARSHALLER) {
        throw new IllegalStateException("Expected the BinaryLog's ByteArrayMarshaller");
      }
      GrpcLogEntry.Builder entryBuilder = GrpcLogEntry.newBuilder()
          .setSequenceIdWithinCall(seq)
          .setType(Type.RECV_MESSAGE)
          .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
          .setCallId(callIdToProto(callId));

      messageToProto(entryBuilder, (byte[]) message, compressed, maxMessageBytes);
      sink.write(entryBuilder.build());
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
     * Logs the sending of initial metadata. This method logs the appropriate number of bytes
     * as determined by the binary logging configuration.
     */
    abstract void logSendInitialMetadata(
        int seq,
        String methodName,
        Duration timeout,
        Metadata metadata,
        boolean isServer,
        CallId callId);

    /**
     * Logs the receiving of initial metadata. This method logs the appropriate number of bytes
     * as determined by the binary logging configuration.
     */
    abstract void logRecvInitialMetadata(
        int seq,
        String methodName,
        Duration timeout,
        Metadata metadata,
        boolean isServer,
        CallId callId,
        SocketAddress peerSocket);

    /**
     * Logs the trailing metadata. This method logs the appropriate number of bytes
     * as determined by the binary logging configuration.
     */
    abstract void logTrailingMetadata(
        int seq, Status status, Metadata metadata, boolean isServer, CallId callId);

    /**
     * Logs the outbound message. This method logs the appropriate number of bytes from
     * {@code message}, and returns a duplicate of the message.
     * The number of bytes logged is determined by the binary logging configuration.
     * This method takes ownership of {@code message}.
     */
    abstract <T> void logOutboundMessage(
        int seq, Marshaller<T> marshaller, T message, boolean compressed, boolean isServer,
        CallId callId);

    /**
     * Logs the inbound message. This method logs the appropriate number of bytes from
     * {@code message}, and returns a duplicate of the message.
     * The number of bytes logged is determined by the binary logging configuration.
     * This method takes ownership of {@code message}.
     */
    abstract <T> void logInboundMessage(
        int seq, Marshaller<T> marshaller, T message, boolean compressed, boolean isServer,
        CallId callId);

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
    SocketAddress peer = streamAttributes.get(Grpc.TRANSPORT_ATTR_REMOTE_ADDR);
    if (peer == null) {
      return DUMMY_SOCKET;
    }
    return peer;
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

  public ClientInterceptor getClientInterceptor(final CallId callId) {
    return new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          final MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
        final AtomicInteger seq = new AtomicInteger(1);
        final String methodName = method.getFullMethodName();
        // The timeout should reflect the time remaining when the call is started, so do not
        // compute remaining time here.
        final Deadline deadline = min(callOptions.getDeadline(), Context.current().getDeadline());

        return new SimpleForwardingClientCall<ReqT, RespT>(next.newCall(method, callOptions)) {
          @Override
          public void start(Listener<RespT> responseListener, Metadata headers) {
            final Duration timeout = deadline == null ? null
                : Durations.fromNanos(deadline.timeRemaining(TimeUnit.NANOSECONDS));
            writer.logSendInitialMetadata(
                seq.getAndIncrement(), methodName, timeout, headers, CLIENT, callId);
            ClientCall.Listener<RespT> wListener =
                new SimpleForwardingClientCallListener<RespT>(responseListener) {
                  @Override
                  public void onMessage(RespT message) {
                    writer.logInboundMessage(
                        seq.getAndIncrement(),
                        method.getResponseMarshaller(),
                        message,
                        DUMMY_IS_COMPRESSED,
                        CLIENT,
                        callId);
                    super.onMessage(message);
                  }

                  @Override
                  public void onHeaders(Metadata headers) {
                    SocketAddress peer = getPeerSocket(getAttributes());
                    writer.logRecvInitialMetadata(
                        seq.getAndIncrement(),
                        /*methodName=*/ null,
                        /*timeout=*/ null,
                        headers,
                        CLIENT,
                        callId,
                        peer);
                    super.onHeaders(headers);
                  }

                  @Override
                  public void onClose(Status status, Metadata trailers) {
                    writer.logTrailingMetadata(
                        seq.getAndIncrement(), status, trailers, CLIENT, callId);
                    super.onClose(status, trailers);
                  }
                };
            super.start(wListener, headers);
          }

          @Override
          public void sendMessage(ReqT message) {
            writer.logOutboundMessage(
                seq.getAndIncrement(),
                method.getRequestMarshaller(),
                message,
                DUMMY_IS_COMPRESSED,
                CLIENT,
                callId);
            super.sendMessage(message);
          }
        };
      }
    };
  }

  public ServerInterceptor getServerInterceptor(final CallId callId) {
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> Listener<ReqT> interceptCall(
          final ServerCall<ReqT, RespT> call,
          Metadata headers,
          ServerCallHandler<ReqT, RespT> next) {
        final AtomicInteger seq = new AtomicInteger(1);
        SocketAddress peer = getPeerSocket(call.getAttributes());
        String methodName = call.getMethodDescriptor().getFullMethodName();
        Long timeoutNanos = headers.get(GrpcUtil.TIMEOUT_KEY);
        final Duration timeout =
            timeoutNanos == null ? null : Durations.fromNanos(timeoutNanos);

        writer.logRecvInitialMetadata(
            seq.getAndIncrement(), methodName, timeout, headers, SERVER, callId, peer);
        ServerCall<ReqT, RespT> wCall = new SimpleForwardingServerCall<ReqT, RespT>(call) {
          @Override
          public void sendMessage(RespT message) {
            writer.logOutboundMessage(
                seq.getAndIncrement(),
                call.getMethodDescriptor().getResponseMarshaller(),
                message,
                DUMMY_IS_COMPRESSED,
                SERVER,
                callId);
            super.sendMessage(message);
          }

          @Override
          public void sendHeaders(Metadata headers) {
            writer.logSendInitialMetadata(
                seq.getAndIncrement(),
                /*methodName=*/ null,
                /*timeout=*/ null,
                headers,
                SERVER,
                callId);
            super.sendHeaders(headers);
          }

          @Override
          public void close(Status status, Metadata trailers) {
            writer.logTrailingMetadata(seq.getAndIncrement(), status, trailers, SERVER, callId);
            super.close(status, trailers);
          }
        };

        return new SimpleForwardingServerCallListener<ReqT>(next.startCall(wCall, headers)) {
          @Override
          public void onMessage(ReqT message) {
            writer.logInboundMessage(
                seq.getAndIncrement(),
                call.getMethodDescriptor().getRequestMarshaller(),
                message,
                DUMMY_IS_COMPRESSED,
                SERVER,
                callId);
            super.onMessage(message);
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
            throw new IllegalArgumentException("Bad input: " + configuration);
          }
          String methodOrSvc = configMatcher.group(1);
          String binlogOptionStr = configMatcher.group(2);
          BinlogHelper binLog = createBinaryLog(sink, binlogOptionStr);
          if (binLog == null) {
            continue;
          }
          if (methodOrSvc.equals("*")) {
            if (globalLog != null) {
              logger.log(Level.SEVERE, "Ignoring duplicate entry: {0}", configuration);
              continue;
            }
            globalLog = binLog;
            logger.log(Level.INFO, "Global binlog: {0}", binlogOptionStr);
          } else if (isServiceGlob(methodOrSvc)) {
            String service = MethodDescriptor.extractFullServiceName(methodOrSvc);
            if (perServiceLogs.containsKey(service)) {
              logger.log(Level.SEVERE, "Ignoring duplicate entry: {0}", configuration);
              continue;
            }
            perServiceLogs.put(service, binLog);
            logger.log(
                Level.INFO,
                "Service binlog: service={0} config={1}",
                new Object[] {service, binlogOptionStr});
          } else if (methodOrSvc.startsWith("-")) {
            String blacklistedMethod = methodOrSvc.substring(1);
            if (blacklistedMethod.length() == 0) {
              continue;
            }
            if (!blacklistedMethods.add(blacklistedMethod)) {
              logger.log(Level.SEVERE, "Ignoring duplicate entry: {0}", configuration);
            }
          } else {
            // assume fully qualified method name
            if (perMethodLogs.containsKey(methodOrSvc)) {
              logger.log(Level.SEVERE, "Ignoring duplicate entry: {0}", configuration);
              continue;
            }
            perMethodLogs.put(methodOrSvc, binLog);
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
            new SinkWriterImpl(sink, Integer.MAX_VALUE, Integer.MAX_VALUE));
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
          logger.log(Level.SEVERE, "Illegal log config pattern: " + logConfig);
          return null;
        }
        return new BinlogHelper(new SinkWriterImpl(sink, maxHeaderBytes, maxMsgBytes));
      } catch (NumberFormatException e) {
        logger.log(Level.SEVERE, "Illegal log config pattern: " + logConfig);
        return null;
      }
    }

    /**
     * Returns true if the input string is a glob of the form: {@code <package-service>/*}.
     */
    static boolean isServiceGlob(String input) {
      return input.endsWith("/*");
    }
  }

  /**
   * Returns a {@link Uint128} from a CallId.
   */
  static Uint128 callIdToProto(CallId callId) {
    checkNotNull(callId, "callId");
    return Uint128
        .newBuilder()
        .setHigh(callId.hi)
        .setLow(callId.lo)
        .build();
  }

  @VisibleForTesting
  static Peer socketToProto(SocketAddress address) {
    checkNotNull(address, "address");

    Peer.Builder builder = Peer.newBuilder();
    if (address instanceof InetSocketAddress) {
      InetAddress inetAddress = ((InetSocketAddress) address).getAddress();
      if (inetAddress instanceof Inet4Address) {
        builder.setPeerType(PeerType.PEER_IPV4)
            .setAddress(InetAddressUtil.toAddrString(inetAddress));
      } else if (inetAddress instanceof Inet6Address) {
        builder.setPeerType(PeerType.PEER_IPV6)
            .setAddress(InetAddressUtil.toAddrString(inetAddress));
      } else {
        logger.log(Level.SEVERE, "unknown type of InetSocketAddress: {}", address);
        builder.setAddress(address.toString());
      }
      builder.setIpPort(((InetSocketAddress) address).getPort());
    } else if (address.getClass().getName().equals("io.netty.channel.unix.DomainSocketAddress")) {
      // To avoid a compile time dependency on grpc-netty, we check against the runtime class name.
      builder.setPeerType(PeerType.PEER_UNIX)
          .setAddress(address.toString());
    } else {
      builder.setPeerType(PeerType.UNKNOWN_PEERTYPE).setAddress(address.toString());
    }
    return builder.build();
  }

  @VisibleForTesting
  static void addMetadataToProto(
      GrpcLogEntry.Builder entryBuilder, Metadata metadata, int maxHeaderBytes) {
    checkNotNull(entryBuilder, "entryBuilder");
    checkNotNull(metadata, "metadata");
    checkArgument(maxHeaderBytes >= 0, "maxHeaderBytes must be non negative");
    Builder metaBuilder = io.grpc.binarylog.v1alpha.Metadata.newBuilder();
    // This code is tightly coupled with Metadata's implementation
    byte[][] serialized = null;
    if (maxHeaderBytes > 0 && (serialized = InternalMetadata.serialize(metadata)) != null) {
      int written = 0;
      for (int i = 0; i < serialized.length && written < maxHeaderBytes; i += 2) {
        byte[] key = serialized[i];
        byte[] value = serialized[i + 1];
        if (written + key.length + value.length <= maxHeaderBytes) {
          metaBuilder.addEntryBuilder()
                  .setKey(ByteString.copyFrom(key))
                  .setValue(ByteString.copyFrom(value));
          written += key.length;
          written += value.length;
        }
      }
    }
    // This check must be updated when we add filtering
    entryBuilder.setTruncated(maxHeaderBytes == 0
        || (serialized != null && metaBuilder.getEntryCount() < (serialized.length / 2)));
    entryBuilder.setMetadata(metaBuilder);
  }

  @VisibleForTesting
  static void messageToProto(
      GrpcLogEntry.Builder entryBuilder, byte[] message, boolean compressed, int maxMessageBytes) {
    checkNotNull(message, "message");
    checkArgument(maxMessageBytes >= 0, "maxMessageBytes must be non negative");
    Message.Builder msgBuilder = Message
        .newBuilder()
        .setFlags(flagsForMessage(compressed))
        .setLength(message.length);
    if (maxMessageBytes > 0) {
      int desiredBytes = Math.min(maxMessageBytes, message.length);
      msgBuilder.setData(ByteString.copyFrom(message, 0, desiredBytes));
    }
    entryBuilder.setMessage(msgBuilder);
    entryBuilder.setTruncated(maxMessageBytes < message.length);
  }

  /**
   * Returns a flag based on the arguments.
   */
  @VisibleForTesting
  static int flagsForMessage(boolean compressed) {
    return compressed ? 1 : 0;
  }

  private static class DummySocketAddress extends SocketAddress {
    private static final long serialVersionUID = 0;
  }
}
