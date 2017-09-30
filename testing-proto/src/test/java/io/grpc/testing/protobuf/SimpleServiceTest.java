/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

package io.grpc.testing.protobuf;

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING;
import static io.grpc.MethodDescriptor.MethodType.CLIENT_STREAMING;
import static io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING;
import static io.grpc.MethodDescriptor.MethodType.UNARY;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;

import io.grpc.MethodDescriptor;
import io.opencensus.trace.Tracing;
import io.opencensus.trace.export.SampledSpanStore;
import java.util.ArrayList;
import java.util.Set;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Test to verify that the proto file simpleservice.proto generates the expected service. */
@RunWith(JUnit4.class)
public class SimpleServiceTest {
  @Test
  public void serviceDescriptor() {
    assertEquals("grpc.testing.SimpleService", SimpleServiceGrpc.getServiceDescriptor().getName());
  }

  @Test
  public void serviceMethodDescriotrs() {
    MethodDescriptor<SimpleRequest, SimpleResponse> genericTypeShouldMatchWhenAssigned;

    genericTypeShouldMatchWhenAssigned = SimpleServiceGrpc.METHOD_UNARY_RPC;
    assertEquals(UNARY, genericTypeShouldMatchWhenAssigned.getType());

    genericTypeShouldMatchWhenAssigned = SimpleServiceGrpc.METHOD_CLIENT_STREAMING_RPC;
    assertEquals(CLIENT_STREAMING, genericTypeShouldMatchWhenAssigned.getType());

    genericTypeShouldMatchWhenAssigned = SimpleServiceGrpc.METHOD_SERVER_STREAMING_RPC;
    assertEquals(SERVER_STREAMING, genericTypeShouldMatchWhenAssigned.getType());

    genericTypeShouldMatchWhenAssigned = SimpleServiceGrpc.METHOD_BIDI_STREAMING_RPC;
    assertEquals(BIDI_STREAMING, genericTypeShouldMatchWhenAssigned.getType());
  }

  @Test
  public void registerSampledMethodsForTracing() throws Exception {
    // Make sure SimpleServiceGrpc and CensusTracingModule classes are loaded.
    assertNotNull(Class.forName(SimpleServiceGrpc.class.getName()));
    assertNotNull(Class.forName("io.grpc.internal.CensusTracingModule"));

    String[] methodNames = new String[] {
      "grpc.testing.SimpleService/UnaryRpc",
      "grpc.testing.SimpleService/ClientStreamingRpc",
      "grpc.testing.SimpleService/ServerStreamingRpc",
      "grpc.testing.SimpleService/BidiStreamingRpc"};

    ArrayList<String> expectedSpans = new ArrayList<String>();
    for (String methodName : methodNames) {
      expectedSpans.add(generateTraceSpanName(false, methodName));
      expectedSpans.add(generateTraceSpanName(true, methodName));
    }

    SampledSpanStore sampledStore = Tracing.getExportComponent().getSampledSpanStore();
    Set<String> registeredSpans = sampledStore.getRegisteredSpanNamesForCollection();
    assertThat(registeredSpans).containsAllIn(expectedSpans);
  }

  /**
   * Copy of {@link io.grpc.internal.CensusTracingModule#generateTraceSpanName} to break dependency.
   */
  private static String generateTraceSpanName(boolean isServer, String fullMethodName) {
    String prefix = isServer ? "Recv" : "Sent";
    return prefix + "." + fullMethodName.replace('/', '.');
  }
}
