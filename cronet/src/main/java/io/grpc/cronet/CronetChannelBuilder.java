/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

package io.grpc.cronet;

import static com.google.common.base.Preconditions.checkArgument;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.Attributes;
import io.grpc.ExperimentalApi;
import io.grpc.NameResolver;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.ConnectionClientTransport;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.ProxyParameters;
import io.grpc.internal.SharedResourceHolder;
import io.grpc.internal.TransportTracer;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import javax.annotation.Nullable;
import org.chromium.net.BidirectionalStream;
import org.chromium.net.CronetEngine;
import org.chromium.net.ExperimentalCronetEngine;

/** Convenience class for building channels with the cronet transport. */
@ExperimentalApi("There is no plan to make this API stable, given transport API instability")
public final class CronetChannelBuilder extends
    AbstractManagedChannelImplBuilder<CronetChannelBuilder> {

  /** BidirectionalStream.Builder factory used for getting the gRPC BidirectionalStream. */
  public static abstract class StreamBuilderFactory {
    public abstract BidirectionalStream.Builder newBidirectionalStreamBuilder(
        String url, BidirectionalStream.Callback callback, Executor executor);
  }

  /** Creates a new builder for the given server host, port and CronetEngine. */
  public static CronetChannelBuilder forAddress(
      String host, int port, final CronetEngine cronetEngine) {
    Preconditions.checkNotNull(cronetEngine, "cronetEngine");
    return new CronetChannelBuilder(
        host,
        port,
        new StreamBuilderFactory() {
          @Override
          public BidirectionalStream.Builder newBidirectionalStreamBuilder(
              String url, BidirectionalStream.Callback callback, Executor executor) {
            return ((ExperimentalCronetEngine) cronetEngine)
                .newBidirectionalStreamBuilder(url, callback, executor);
          }
        });
  }

  /** Creates a new builder for the given server host, port and StreamBuilderFactory. */
  public static CronetChannelBuilder forAddress(
      String host, int port, StreamBuilderFactory streamFactory) {
    return new CronetChannelBuilder(host, port, streamFactory);
  }

  /**
   * Always fails.  Call {@link #forAddress(String, int, CronetEngine)} instead.
   */
  public static CronetChannelBuilder forTarget(String target) {
    throw new UnsupportedOperationException("call forAddress() instead");
  }

  /**
   * Always fails.  Call {@link #forAddress(String, int, CronetEngine)} instead.
   */
  public static CronetChannelBuilder forAddress(String name, int port) {
    throw new UnsupportedOperationException("call forAddress(String, int, CronetEngine) instead");
  }

  private boolean alwaysUsePut = false;

  private int maxMessageSize = DEFAULT_MAX_MESSAGE_SIZE;

  private StreamBuilderFactory streamFactory;

  private CronetChannelBuilder(String host, int port, StreamBuilderFactory streamFactory) {
    super(
        InetSocketAddress.createUnresolved(host, port),
        GrpcUtil.authorityFromHostAndPort(host, port));
    this.streamFactory = Preconditions.checkNotNull(streamFactory, "streamFactory");
  }

  /**
   * Sets the maximum message size allowed to be received on the channel. If not called,
   * defaults to {@link io.grpc.internal.GrpcUtil#DEFAULT_MAX_MESSAGE_SIZE}.
   */
  public final CronetChannelBuilder maxMessageSize(int maxMessageSize) {
    checkArgument(maxMessageSize >= 0, "maxMessageSize must be >= 0");
    this.maxMessageSize = maxMessageSize;
    return this;
  }

  /**
   * Sets the Cronet channel to always use PUT instead of POST. Defaults to false.
   */
  public final CronetChannelBuilder alwaysUsePut(boolean enable) {
    this.alwaysUsePut = enable;
    return this;
  }

  /**
   * Not supported for building cronet channel.
   */
  @Override
  public final CronetChannelBuilder usePlaintext(boolean skipNegotiation) {
    throw new IllegalArgumentException("Plaintext not currently supported");
  }

  @Override
  protected final ClientTransportFactory buildTransportFactory() {
    return new CronetTransportFactory(streamFactory, MoreExecutors.directExecutor(),
        maxMessageSize, alwaysUsePut, transportTracerFactory.create());
  }

  @Override
  protected Attributes getNameResolverParams() {
    return Attributes.newBuilder()
        .set(NameResolver.Factory.PARAMS_DEFAULT_PORT, GrpcUtil.DEFAULT_PORT_SSL).build();
  }

  @VisibleForTesting
  static class CronetTransportFactory implements ClientTransportFactory {
    private final ScheduledExecutorService timeoutService =
        SharedResourceHolder.get(GrpcUtil.TIMER_SERVICE);
    private final Executor executor;
    private final int maxMessageSize;
    private final boolean alwaysUsePut;
    private final StreamBuilderFactory streamFactory;
    private final TransportTracer transportTracer;

    private CronetTransportFactory(
        StreamBuilderFactory streamFactory,
        Executor executor,
        int maxMessageSize,
        boolean alwaysUsePut,
        TransportTracer transportTracer) {
      this.maxMessageSize = maxMessageSize;
      this.alwaysUsePut = alwaysUsePut;
      this.streamFactory = streamFactory;
      this.executor = Preconditions.checkNotNull(executor, "executor");
      this.transportTracer = Preconditions.checkNotNull(transportTracer, "transportTracer");
    }

    @Override
    public ConnectionClientTransport newClientTransport(SocketAddress addr, String authority,
        @Nullable String userAgent, @Nullable ProxyParameters proxy) {
      InetSocketAddress inetSocketAddr = (InetSocketAddress) addr;
      return new CronetClientTransport(streamFactory, inetSocketAddr, authority, userAgent,
          executor, maxMessageSize, alwaysUsePut, transportTracer);
    }

    @Override
    public ScheduledExecutorService getScheduledExecutorService() {
      return timeoutService;
    }

    @Override
    public void close() {
      SharedResourceHolder.release(GrpcUtil.TIMER_SERVICE, timeoutService);
    }
  }
}
