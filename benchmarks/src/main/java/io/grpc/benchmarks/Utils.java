/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.benchmarks;

import static java.util.concurrent.ForkJoinPool.defaultForkJoinWorkerThreadFactory;

import com.google.common.util.concurrent.UncaughtExceptionHandlers;
import com.google.protobuf.ByteString;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Status;
import io.grpc.benchmarks.proto.Messages;
import io.grpc.benchmarks.proto.Messages.Payload;
import io.grpc.benchmarks.proto.Messages.SimpleRequest;
import io.grpc.benchmarks.proto.Messages.SimpleResponse;
import io.grpc.internal.testing.TestUtils;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.okhttp.OkHttpChannelBuilder;
import io.grpc.okhttp.internal.Platform;
import io.netty.channel.epoll.EpollDomainSocketChannel;
import io.netty.channel.epoll.EpollEventLoopGroup;
import io.netty.channel.epoll.EpollSocketChannel;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.channel.unix.DomainSocketAddress;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.SocketAddress;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.ForkJoinPool;
import java.util.concurrent.ForkJoinPool.ForkJoinWorkerThreadFactory;
import java.util.concurrent.ForkJoinWorkerThread;
import java.util.concurrent.atomic.AtomicInteger;
import javax.annotation.Nullable;
import org.HdrHistogram.Histogram;

/**
 * Utility methods to support benchmarking classes.
 */
public final class Utils {
  private static final String UNIX_DOMAIN_SOCKET_PREFIX = "unix://";

  // The histogram can record values between 1 microsecond and 1 min.
  public static final long HISTOGRAM_MAX_VALUE = 60000000L;

  // Value quantization will be no more than 1%. See the README of HdrHistogram for more details.
  public static final int HISTOGRAM_PRECISION = 2;

  public static final int DEFAULT_FLOW_CONTROL_WINDOW =
      NettyChannelBuilder.DEFAULT_FLOW_CONTROL_WINDOW;

  private Utils() {
  }

  public static boolean parseBoolean(String value) {
    return value.isEmpty() || Boolean.parseBoolean(value);
  }

  /**
   * Parse a {@link SocketAddress} from the given string.
   */
  public static SocketAddress parseSocketAddress(String value) {
    if (value.startsWith(UNIX_DOMAIN_SOCKET_PREFIX)) {
      // Unix Domain Socket address.
      // Create the underlying file for the Unix Domain Socket.
      String filePath = value.substring(UNIX_DOMAIN_SOCKET_PREFIX.length());
      File file = new File(filePath);
      if (!file.isAbsolute()) {
        throw new IllegalArgumentException("File path must be absolute: " + filePath);
      }
      try {
        if (file.createNewFile()) {
          // If this application created the file, delete it when the application exits.
          file.deleteOnExit();
        }
      } catch (IOException ex) {
        throw new RuntimeException(ex);
      }
      // Create the SocketAddress referencing the file.
      return new DomainSocketAddress(file);
    } else {
      // Standard TCP/IP address.
      String[] parts = value.split(":", 2);
      if (parts.length < 2) {
        throw new IllegalArgumentException(
            "Address must be a unix:// path or be in the form host:port. Got: " + value);
      }
      String host = parts[0];
      int port = Integer.parseInt(parts[1]);
      return new InetSocketAddress(host, port);
    }
  }

  private static OkHttpChannelBuilder newOkHttpClientChannel(
      SocketAddress address, boolean tls, boolean testca) {
    InetSocketAddress addr = (InetSocketAddress) address;
    OkHttpChannelBuilder builder =
        OkHttpChannelBuilder.forAddress(addr.getHostName(), addr.getPort());
    if (!tls) {
      builder.usePlaintext();
    } else if (testca) {
      try {
        builder.sslSocketFactory(TestUtils.newSslSocketFactoryForCa(
            Platform.get().getProvider(),
            TestUtils.loadCert("ca.pem")));
      } catch (Exception e) {
        throw new RuntimeException(e);
      }
    }
    return builder;
  }

  private static NettyChannelBuilder newNettyClientChannel(Transport transport,
      SocketAddress address, boolean tls, boolean testca, int flowControlWindow)
      throws IOException {
    NettyChannelBuilder builder =
        NettyChannelBuilder.forAddress(address).flowControlWindow(flowControlWindow);
    if (!tls) {
      builder.usePlaintext();
    } else if (testca) {
      File cert = TestUtils.loadCert("ca.pem");
      builder.sslContext(GrpcSslContexts.forClient().trustManager(cert).build());
    }

    DefaultThreadFactory tf = new DefaultThreadFactory("client-elg-", true /*daemon */);
    switch (transport) {
      case NETTY_NIO:
        builder
            .eventLoopGroup(new NioEventLoopGroup(0, tf))
            .channelType(NioSocketChannel.class);
        break;

      case NETTY_EPOLL:
        // These classes only work on Linux.
        builder
            .eventLoopGroup(new EpollEventLoopGroup(0, tf))
            .channelType(EpollSocketChannel.class);
        break;

      case NETTY_UNIX_DOMAIN_SOCKET:
        // These classes only work on Linux.
        builder
            .eventLoopGroup(new EpollEventLoopGroup(0, tf))
            .channelType(EpollDomainSocketChannel.class);
        break;

      default:
        // Should never get here.
        throw new IllegalArgumentException("Unsupported transport: " + transport);
    }
    return builder;
  }

