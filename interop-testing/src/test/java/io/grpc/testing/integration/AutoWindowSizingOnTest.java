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

package io.grpc.testing.integration;

import io.grpc.ManagedChannel;
import io.grpc.internal.AbstractServerImplBuilder;
import io.grpc.netty.InternalHandlerSettings;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import org.junit.BeforeClass;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class AutoWindowSizingOnTest extends AbstractInteropTest {

  @BeforeClass
  public static void turnOnAutoWindow() {
    InternalHandlerSettings.enable(true);
    InternalHandlerSettings.autoWindowOn(true);
  }

  @Override
  protected AbstractServerImplBuilder<?> getServerBuilder() {
    return NettyServerBuilder.forPort(0)
        .maxInboundMessageSize(AbstractInteropTest.MAX_MESSAGE_SIZE);
  }

  @Override
  protected ManagedChannel createChannel() {
    NettyChannelBuilder builder = NettyChannelBuilder.forAddress("localhost", getPort())
        .negotiationType(NegotiationType.PLAINTEXT)
        .maxInboundMessageSize(AbstractInteropTest.MAX_MESSAGE_SIZE);
    io.grpc.internal.TestingAccessor.setStatsImplementation(
        builder, createClientCensusStatsModule());
    return builder.build();
  }
}
