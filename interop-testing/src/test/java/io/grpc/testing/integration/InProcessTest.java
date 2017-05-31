/*
 * Copyright 2015, gRPC Authors All rights reserved.
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
import org.junit.AfterClass;
import org.junit.BeforeClass;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link io.grpc.inprocess}. */
@RunWith(JUnit4.class)
public class InProcessTest extends AbstractInteropTest {

  private static final String SERVER_NAME = "test";

  /** Starts the in-process server. */
  @BeforeClass
  public static void startServer() {
    startStaticServer(InProcessServerBuilder.forName(SERVER_NAME));
  }

  @AfterClass
  public static void stopServer() {
    stopStaticServer();
  }

  @Override
  protected ManagedChannel createChannel() {
    return InProcessChannelBuilder.forName(SERVER_NAME).build();
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
