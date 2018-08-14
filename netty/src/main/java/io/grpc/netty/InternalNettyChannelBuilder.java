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

package io.grpc.netty;

import io.grpc.Internal;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.ProxyParameters;
import java.net.SocketAddress;

/**
 * Internal {@link NettyChannelBuilder} accessor.  This is intended for usage internal to the gRPC
 * team.  If you *really* think you need to use this, contact the gRPC team first.
 */
@Internal
public final class InternalNettyChannelBuilder {

  /**
   * Checks authority upon channel construction.  The purpose of this interface is to raise the
   * visibility of {@link NettyChannelBuilder.OverrideAuthorityChecker}.
   */
  public interface OverrideAuthorityChecker extends NettyChannelBuilder.OverrideAuthorityChecker {}

  public static void overrideAuthorityChecker(
      NettyChannelBuilder channelBuilder, OverrideAuthorityChecker authorityChecker) {
    channelBuilder.overrideAuthorityChecker(authorityChecker);
  }

  /**
   * Interface to create netty dynamic parameters.
   */
  public interface TransportCreationParamsFilterFactory
      extends NettyChannelBuilder.TransportCreationParamsFilterFactory {
    @Override
    TransportCreationParamsFilter create(
        SocketAddress targetServerAddress, String authority, String userAgent,
        ProxyParameters proxy);
  }

  /**
   * {@link TransportCreationParamsFilter} are those that may depend on late-known information about
   * a client transport.  This interface can be used to dynamically alter params based on the
   * params of {@code ClientTransportFactory#newClientTransport}.
   */
  public interface TransportCreationParamsFilter
      extends NettyChannelBuilder.TransportCreationParamsFilter {}

  public static void setDynamicTransportParamsFactory(
      NettyChannelBuilder builder, TransportCreationParamsFilterFactory factory) {
    builder.setDynamicParamsFactory(factory);
  }

  public static void setStatsEnabled(NettyChannelBuilder builder, boolean value) {
    builder.setStatsEnabled(value);
  }

  public static void setTracingEnabled(NettyChannelBuilder builder, boolean value) {
    builder.setTracingEnabled(value);
  }

  public static void setStatsRecordStartedRpcs(NettyChannelBuilder builder, boolean value) {
    builder.setStatsRecordStartedRpcs(value);
  }

  public static ClientTransportFactory buildTransportFactory(NettyChannelBuilder builder) {
    return builder.buildTransportFactory();
  }

  private InternalNettyChannelBuilder() {}
}
