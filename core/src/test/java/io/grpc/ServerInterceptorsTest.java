/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import io.grpc.Metadata.Headers;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/** Unit tests for {@link ServerInterceptors}. */
@RunWith(JUnit4.class)
public class ServerInterceptorsTest {
  @SuppressWarnings("unchecked")
  private Marshaller<String> requestMarshaller = mock(Marshaller.class);
  @SuppressWarnings("unchecked")
  private Marshaller<Integer> responseMarshaller = mock(Marshaller.class);
  @SuppressWarnings("unchecked")
  private ServerCallHandler<String, Integer> handler = mock(ServerCallHandler.class);
  @Mock private ServerCall.Listener<String> listener;
  private String methodName = "/someRandom.Name";
  @Mock private ServerCall<Integer> call;
  private ServerServiceDefinition serviceDefinition = ServerServiceDefinition.builder("basic")
      .addMethod("flow", requestMarshaller, responseMarshaller, handler).build();
  private Headers headers = new Headers();

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    Mockito.when(handler.startCall(
          Mockito.<String>any(), Mockito.<ServerCall<Integer>>any(), Mockito.<Headers>any()))
        .thenReturn(listener);
  }

  @After
  public void makeSureExpectedMocksUnused() {
    verifyZeroInteractions(requestMarshaller);
    verifyZeroInteractions(responseMarshaller);
    verifyZeroInteractions(listener);
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullServiceDefinition() {
    ServerInterceptors.intercept(null, Arrays.<ServerInterceptor>asList());
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullInterceptorList() {
    ServerInterceptors.intercept(serviceDefinition, (List<ServerInterceptor>) null);
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullInterceptor() {
    ServerInterceptors.intercept(serviceDefinition, Arrays.asList((ServerInterceptor) null));
  }

  @Test
  public void noop() {
    assertSame(serviceDefinition,
        ServerInterceptors.intercept(serviceDefinition, Arrays.<ServerInterceptor>asList()));
  }

  @Test
  public void multipleInvocationsOfHandler() {
    ServerInterceptor interceptor = Mockito.spy(new NoopInterceptor());
    ServerServiceDefinition intercepted
        = ServerInterceptors.intercept(serviceDefinition, Arrays.asList(interceptor));
    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(methodName, call, headers));
    verify(interceptor).interceptCall(
        same(methodName), same(call), same(headers), anyCallHandler());
    verify(handler).startCall(methodName, call, headers);
    verifyNoMoreInteractions(interceptor, handler);

    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(methodName, call, headers));
    verify(interceptor, times(2))
        .interceptCall(same(methodName), same(call), same(headers), anyCallHandler());
    verify(handler, times(2)).startCall(methodName, call, headers);
    verifyNoMoreInteractions(interceptor, handler);
  }

  @Test
  public void correctHandlerCalled() {
    @SuppressWarnings("unchecked")
    ServerCallHandler<String, Integer> handler2 = mock(ServerCallHandler.class);
    serviceDefinition = ServerServiceDefinition.builder("basic")
        .addMethod("flow", requestMarshaller, responseMarshaller, handler)
        .addMethod("flow2", requestMarshaller, responseMarshaller, handler2).build();
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.<ServerInterceptor>asList(new NoopInterceptor()));
    getMethod(intercepted, "flow").getServerCallHandler().startCall(methodName, call, headers);
    verify(handler).startCall(methodName, call, headers);
    verifyNoMoreInteractions(handler);
    verifyZeroInteractions(handler2);

    getMethod(intercepted, "flow2").getServerCallHandler().startCall(methodName, call, headers);
    verify(handler2).startCall(methodName, call, headers);
    verifyNoMoreInteractions(handler);
    verifyNoMoreInteractions(handler2);
  }

  @Test(expected = IllegalStateException.class)
  public void callNextTwice() {
    ServerInterceptor interceptor = new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(String method,
          ServerCall<RespT> call, Headers headers, ServerCallHandler<ReqT, RespT> next) {
        next.startCall(method, call, headers);
        return next.startCall(method, call, headers);
      }
    };
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(serviceDefinition,
        interceptor);
    getSoleMethod(intercepted).getServerCallHandler().startCall(methodName, call, headers);
  }

  @Test
  public void ordered() {
    final List<String> order = new ArrayList<String>();
    handler = new ServerCallHandler<String, Integer>() {
          @Override
          public ServerCall.Listener<String> startCall(String method, ServerCall<Integer> call,
              Headers headers) {
            order.add("handler");
            return listener;
          }
        };
    ServerInterceptor interceptor1 = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(String method,
              ServerCall<RespT> call, Headers headers, ServerCallHandler<ReqT, RespT> next) {
            order.add("i1");
            return next.startCall(method, call, headers);
          }
        };
    ServerInterceptor interceptor2 = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(String method,
              ServerCall<RespT> call, Headers headers, ServerCallHandler<ReqT, RespT> next) {
            order.add("i2");
            return next.startCall(method, call, headers);
          }
        };
    ServerServiceDefinition serviceDefinition = ServerServiceDefinition.builder("basic")
        .addMethod("flow", requestMarshaller, responseMarshaller, handler).build();
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor1, interceptor2));
    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(methodName, call, headers));
    assertEquals(Arrays.asList("i1", "i2", "handler"), order);
  }

  @Test
  public void argumentsPassed() {
    final String method2 = "/someOtherRandom.Method";
    @SuppressWarnings("unchecked")
    final ServerCall<Integer> call2 = mock(ServerCall.class);
    @SuppressWarnings("unchecked")
    final ServerCall.Listener<String> listener2 = mock(ServerCall.Listener.class);
    ServerInterceptor interceptor = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(String method,
              ServerCall<RespT> call, Headers headers, ServerCallHandler<ReqT, RespT> next) {
            assertSame(method, methodName);
            assertSame(call, ServerInterceptorsTest.this.call);
            @SuppressWarnings("unchecked")
            ServerCall<RespT> call2Typed = (ServerCall<RespT>) call2;
            assertSame(listener, next.startCall(method2, call2Typed, headers));
            @SuppressWarnings("unchecked")
            ServerCall.Listener<ReqT> listener2Typed = (ServerCall.Listener<ReqT>) listener2;
            return listener2Typed;
          }
        };
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor));
    assertSame(listener2,
        getSoleMethod(intercepted).getServerCallHandler().startCall(methodName, call, headers));
    verify(handler).startCall(method2, call2, headers);
  }

  @SuppressWarnings("unchecked")
  private static ServerMethodDefinition<String, Integer> getSoleMethod(
      ServerServiceDefinition serviceDef) {
    if (serviceDef.getMethods().size() != 1) {
      throw new AssertionError("Not exactly one method present");
    }
    return (ServerMethodDefinition<String, Integer>) serviceDef.getMethods().get(0);
  }

  @SuppressWarnings("unchecked")
  private static ServerMethodDefinition<String, Integer> getMethod(
      ServerServiceDefinition serviceDef, String name) {
    return (ServerMethodDefinition<String, Integer>) serviceDef.getMethod(name);
  }

  private ServerCallHandler<String, Integer> anyCallHandler() {
    return Mockito.<ServerCallHandler<String, Integer>>any();
  }

  private static class NoopInterceptor implements ServerInterceptor {
    @Override
    public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(String method,
        ServerCall<RespT> call, Headers headers, ServerCallHandler<ReqT, RespT> next) {
      return next.startCall(method, call, headers);
    }
  }
}
