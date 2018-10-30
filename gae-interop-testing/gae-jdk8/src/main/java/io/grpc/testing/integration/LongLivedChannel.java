/*
 * Copyright 2017 The gRPC Authors
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

import com.google.protobuf.ByteString;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.testing.integration.Messages.Payload;
import io.grpc.testing.integration.Messages.SimpleRequest;
import io.grpc.testing.integration.Messages.SimpleResponse;
import java.io.IOException;
import java.util.concurrent.TimeUnit;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

/**
 * A test class that checks we can reuse a channel across requests.
 *
 * <p>This servlet communicates with {@code grpc-test.sandbox.googleapis.com}, which is a server
 * managed by the gRPC team. For more information, see
 * <a href="https://github.com/grpc/grpc/blob/master/doc/interop-test-descriptions.md">
 *   Interoperability Test Case Descriptions</a>.
 */
@SuppressWarnings("serial")
public final class LongLivedChannel extends HttpServlet {
  private static final String INTEROP_TEST_ADDRESS = "grpc-test.sandbox.googleapis.com:443";
  private final ManagedChannel channel =
      ManagedChannelBuilder.forTarget(INTEROP_TEST_ADDRESS).build();

  @Override
  public void destroy() {
    try {
      channel.shutdownNow().awaitTermination(1, TimeUnit.SECONDS);
    } catch (Exception e) {
      throw new RuntimeException(e);
    }
  }

  @Override
  public void doGet(HttpServletRequest req, HttpServletResponse resp) throws IOException {
    int requestSize = 1234;
    int responseSize = 5678;
    SimpleRequest request = SimpleRequest.newBuilder()
        .setResponseSize(responseSize)
        .setPayload(Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[requestSize])))
        .build();
    TestServiceGrpc.TestServiceBlockingStub blockingStub =
            TestServiceGrpc.newBlockingStub(channel);
    SimpleResponse simpleResponse = blockingStub.unaryCall(request);
    resp.setContentType("text/plain");
    if (simpleResponse.getPayload().getBody().size() == responseSize) {
      resp.setStatus(200);
      resp.getWriter().println("PASS!");
    } else {
      resp.setStatus(500);
      resp.getWriter().println("FAILED!");
    }
  }
}
