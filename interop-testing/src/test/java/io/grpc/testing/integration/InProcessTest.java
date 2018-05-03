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

package io.grpc.testing.integration;

import io.grpc.ManagedChannel;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.AbstractServerImplBuilder;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link io.grpc.inprocess}. */
@RunWith(JUnit4.class)
public class InProcessTest extends AbstractInteropTest {

  private static final String SERVER_NAME = "test";

  @Override
  protected AbstractServerImplBuilder<?> getServerBuilder() {
    // Starts the in-process server.
    return InProcessServerBuilder.forName(SERVER_NAME);
  }

  @Override
  protected ManagedChannel createChannel() {
    InProcessChannelBuilder builder = InProcessChannelBuilder.forName(SERVER_NAME);
    io.grpc.internal.TestingAccessor.setStatsImplementation(
        builder, createClientCensusStatsModule());
    return builder.build();
  }

  @Override
  protected boolean metricsExpected() {
    // TODO(zhangkun83): InProcessTransport by-passes framer and deframer, thus message sizes are
    // not counted. (https://github.com/grpc/grpc-java/issues/2284)
    return false;
  }

  @Override
  public void maxInboundSize_tooBig() {
    // noop, not enforced.
  }

  @Override
  public void maxOutboundSize_tooBig() {
    // noop, not enforced.
  }
}
