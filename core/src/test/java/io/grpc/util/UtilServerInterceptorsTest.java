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

package io.grpc.util;

import static com.google.common.collect.Iterables.getOnlyElement;
import static org.junit.Assert.assertEquals;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptors;
import io.grpc.ServerMethodDefinition;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServiceDescriptor;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.internal.NoopServerCall;
import io.grpc.testing.TestMethodDescriptors;
import java.util.Arrays;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit test for {@link io.grpc.ServerInterceptor} implementations that come with gRPC. Not to be
 * confused with the unit tests that validate gRPC's usage of interceptors.
 */
@RunWith(JUnit4.class)
public class UtilServerInterceptorsTest {
  private MethodDescriptor<Void, Void> flowMethod = TestMethodDescriptors.voidMethod();
  private final Metadata headers = new Metadata();
  private ServerCallHandler<Void, Void> handler = new ServerCallHandler<Void, Void>() {
      @Override
      public ServerCall.Listener<Void> startCall(
          ServerCall<Void, Void> call, Metadata headers) {
        return listener;
      }
  };
  private ServerServiceDefinition serviceDefinition =
      ServerServiceDefinition.builder(new ServiceDescriptor("service_foo", flowMethod))
          .addMethod(flowMethod, handler)
          .build();
  private ServerCall.Listener<Void> listener;

  @SuppressWarnings("unchecked")
  private static ServerMethodDefinition<Void, Void> getSoleMethod(
      ServerServiceDefinition serviceDef) {
    if (serviceDef.getMethods().size() != 1) {
      throw new AssertionError("Not exactly one method present");
    }
    return (ServerMethodDefinition<Void, Void>) getOnlyElement(serviceDef.getMethods());
  }

  @Test
  public void statusRuntimeExceptionTransmitter() {
    final Status expectedStatus = Status.UNAVAILABLE;
    final Metadata expectedMetadata = new Metadata();
    FakeServerCall<Void, Void> call =
        new FakeServerCall<Void, Void>(expectedStatus, expectedMetadata);
    final StatusRuntimeException exception =
        new StatusRuntimeException(expectedStatus, expectedMetadata);
    listener = new ServerCall.Listener<Void>() {
      @Override
      public void onMessage(Void message) {
        throw exception;
      }

      @Override
      public void onHalfClose() {
        throw exception;
      }

      @Override
      public void onCancel() {
        throw exception;
      }

      @Override
      public void onComplete() {
        throw exception;
      }

      @Override
      public void onReady() {
        throw exception;
      }
    };

    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition,
        Arrays.asList(TransmitStatusRuntimeExceptionInterceptor.instance()));
    // The interceptor should have handled the error by directly closing the ServerCall
    // and the exception should not propagate to the method's caller
    getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers).onMessage(null);
    getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers).onCancel();
    getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers).onComplete();
    getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers).onHalfClose();
    getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers).onReady();
    assertEquals(5, call.numCloses);
  }

  private static class FakeServerCall<ReqT, RespT> extends NoopServerCall<ReqT, RespT> {
    final Status expectedStatus;
    final Metadata expectedMetadata;

    int numCloses;

    FakeServerCall(Status expectedStatus, Metadata expectedMetadata) {
      this.expectedStatus = expectedStatus;
      this.expectedMetadata = expectedMetadata;
    }

    @Override
    @SuppressWarnings("ReferenceEquality")
    public void close(Status status, Metadata trailers) {
      if (status == expectedStatus && trailers == expectedMetadata) {
        numCloses++;
      }
    }
  }
}
