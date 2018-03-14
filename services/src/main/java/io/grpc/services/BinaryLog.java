/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import com.google.common.base.Splitter;
import com.google.common.primitives.Bytes;
import com.google.protobuf.ByteString;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.binarylog.GrpcLogEntry;
import io.grpc.binarylog.GrpcLogEntry.Type;
import io.grpc.binarylog.Message;
import io.grpc.binarylog.Metadata.Builder;
import io.grpc.binarylog.MetadataEntry;
import io.grpc.binarylog.Peer;
import io.grpc.binarylog.Peer.PeerType;
import io.grpc.binarylog.Uint128;
import java.io.InputStream;
import java.net.Inet4Address;
import java.net.Inet6Address;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.nio.ByteBuffer;
import java.nio.charset.Charset;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.logging.Level;
import java.util.logging.Logger;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A binary log class that is configured for a specific {@link MethodDescriptor}.
 */
// TODO(zpencer): this is really a per-method logger class, make the class name more clear.
@ThreadSafe
final class BinaryLog {
  private static final Logger logger = Logger.getLogger(BinaryLog.class.getName());
  private static final int IP_PORT_BYTES = 2;
  private static final int IP_PORT_UPPER_MASK = 0xff00;
  private static final int IP_PORT_LOWER_MASK = 0xff;

  private final BinaryLogSink sink;
  @VisibleForTesting
  final int maxHeaderBytes;
  @VisibleForTesting
  final int maxMessageBytes;

  @VisibleForTesting
  BinaryLog(BinaryLogSink sink, int maxHeaderBytes, int maxMessageBytes) {
    this.sink = sink;
    this.maxHeaderBytes = maxHeaderBytes;
    this.maxMessageBytes = maxMessageBytes;
  }

  /**
   * Logs the sending of initial metadata. This method logs the appropriate number of bytes
   * as determined by the binary logging configuration.
   */
  public void logSendInitialMetadata(
      Metadata metadata, boolean isServer, byte[] callId, SocketAddress peerSocket) {
    Preconditions.checkNotNull(metadata);
    // TODO(zpencer): peerSocket may go away, because at this time we have not selected a peer yet
    Preconditions.checkNotNull(peerSocket);
    GrpcLogEntry entry = GrpcLogEntry
        .newBuilder()
        .setType(Type.SEND_INITIAL_METADATA)
        .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
        .setCallId(callIdToProto(callId))
        .setPeer(socketToProto(peerSocket))
        .setMetadata(metadataToProto(metadata, maxHeaderBytes))
        .build();
    sink.write(entry);
  }

  /**
   * Logs the receiving of initial metadata. This method logs the appropriate number of bytes
   * as determined by the binary logging configuration.
   */
  public void logRecvInitialMetadata(
      Metadata metadata, boolean isServer, byte[] callId, SocketAddress peerSocket) {
    Preconditions.checkNotNull(metadata);
    Preconditions.checkNotNull(peerSocket);
    GrpcLogEntry entry = GrpcLogEntry
        .newBuilder()
        .setType(Type.RECV_INITIAL_METADATA)
        .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
        .setCallId(callIdToProto(callId))
        .setPeer(socketToProto(peerSocket))
        .setMetadata(metadataToProto(metadata, maxHeaderBytes))
        .build();
    sink.write(entry);
  }

  /**
   * Logs the trailing metadata. This method logs the appropriate number of bytes
   * as determined by the binary logging configuration.
   */
  public void logTrailingMetadata(Metadata metadata, boolean isServer, byte[] callId) {
    Preconditions.checkNotNull(metadata);
    GrpcLogEntry entry = GrpcLogEntry
        .newBuilder()
        .setType(isServer ? Type.SEND_TRAILING_METADATA : Type.RECV_TRAILING_METADATA)
        .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
        .setCallId(callIdToProto(callId))
        .setMetadata(metadataToProto(metadata, maxHeaderBytes))
        .build();
    sink.write(entry);
  }

  /**
   * Logs the outbound message. This method logs the appropriate number of bytes from
   * {@code message}, and returns an {@link InputStream} that contains the original message.
   * The number of bytes logged is determined by the binary logging configuration.
   */
  public void logOutboundMessage(
      ByteBuffer message, boolean compressed, boolean isServer, byte[] callId) {
    Preconditions.checkNotNull(message);
    Preconditions.checkNotNull(callId);
    GrpcLogEntry entry = GrpcLogEntry
        .newBuilder()
        .setType(Type.SEND_MESSAGE)
        .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
        .setCallId(callIdToProto(callId))
        .setMessage(messageToProto(message, compressed, maxMessageBytes))
        .build();
    sink.write(entry);
  }

