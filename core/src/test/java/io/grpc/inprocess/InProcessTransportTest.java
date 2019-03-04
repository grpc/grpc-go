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

package io.grpc.inprocess;

import com.google.common.collect.ImmutableList;
import io.grpc.ServerStreamTracer;
import io.grpc.internal.AbstractTransportTest;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.InternalServer;
import io.grpc.internal.ManagedClientTransport;
import java.util.List;
import org.junit.Ignore;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link InProcessTransport}. */
@RunWith(JUnit4.class)
public class InProcessTransportTest extends AbstractTransportTest {
  private static final String TRANSPORT_NAME = "perfect-for-testing";
  private static final String AUTHORITY = "a-testing-authority";
  private static final String USER_AGENT = "a-testing-user-agent";

  @Override
  protected List<? extends InternalServer> newServer(
      List<ServerStreamTracer.Factory> streamTracerFactories) {
    InProcessServerBuilder builder = InProcessServerBuilder
        .forName(TRANSPORT_NAME)
        .maxInboundMetadataSize(GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE);
    return ImmutableList.of(new InProcessServer(builder, streamTracerFactories));
  }

  @Override
  protected List<? extends InternalServer> newServer(
      int port, List<ServerStreamTracer.Factory> streamTracerFactories) {
    return newServer(streamTracerFactories);
  }

  @Override
  protected String testAuthority(InternalServer server) {
    return AUTHORITY;
  }

  @Override
  protected ManagedClientTransport newClientTransport(InternalServer server) {
    return new InProcessTransport(
        TRANSPORT_NAME, GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE, testAuthority(server), USER_AGENT,
        eagAttrs());
  }

  @Override
  protected boolean sizesReported() {
    // TODO(zhangkun83): InProcessTransport doesn't record metrics for now
    // (https://github.com/grpc/grpc-java/issues/2284)
    return false;
  }

  @Test
  @Ignore
  @Override
  public void socketStats() throws Exception {
    // test does not apply to in-process
  }
}
