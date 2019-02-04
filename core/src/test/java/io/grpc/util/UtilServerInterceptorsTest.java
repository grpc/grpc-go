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
  private static class VoidCallListener extends ServerCall.Listener<Void> {
    public void onCall(ServerCall<Void, Void> call, Metadata headers) { }
  }

  private MethodDescriptor<Void, Void> flowMethod = TestMethodDescriptors.voidMethod();
  private final Metadata headers = new Metadata();
  private ServerCallHandler<Void, Void> handler = new ServerCallHandler<Void, Void>() {
      @Override
      public ServerCall.Listener<Void> startCall(
          ServerCall<Void, Void> call, Metadata headers) {
        listener.onCall(call, headers);
        return listener;
      }
  };
  private ServerServiceDefinition serviceDefinition =
      ServerServiceDefinition.builder(new ServiceDescriptor("service_foo", flowMethod))
          .addMethod(flowMethod, handler)
          .build();
  private VoidCallListener listener;

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
        new FakeServerCall<>(expectedStatus, expectedMetadata);
    final StatusRuntimeException exception =
        new StatusRuntimeException(expectedStatus, expectedMetadata);
    listener = new VoidCallListener() {
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

  @Test
  public void statusRuntimeExceptionTransmitterIgnoresClosedCalls() {
    final Status expectedStatus = Status.UNAVAILABLE;
    final Status unexpectedStatus = Status.CANCELLED;
    final Metadata expectedMetadata = new Metadata();

    FakeServerCall<Void, Void> call =
        new FakeServerCall<>(expectedStatus, expectedMetadata);
    final StatusRuntimeException exception =
        new StatusRuntimeException(expectedStatus, expectedMetadata);

    listener = new VoidCallListener() {
      @Override
      public void onMessage(Void message) {
        throw exception;
      }

      @Override
      public void onHalfClose() {
        throw exception;
      }
    };

    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition,
        Arrays.asList(TransmitStatusRuntimeExceptionInterceptor.instance()));
    ServerCall.Listener<Void> callDoubleSreListener =
        getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers);
    callDoubleSreListener.onMessage(null); // the only close with our exception
    callDoubleSreListener.onHalfClose(); // should not trigger a close

    // this listener closes the call when it is initialized with startCall
    listener = new VoidCallListener() {
      @Override
      public void onCall(ServerCall<Void, Void> call, Metadata headers) {
        call.close(unexpectedStatus, headers);
      }

      @Override
      public void onHalfClose() {
        throw exception;
      }
    };

    ServerCall.Listener<Void> callClosedListener =
        getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers);
    // call is already closed, does not match exception
    callClosedListener.onHalfClose(); // should not trigger a close
    assertEquals(1, call.numCloses);
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