  /**
   * Logs the inbound message. This method logs the appropriate number of bytes from
   * {@code message}, and returns an {@link InputStream} that contains the original message.
   * The number of bytes logged is determined by the binary logging configuration.
   */
  public void logInboundMessage(
      ByteBuffer message, boolean compressed, boolean isServer, byte[] callId) {
    Preconditions.checkNotNull(message);
    Preconditions.checkNotNull(callId);
    GrpcLogEntry entry = GrpcLogEntry
        .newBuilder()
        .setType(Type.RECV_MESSAGE)
        .setLogger(isServer ? GrpcLogEntry.Logger.SERVER : GrpcLogEntry.Logger.CLIENT)
        .setCallId(callIdToProto(callId))
        .setMessage(messageToProto(message, compressed, maxMessageBytes))
        .build();
    sink.write(entry);
  }

  @Override
  public boolean equals(Object o) {
    if (!(o instanceof BinaryLog)) {
      return false;
    }
    BinaryLog that = (BinaryLog) o;
    return this.maxHeaderBytes == that.maxHeaderBytes
        && this.maxMessageBytes == that.maxMessageBytes
        && this.sink.equals(that.sink);
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(maxHeaderBytes, maxMessageBytes);
  }

  @Override
  public String toString() {
    return getClass().getSimpleName() + '['
        + "maxHeaderBytes=" + maxHeaderBytes + ", "
        + "maxMessageBytes=" + maxMessageBytes
        + "sink=" + sink
        + "]";
  }

  private static final Factory DEFAULT_FACTORY;
  private static final Factory NULL_FACTORY = new NullFactory();

  static {
    Factory defaultFactory = NULL_FACTORY;
    try {
      String configStr = System.getenv("GRPC_BINARY_LOG_CONFIG");
      // TODO(zpencer): make BinaryLog.java implement isAvailable, and put this check there
      BinaryLogSink sink = BinaryLogSinkProvider.provider();
      if (sink != null && configStr != null && configStr.length() > 0) {
        defaultFactory = new FactoryImpl(sink, configStr);
      }
    } catch (Throwable t) {
      logger.log(Level.SEVERE, "Failed to initialize binary log. Disabling binary log.", t);
      defaultFactory = NULL_FACTORY;
    }
    DEFAULT_FACTORY = defaultFactory;
  }

  /**
   * Accepts the fullMethodName and returns the binary log that should be used. The log may be
   * a log that does nothing.
   */
  static BinaryLog getLog(String fullMethodName) {
    return DEFAULT_FACTORY.getLog(fullMethodName);
  }

  interface Factory {
    @Nullable
    BinaryLog getLog(String fullMethodName);
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

    private final BinaryLog globalLog;
    private final Map<String, BinaryLog> perServiceLogs;
    private final Map<String, BinaryLog> perMethodLogs;

    /**
     * Accepts a string in the format specified by the binary log spec.
     */
    @VisibleForTesting
    FactoryImpl(BinaryLogSink sink, String configurationString) {
      Preconditions.checkState(configurationString != null && configurationString.length() > 0);
      BinaryLog globalLog = null;
      Map<String, BinaryLog> perServiceLogs = new HashMap<String, BinaryLog>();
      Map<String, BinaryLog> perMethodLogs = new HashMap<String, BinaryLog>();

      for (String configuration : Splitter.on(',').split(configurationString)) {
        Matcher configMatcher = configRe.matcher(configuration);
        if (!configMatcher.matches()) {
          throw new IllegalArgumentException("Bad input: " + configuration);
        }
        String methodOrSvc = configMatcher.group(1);
        String binlogOptionStr = configMatcher.group(2);
        BinaryLog binLog = createBinaryLog(sink, binlogOptionStr);
        if (binLog == null) {
          continue;
        }
        if (methodOrSvc.equals("*")) {
          if (globalLog != null) {
            logger.log(Level.SEVERE, "Ignoring duplicate entry: " + configuration);
            continue;
          }
          globalLog = binLog;
          logger.info("Global binlog: " + globalLog);
        } else if (isServiceGlob(methodOrSvc)) {
          String service = MethodDescriptor.extractFullServiceName(methodOrSvc);
          if (perServiceLogs.containsKey(service)) {
            logger.log(Level.SEVERE, "Ignoring duplicate entry: " + configuration);
            continue;
          }
          perServiceLogs.put(service, binLog);
          logger.info(String.format("Service binlog: service=%s log=%s", service, binLog));
        } else {
          // assume fully qualified method name
          if (perMethodLogs.containsKey(methodOrSvc)) {
            logger.log(Level.SEVERE, "Ignoring duplicate entry: " + configuration);
            continue;
          }
          perMethodLogs.put(methodOrSvc, binLog);
          logger.info(String.format("Method binlog: method=%s log=%s", methodOrSvc, binLog));
        }
      }
      this.globalLog = globalLog;
      this.perServiceLogs = Collections.unmodifiableMap(perServiceLogs);
      this.perMethodLogs = Collections.unmodifiableMap(perMethodLogs);
    }

