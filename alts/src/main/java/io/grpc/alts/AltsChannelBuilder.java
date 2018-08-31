/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts;

import static com.google.common.base.Preconditions.checkArgument;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ExperimentalApi;
import io.grpc.ForwardingChannelBuilder;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.alts.internal.AltsClientOptions;
import io.grpc.alts.internal.AltsProtocolNegotiator;
import io.grpc.alts.internal.AltsTsiHandshaker;
import io.grpc.alts.internal.HandshakerServiceGrpc;
import io.grpc.alts.internal.RpcProtocolVersionsUtil;
import io.grpc.alts.internal.TsiHandshaker;
import io.grpc.alts.internal.TsiHandshakerFactory;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.ObjectPool;
import io.grpc.internal.ProxyParameters;
import io.grpc.internal.SharedResourcePool;
import io.grpc.netty.InternalNettyChannelBuilder;
import io.grpc.netty.InternalNettyChannelBuilder.TransportCreationParamsFilter;
import io.grpc.netty.InternalNettyChannelBuilder.TransportCreationParamsFilterFactory;
import io.grpc.netty.NettyChannelBuilder;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * ALTS version of {@code ManagedChannelBuilder}. This class sets up a secure and authenticated
 * commmunication between two cloud VMs using ALTS.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4151")
public final class AltsChannelBuilder extends ForwardingChannelBuilder<AltsChannelBuilder> {

  private static final Logger logger = Logger.getLogger(AltsChannelBuilder.class.getName());
  private final NettyChannelBuilder delegate;
  private final AltsClientOptions.Builder handshakerOptionsBuilder =
      new AltsClientOptions.Builder();
  private ObjectPool<ManagedChannel> handshakerChannelPool =
      SharedResourcePool.forResource(HandshakerServiceChannel.SHARED_HANDSHAKER_CHANNEL);
  private TcpfFactory tcpfFactoryForTest;
  private boolean enableUntrustedAlts;

  /** "Overrides" the static method in {@link ManagedChannelBuilder}. */
  public static final AltsChannelBuilder forTarget(String target) {
    return new AltsChannelBuilder(target);
  }

  /** "Overrides" the static method in {@link ManagedChannelBuilder}. */
  public static AltsChannelBuilder forAddress(String name, int port) {
    return forTarget(GrpcUtil.authorityFromHostAndPort(name, port));
  }

  private AltsChannelBuilder(String target) {
    delegate =
        NettyChannelBuilder.forTarget(target)
            .keepAliveTime(20, TimeUnit.SECONDS)
            .keepAliveTimeout(10, TimeUnit.SECONDS)
            .keepAliveWithoutCalls(true);
    handshakerOptionsBuilder.setRpcProtocolVersions(
        RpcProtocolVersionsUtil.getRpcProtocolVersions());
  }

  /** The server service account name for secure name checking. */
  public AltsChannelBuilder withSecureNamingTarget(String targetName) {
    handshakerOptionsBuilder.setTargetName(targetName);
    return this;
  }

  /**
   * Adds an expected target service accounts. One of the added service accounts should match peer
   * service account in the handshaker result. Otherwise, the handshake fails.
   */
  public AltsChannelBuilder addTargetServiceAccount(String targetServiceAccount) {
    handshakerOptionsBuilder.addTargetServiceAccount(targetServiceAccount);
    return this;
  }

  /**
   * Enables untrusted ALTS for testing. If this function is called, we will not check whether ALTS
   * is running on Google Cloud Platform.
   */
  public AltsChannelBuilder enableUntrustedAltsForTesting() {
    enableUntrustedAlts = true;
    return this;
  }

  /** Sets a new handshaker service address for testing. */
  public AltsChannelBuilder setHandshakerAddressForTesting(String handshakerAddress) {
    // Instead of using the default shared channel to the handshaker service, create a fix object
    // pool of handshaker service channel for testing.
    handshakerChannelPool =
        HandshakerServiceChannel.getHandshakerChannelPoolForTesting(handshakerAddress);
    return this;
  }

  @Override
  protected NettyChannelBuilder delegate() {
    return delegate;
  }

  @Override
  public ManagedChannel build() {
    if (!CheckGcpEnvironment.isOnGcp()) {
      if (enableUntrustedAlts) {
        logger.log(
            Level.WARNING,
            "Untrusted ALTS mode is enabled and we cannot guarantee the trustworthiness of the "
                + "ALTS handshaker service");
      } else {
        Status status =
            Status.INTERNAL.withDescription("ALTS is only allowed to run on Google Cloud Platform");
        delegate().intercept(new FailingClientInterceptor(status));
      }
    }

    final AltsClientOptions handshakerOptions = handshakerOptionsBuilder.build();
    TsiHandshakerFactory altsHandshakerFactory =
        new TsiHandshakerFactory() {
          @Override
          public TsiHandshaker newHandshaker() {
            // Used the shared grpc channel to connecting to the ALTS handshaker service.
            // TODO: Release the channel if it is not used.
            // https://github.com/grpc/grpc-java/issues/4755.
            return AltsTsiHandshaker.newClient(
                HandshakerServiceGrpc.newStub(handshakerChannelPool.getObject()),
                handshakerOptions);
          }
        };
    AltsProtocolNegotiator negotiator = AltsProtocolNegotiator.create(altsHandshakerFactory);

    TcpfFactory tcpfFactory = new TcpfFactory(handshakerOptions, negotiator);
    InternalNettyChannelBuilder.setDynamicTransportParamsFactory(delegate(), tcpfFactory);
    tcpfFactoryForTest = tcpfFactory;

    return delegate().build();
  }

  @VisibleForTesting
  @Nullable
  TransportCreationParamsFilterFactory getTcpfFactoryForTest() {
    return tcpfFactoryForTest;
  }

  @VisibleForTesting
  @Nullable
  AltsClientOptions getAltsClientOptionsForTest() {
    if (tcpfFactoryForTest == null) {
      return null;
    }
    return tcpfFactoryForTest.handshakerOptions;
  }

  private static final class TcpfFactory implements TransportCreationParamsFilterFactory {

    final AltsClientOptions handshakerOptions;
    private final AltsProtocolNegotiator negotiator;

    public TcpfFactory(AltsClientOptions handshakerOptions, AltsProtocolNegotiator negotiator) {
      this.handshakerOptions = handshakerOptions;
      this.negotiator = negotiator;
    }

    @Override
    public TransportCreationParamsFilter create(
        final SocketAddress serverAddress,
        final String authority,
        final String userAgent,
        final ProxyParameters proxy) {
      checkArgument(
          serverAddress instanceof InetSocketAddress,
          "%s must be a InetSocketAddress",
          serverAddress);
      return new TransportCreationParamsFilter() {
        @Override
        public SocketAddress getTargetServerAddress() {
          return serverAddress;
        }

        @Override
        public String getAuthority() {
          return authority;
        }

        @Override
        public String getUserAgent() {
          return userAgent;
        }

        @Override
        public AltsProtocolNegotiator getProtocolNegotiator() {
          return negotiator;
        }
      };
    }
  }

  /** An implementation of {@link ClientInterceptor} that fails each call. */
  static final class FailingClientInterceptor implements ClientInterceptor {

    private final Status status;

    public FailingClientInterceptor(Status status) {
      this.status = status;
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      return new FailingClientCall<>(status);
    }
  }
}
