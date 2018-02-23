/*
 * Copyright 2018, gRPC Authors All rights reserved.
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
import io.grpc.ClientCall;
import io.grpc.ConnectivityState;
import io.grpc.ForwardingChannelBuilder;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.MethodDescriptor;
import io.grpc.alts.transportsecurity.AltsClientOptions;
import io.grpc.alts.transportsecurity.AltsTsiHandshaker;
import io.grpc.alts.transportsecurity.TsiHandshaker;
import io.grpc.alts.transportsecurity.TsiHandshakerFactory;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.ProxyParameters;
import io.grpc.netty.InternalNettyChannelBuilder;
import io.grpc.netty.InternalNettyChannelBuilder.TransportCreationParamsFilter;
import io.grpc.netty.InternalNettyChannelBuilder.TransportCreationParamsFilterFactory;
import io.grpc.netty.NettyChannelBuilder;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;

/**
 * ALTS version of {@code ManagedChannelBuilder}. This class sets up a secure and authenticated
 * commmunication between two cloud VMs using ALTS.
 */
public final class AltsChannelBuilder extends ForwardingChannelBuilder<AltsChannelBuilder> {

  private final NettyChannelBuilder delegate;
  private final AltsClientOptions.Builder handshakerOptionsBuilder =
      new AltsClientOptions.Builder();
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
    HandshakerServiceChannel.setHandshakerAddressForTesting(handshakerAddress);
    return this;
  }

  @Override
  protected NettyChannelBuilder delegate() {
    return delegate;
  }

  @Override
  public ManagedChannel build() {
    CheckGcpEnvironment.check(enableUntrustedAlts);
    TcpfFactory tcpfFactory = new TcpfFactory();
    InternalNettyChannelBuilder.setDynamicTransportParamsFactory(delegate(), tcpfFactory);

    tcpfFactoryForTest = tcpfFactory;

    return new AltsChannel(delegate().build());
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

  private final class TcpfFactory implements TransportCreationParamsFilterFactory {
    final AltsClientOptions handshakerOptions = handshakerOptionsBuilder.build();

    private final TsiHandshakerFactory altsHandshakerFactory =
        new TsiHandshakerFactory() {
          @Override
          public TsiHandshaker newHandshaker() {
            // Used the shared grpc channel to connecting to the ALTS handshaker service.
            ManagedChannel channel = HandshakerServiceChannel.get();
            return AltsTsiHandshaker.newClient(
                HandshakerServiceGrpc.newStub(channel), handshakerOptions);
          }
        };

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
      final AltsProtocolNegotiator negotiator =
          AltsProtocolNegotiator.create(altsHandshakerFactory);
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

  static final class AltsChannel extends ManagedChannel {
    private final ManagedChannel delegate;

    AltsChannel(ManagedChannel delegate) {
      this.delegate = delegate;
    }

    @Override
    public ConnectivityState getState(boolean requestConnection) {
      return delegate.getState(requestConnection);
    }

    @Override
    public void notifyWhenStateChanged(ConnectivityState source, Runnable callback) {
      delegate.notifyWhenStateChanged(source, callback);
    }

    @Override
    public AltsChannel shutdown() {
      delegate.shutdown();
      return this;
    }

    @Override
    public boolean isShutdown() {
      return delegate.isShutdown();
    }

    @Override
    public boolean isTerminated() {
      return delegate.isTerminated();
    }

    @Override
    public AltsChannel shutdownNow() {
      delegate.shutdownNow();
      return this;
    }

    @Override
    public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException {
      return delegate.awaitTermination(timeout, unit);
    }

    @Override
    public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
        MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions) {
      return delegate.newCall(methodDescriptor, callOptions);
    }

    @Override
    public String authority() {
      return delegate.authority();
    }

    @Override
    public void resetConnectBackoff() {
      delegate.resetConnectBackoff();
    }

    @Override
    public void prepareToLoseNetwork() {
      delegate.prepareToLoseNetwork();
    }
  }
}