    /**
     * Accepts a full method name and returns the log that should be used.
     */
    @Override
    public BinaryLog getLog(String fullMethodName) {
      BinaryLog methodLog = perMethodLogs.get(fullMethodName);
      if (methodLog != null) {
        return methodLog;
      }
      BinaryLog serviceLog = perServiceLogs.get(
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
    static BinaryLog createBinaryLog(BinaryLogSink sink, @Nullable String logConfig) {
      if (logConfig == null) {
        return new BinaryLog(sink, Integer.MAX_VALUE, Integer.MAX_VALUE);
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
        return new BinaryLog(sink, maxHeaderBytes, maxMsgBytes);
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

  private static final class NullFactory implements Factory {
    @Override
    public BinaryLog getLog(String fullMethodName) {
      return null;
    }
  }

  /**
   * Returns a {@link Uint128} by interpreting the first 8 bytes as the high int64 and the second
   * 8 bytes as the low int64.
   */
  // TODO(zpencer): verify int64 representation with other gRPC languages
  static Uint128 callIdToProto(byte[] bytes) {
    Preconditions.checkNotNull(bytes);
    Preconditions.checkArgument(
        bytes.length == 16,
        String.format("can only convert from 16 byte input, actual length = %d", bytes.length));
    ByteBuffer bb = ByteBuffer.wrap(bytes);
    long high = bb.getLong();
    long low = bb.getLong();
    return Uint128.newBuilder().setHigh(high).setLow(low).build();
  }

  @VisibleForTesting
  // TODO(zpencer): the binlog design does not specify how to actually express the peer bytes
  static Peer socketToProto(SocketAddress address) {
    Preconditions.checkNotNull(address);
    PeerType peerType = PeerType.UNKNOWN_PEERTYPE;
    byte[] peerAddress = null;

    if (address instanceof InetSocketAddress) {
      InetAddress inetAddress = ((InetSocketAddress) address).getAddress();
      if (inetAddress instanceof Inet4Address) {
        peerType = PeerType.PEER_IPV4;
      } else if (inetAddress instanceof Inet6Address) {
        peerType = PeerType.PEER_IPV6;
      } else {
        logger.log(Level.SEVERE, "unknown type of InetSocketAddress: {}", address);
      }
      int port = ((InetSocketAddress) address).getPort();
      byte[] portBytes = new byte[IP_PORT_BYTES];
      portBytes[0] = (byte) ((port & IP_PORT_UPPER_MASK) >> 8);
      portBytes[1] = (byte) (port & IP_PORT_LOWER_MASK);
      peerAddress = Bytes.concat(inetAddress.getAddress(), portBytes);
    } else if (address.getClass().getName().equals("io.netty.channel.unix.DomainSocketAddress")) {
      // To avoid a compile time dependency on grpc-netty, we check against the runtime class name.
      peerType = PeerType.PEER_UNIX;
    }
    if (peerAddress == null) {
      peerAddress = address.toString().getBytes(Charset.defaultCharset());
    }
    return Peer.newBuilder()
        .setPeerType(peerType)
        .setPeer(ByteString.copyFrom(peerAddress))
        .build();
  }

  @VisibleForTesting
  static io.grpc.binarylog.Metadata metadataToProto(Metadata metadata, int maxHeaderBytes) {
    Preconditions.checkNotNull(metadata);
    Preconditions.checkState(maxHeaderBytes >= 0);
    Builder builder = io.grpc.binarylog.Metadata.newBuilder();
    // This code is tightly coupled with Metadata's implementation
    byte[][] serialized;
    if (maxHeaderBytes > 0 && (serialized = InternalMetadata.serialize(metadata)) != null) {
      int written = 0;
      for (int i = 0; i < serialized.length && written < maxHeaderBytes; i += 2) {
        byte[] key = serialized[i];
        byte[] value = serialized[i + 1];
        if (written + key.length + value.length <= maxHeaderBytes) {
          builder.addEntry(
              MetadataEntry
                  .newBuilder()
                  .setKey(ByteString.copyFrom(key))
                  .setValue(ByteString.copyFrom(value))
                  .build());
          written += key.length;
          written += value.length;
        }
      }
    }
    return builder.build();
  }

  @VisibleForTesting
  static Message messageToProto(ByteBuffer message, boolean compressed, int maxMessageBytes) {
    Preconditions.checkNotNull(message);
    int messageSize = message.remaining();
    Message.Builder builder = Message
        .newBuilder()
        .setFlags(flagsForMessage(compressed))
        .setLength(messageSize);
    if (maxMessageBytes > 0) {
      int desiredRemaining = Math.min(maxMessageBytes, messageSize);
      ByteBuffer dup = message.duplicate();
      dup.limit(dup.position() + desiredRemaining);
      builder.setData(ByteString.copyFrom(dup));
    }
    return builder.build();
  }

  /**
   * Returns a flag based on the arguments.
   */
  @VisibleForTesting
  static int flagsForMessage(boolean compressed) {
    return compressed ? 1 : 0;
  }
}
