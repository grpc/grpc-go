/*
 * Copyright 2016 The gRPC Authors
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
import static com.google.common.base.Preconditions.checkNotNull;
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
import org.chromium.net.ExperimentalBidirectionalStream;
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
  public static CronetChannelBuilder forAddress(String host, int port, CronetEngine cronetEngine) {
    Preconditions.checkNotNull(cronetEngine, "cronetEngine");
    return new CronetChannelBuilder(host, port, cronetEngine);
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

  @Nullable
  private ScheduledExecutorService scheduledExecutorService;

  private final CronetEngine cronetEngine;

  private boolean alwaysUsePut = false;

  private int maxMessageSize = DEFAULT_MAX_MESSAGE_SIZE;

  private boolean trafficStatsTagSet;
  private int trafficStatsTag;
  private boolean trafficStatsUidSet;
  private int trafficStatsUid;

  private CronetChannelBuilder(String host, int port, CronetEngine cronetEngine) {
    super(
        InetSocketAddress.createUnresolved(host, port),
        GrpcUtil.authorityFromHostAndPort(host, port));
    this.cronetEngine = Preconditions.checkNotNull(cronetEngine, "cronetEngine");
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

  /**
   * Sets {@link android.net.TrafficStats} tag to use when accounting socket traffic caused by this
   * channel. See {@link android.net.TrafficStats} for more information. If no tag is set (e.g. this
   * method isn't called), then Android accounts for the socket traffic caused by this channel as if
   * the tag value were set to 0.
   *
   * <p><b>NOTE:</b>Setting a tag disallows sharing of sockets with channels with other tags, which
   * may adversely effect performance by prohibiting connection sharing. In other words use of
   * multiplexed sockets (e.g. HTTP/2 and QUIC) will only be allowed if all channels have the same
   * socket tag.
   *
   * @param tag the tag value used to when accounting for socket traffic caused by this channel.
   *     Tags between 0xFFFFFF00 and 0xFFFFFFFF are reserved and used internally by system services
   *     like {@link android.app.DownloadManager} when performing traffic on behalf of an
   *     application.
   * @return the builder to facilitate chaining.
   */
  public final CronetChannelBuilder setTrafficStatsTag(int tag) {
    trafficStatsTagSet = true;
    trafficStatsTag = tag;
    return this;
  }

  /**
   * Sets specific UID to use when accounting socket traffic caused by this channel. See {@link
   * android.net.TrafficStats} for more information. Designed for use when performing an operation
   * on behalf of another application. Caller must hold {@link
   * android.Manifest.permission#MODIFY_NETWORK_ACCOUNTING} permission. By default traffic is
   * attributed to UID of caller.
   *
   * <p><b>NOTE:</b>Setting a UID disallows sharing of sockets with channels with other UIDs, which
   * may adversely effect performance by prohibiting connection sharing. In other words use of
   * multiplexed sockets (e.g. HTTP/2 and QUIC) will only be allowed if all channels have the same
   * UID set.
   *
   * @param uid the UID to attribute socket traffic caused by this channel.
   * @return the builder to facilitate chaining.
   */
  public final CronetChannelBuilder setTrafficStatsUid(int uid) {
    trafficStatsUidSet = true;
    trafficStatsUid = uid;
    return this;
  }

  /**
   * Provides a custom scheduled executor service.
   *
   * <p>It's an optional parameter. If the user has not provided a scheduled executor service when
   * the channel is built, the builder will use a static cached thread pool.
   *
   * @return this
   *
   * @since 1.12.0
   */
  public final CronetChannelBuilder scheduledExecutorService(
      ScheduledExecutorService scheduledExecutorService) {
    this.scheduledExecutorService =
        checkNotNull(scheduledExecutorService, "scheduledExecutorService");
    return this;
  }

  @Override
  protected final ClientTransportFactory buildTransportFactory() {
    return new CronetTransportFactory(
        new TaggingStreamFactory(
            cronetEngine, trafficStatsTagSet, trafficStatsTag, trafficStatsUidSet, trafficStatsUid),
        MoreExecutors.directExecutor(),
        scheduledExecutorService,
        maxMessageSize,
        alwaysUsePut,
        transportTracerFactory.create());
  }

  @Override
  protected Attributes getNameResolverParams() {
    return Attributes.newBuilder()
        .set(NameResolver.Factory.PARAMS_DEFAULT_PORT, GrpcUtil.DEFAULT_PORT_SSL).build();
  }

  @VisibleForTesting
  static class CronetTransportFactory implements ClientTransportFactory {
    private final ScheduledExecutorService timeoutService;
    private final Executor executor;
    private final int maxMessageSize;
    private final boolean alwaysUsePut;
    private final StreamBuilderFactory streamFactory;
    private final TransportTracer transportTracer;
    private final boolean usingSharedScheduler;

    private CronetTransportFactory(
        StreamBuilderFactory streamFactory,
        Executor executor,
        @Nullable ScheduledExecutorService timeoutService,
        int maxMessageSize,
        boolean alwaysUsePut,
        TransportTracer transportTracer) {
      usingSharedScheduler = timeoutService == null;
      this.timeoutService = usingSharedScheduler
          ? SharedResourceHolder.get(GrpcUtil.TIMER_SERVICE) : timeoutService;
      this.maxMessageSize = maxMessageSize;
      this.alwaysUsePut = alwaysUsePut;
      this.streamFactory = streamFactory;
      this.executor = Preconditions.checkNotNull(executor, "executor");
      this.transportTracer = Preconditions.checkNotNull(transportTracer, "transportTracer");
    }

    @Override
    public ConnectionClientTransport newClientTransport(
        SocketAddress addr, ClientTransportOptions options) {
      InetSocketAddress inetSocketAddr = (InetSocketAddress) addr;
      return new CronetClientTransport(streamFactory, inetSocketAddr, options.getAuthority(),
          options.getUserAgent(), executor, maxMessageSize, alwaysUsePut, transportTracer);
    }

    @Override
    public ScheduledExecutorService getScheduledExecutorService() {
      return timeoutService;
    }

    @Override
    public void close() {
      if (usingSharedScheduler) {
        SharedResourceHolder.release(GrpcUtil.TIMER_SERVICE, timeoutService);
      }
    }
  }

  /**
   * StreamBuilderFactory impl that applies TrafficStats tags to stream builders that are produced.
   */
  private static class TaggingStreamFactory extends StreamBuilderFactory {
    private final CronetEngine cronetEngine;
    private final boolean trafficStatsTagSet;
    private final int trafficStatsTag;
    private final boolean trafficStatsUidSet;
    private final int trafficStatsUid;

    TaggingStreamFactory(
        CronetEngine cronetEngine,
        boolean trafficStatsTagSet,
        int trafficStatsTag,
        boolean trafficStatsUidSet,
        int trafficStatsUid) {
      this.cronetEngine = cronetEngine;
      this.trafficStatsTagSet = trafficStatsTagSet;
      this.trafficStatsTag = trafficStatsTag;
      this.trafficStatsUidSet = trafficStatsUidSet;
      this.trafficStatsUid = trafficStatsUid;
    }

    @Override
    public BidirectionalStream.Builder newBidirectionalStreamBuilder(
        String url, BidirectionalStream.Callback callback, Executor executor) {
      ExperimentalBidirectionalStream.Builder builder =
          ((ExperimentalCronetEngine) cronetEngine)
              .newBidirectionalStreamBuilder(url, callback, executor);
      if (trafficStatsTagSet) {
        builder.setTrafficStatsTag(trafficStatsTag);
      }
      if (trafficStatsUidSet) {
        builder.setTrafficStatsUid(trafficStatsUid);
      }
      return builder;
    }
  }
}