  private static ExecutorService clientExecutor;

  private static synchronized ExecutorService getExecutor() {
    if (clientExecutor == null) {
      clientExecutor = new ForkJoinPool(
          Runtime.getRuntime().availableProcessors(),
          new ForkJoinWorkerThreadFactory() {
            final AtomicInteger num = new AtomicInteger();
            @Override
            public ForkJoinWorkerThread newThread(ForkJoinPool pool) {
              ForkJoinWorkerThread thread = defaultForkJoinWorkerThreadFactory.newThread(pool);
              thread.setDaemon(true);
              thread.setName("grpc-client-app-" + "-" + num.getAndIncrement());
              return thread;
            }
          }, UncaughtExceptionHandlers.systemExit(), true /* async */);
    }
    return clientExecutor;
  }

  /**
   * Create a {@link ManagedChannel} for the given parameters.
   */
  public static ManagedChannel newClientChannel(Transport transport, SocketAddress address,
        boolean tls, boolean testca, @Nullable String authorityOverride,
        int flowControlWindow, boolean directExecutor) {
    ManagedChannelBuilder<?> builder;
    if (transport == Transport.OK_HTTP) {
      builder = newOkHttpClientChannel(address, tls, testca);
    } else {
      try {
        builder = newNettyClientChannel(transport, address, tls, testca, flowControlWindow);
      } catch (Exception e) {
        throw new RuntimeException(e);
      }
    }
    if (authorityOverride != null) {
      builder.overrideAuthority(authorityOverride);
    }

    if (directExecutor) {
      builder.directExecutor();
    } else {
      // TODO(carl-mastrangelo): This should not be necessary.  I don't know where this should be
      // put.  Move it somewhere else, or remove it if no longer necessary.
      // See: https://github.com/grpc/grpc-java/issues/2119
      builder.executor(getExecutor());
    }

    return builder.build();
  }

  /**
   * Save a {@link Histogram} to a file.
   */
  public static void saveHistogram(Histogram histogram, String filename) throws IOException {
    File file;
    PrintStream log = null;
    try {
      file = new File(filename);
      if (file.exists() && !file.delete()) {
        System.err.println("Failed deleting previous histogram file: " + file.getAbsolutePath());
      }
      log = new PrintStream(new FileOutputStream(file), false);
      histogram.outputPercentileDistribution(log, 1.0);
    } finally {
      if (log != null) {
        log.close();
      }
    }
  }

  /**
   * Construct a {@link SimpleResponse} for the given request.
   */
  public static SimpleResponse makeResponse(SimpleRequest request) {
    if (request.getResponseSize() > 0) {
      if (!Messages.PayloadType.COMPRESSABLE.equals(request.getResponseType())) {
        throw Status.INTERNAL.augmentDescription("Error creating payload.").asRuntimeException();
      }

      ByteString body = ByteString.copyFrom(new byte[request.getResponseSize()]);
      Messages.PayloadType type = request.getResponseType();

      Payload payload = Payload.newBuilder().setType(type).setBody(body).build();
      return SimpleResponse.newBuilder().setPayload(payload).build();
    }
    return SimpleResponse.getDefaultInstance();
  }

  /**
   * Construct a {@link SimpleRequest} with the specified dimensions.
   */
  public static SimpleRequest makeRequest(Messages.PayloadType payloadType, int reqLength,
                                          int respLength) {
    ByteString body = ByteString.copyFrom(new byte[reqLength]);
    Payload payload = Payload.newBuilder()
        .setType(payloadType)
        .setBody(body)
        .build();

    return SimpleRequest.newBuilder()
        .setResponseType(payloadType)
        .setResponseSize(respLength)
        .setPayload(payload)
        .build();
  }

  /**
   * Picks a port that is not used right at this moment.
   * Warning: Not thread safe. May see "BindException: Address already in use: bind" if using the
   * returned port to create a new server socket when other threads/processes are concurrently
   * creating new sockets without a specific port.
   */
  public static int pickUnusedPort() {
    try {
      ServerSocket serverSocket = new ServerSocket(0);
      int port = serverSocket.getLocalPort();
      serverSocket.close();
      return port;
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }
}
