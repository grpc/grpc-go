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

import com.google.auth.oauth2.GoogleCredentials;
import com.google.common.annotations.VisibleForTesting;
import io.grpc.CallCredentials;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ForwardingChannelBuilder;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.alts.internal.AltsClientOptions;
import io.grpc.alts.internal.AltsTsiHandshaker;
import io.grpc.alts.internal.GoogleDefaultProtocolNegotiator;
import io.grpc.alts.internal.HandshakerServiceGrpc;
import io.grpc.alts.internal.RpcProtocolVersionsUtil;
import io.grpc.alts.internal.TsiHandshaker;
import io.grpc.alts.internal.TsiHandshakerFactory;
import io.grpc.auth.MoreCallCredentials;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.ProxyParameters;
import io.grpc.internal.SharedResourceHolder;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.InternalNettyChannelBuilder;
import io.grpc.netty.InternalNettyChannelBuilder.TransportCreationParamsFilter;
import io.grpc.netty.InternalNettyChannelBuilder.TransportCreationParamsFilterFactory;
import io.grpc.netty.NettyChannelBuilder;
import io.netty.handler.ssl.SslContext;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import javax.annotation.Nullable;
import javax.net.ssl.SSLException;

/**
 * Google default version of {@code ManagedChannelBuilder}. This class sets up a secure channel
 * using ALTS if applicable and using TLS as fallback.
 */
public final class GoogleDefaultChannelBuilder
    extends ForwardingChannelBuilder<GoogleDefaultChannelBuilder> {

  private final NettyChannelBuilder delegate;
  private final TcpfFactory tcpfFactory;

  private GoogleDefaultChannelBuilder(String target) {
    delegate = NettyChannelBuilder.forTarget(target);

    final AltsClientOptions handshakerOptions =
        new AltsClientOptions.Builder()
            .setRpcProtocolVersions(RpcProtocolVersionsUtil.getRpcProtocolVersions())
            .build();
    TsiHandshakerFactory altsHandshakerFactory =
        new TsiHandshakerFactory() {
          @Override
          public TsiHandshaker newHandshaker() {
            // Used the shared grpc channel to connecting to the ALTS handshaker service.
            // TODO: Release the channel if it is not used.
            // https://github.com/grpc/grpc-java/issues/4755.
            ManagedChannel channel =
                SharedResourceHolder.get(HandshakerServiceChannel.SHARED_HANDSHAKER_CHANNEL);
            return AltsTsiHandshaker.newClient(
                HandshakerServiceGrpc.newStub(channel), handshakerOptions);
          }
        };
    SslContext sslContext;
    try {
      sslContext = GrpcSslContexts.forClient().build();
    } catch (SSLException ex) {
      throw new RuntimeException(ex);
    }
    tcpfFactory = new TcpfFactory(
        new GoogleDefaultProtocolNegotiator(altsHandshakerFactory, sslContext));
    InternalNettyChannelBuilder.setDynamicTransportParamsFactory(delegate(), tcpfFactory);
  }

  /** "Overrides" the static method in {@link ManagedChannelBuilder}. */
  public static final GoogleDefaultChannelBuilder forTarget(String target) {
    return new GoogleDefaultChannelBuilder(target);
  }

  /** "Overrides" the static method in {@link ManagedChannelBuilder}. */
  public static GoogleDefaultChannelBuilder forAddress(String name, int port) {
    return forTarget(GrpcUtil.authorityFromHostAndPort(name, port));
  }

  @Override
  protected NettyChannelBuilder delegate() {
    return delegate;
  }

  @Override
  public ManagedChannel build() {
    @Nullable CallCredentials credentials = null;
    Status status = Status.OK;
    try {
      credentials = MoreCallCredentials.from(GoogleCredentials.getApplicationDefault());
    } catch (IOException e) {
      status =
          Status.UNAUTHENTICATED
              .withDescription("Failed to get Google default credentials")
              .withCause(e);
    }
    return delegate().intercept(new GoogleDefaultInterceptor(credentials, status)).build();
  }

  @VisibleForTesting
  TransportCreationParamsFilterFactory getTcpfFactoryForTest() {
    return tcpfFactory;
  }

  private static final class TcpfFactory implements TransportCreationParamsFilterFactory {
    private final GoogleDefaultProtocolNegotiator negotiator;

    private TcpfFactory(GoogleDefaultProtocolNegotiator negotiator) {
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
        public GoogleDefaultProtocolNegotiator getProtocolNegotiator() {
          return negotiator;
        }
      };
    }
  }

  /**
   * An implementation of {@link ClientInterceptor} that adds Google call credentials on each call.
   */
  static final class GoogleDefaultInterceptor implements ClientInterceptor {

    @Nullable private final CallCredentials credentials;
    private final Status status;

    public GoogleDefaultInterceptor(@Nullable CallCredentials credentials, Status status) {
      this.credentials = credentials;
      this.status = status;
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
      if (!status.isOk()) {
        return new FailingClientCall<>(status);
      }
      return next.newCall(method, callOptions.withCallCredentials(credentials));
    }
  }
}
