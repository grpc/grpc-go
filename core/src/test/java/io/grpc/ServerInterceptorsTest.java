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

import static com.google.common.collect.Iterables.getOnlyElement;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall.Listener;

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
  private MethodDescriptor<String, Integer> method = MethodDescriptor.create(
      MethodType.UNKNOWN,
      "someRandom/Name",
      requestMarshaller,
      responseMarshaller);
  @Mock private ServerCall<Integer> call;
  private ServerServiceDefinition serviceDefinition = ServerServiceDefinition.builder("basic")
      .addMethod(
          MethodDescriptor.create(
            MethodType.UNKNOWN, "basic/flow", requestMarshaller, responseMarshaller),
          handler).build();
  private Metadata headers = new Metadata();

  /** Set up for test. */
  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    Mockito.when(handler.startCall(
        Mockito.<MethodDescriptor<String, Integer>>any(),
        Mockito.<ServerCall<Integer>>any(), Mockito.<Metadata>any()))
            .thenReturn(listener);
  }

  /** Final checks for all tests. */
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
        getSoleMethod(intercepted).getServerCallHandler().startCall(method, call, headers));
    verify(interceptor).interceptCall(
        same(method), same(call), same(headers), anyCallHandler());
    verify(handler).startCall(method, call, headers);
    verifyNoMoreInteractions(interceptor, handler);

    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(method, call, headers));
    verify(interceptor, times(2))
        .interceptCall(same(method), same(call), same(headers), anyCallHandler());
    verify(handler, times(2)).startCall(method, call, headers);
    verifyNoMoreInteractions(interceptor, handler);
  }

  @Test
  public void correctHandlerCalled() {
    @SuppressWarnings("unchecked")
    ServerCallHandler<String, Integer> handler2 = mock(ServerCallHandler.class);
    serviceDefinition = ServerServiceDefinition.builder("basic")
        .addMethod(MethodDescriptor.create(MethodType.UNKNOWN, "basic/flow",
              requestMarshaller, responseMarshaller), handler)
        .addMethod(MethodDescriptor.create(MethodType.UNKNOWN, "basic/flow2",
              requestMarshaller, responseMarshaller), handler2).build();
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.<ServerInterceptor>asList(new NoopInterceptor()));
    getMethod(intercepted, "basic/flow").getServerCallHandler().startCall(
        method, call, headers);
    verify(handler).startCall(method, call, headers);
    verifyNoMoreInteractions(handler);
    verifyZeroInteractions(handler2);

    getMethod(intercepted, "basic/flow2").getServerCallHandler().startCall(
        method, call, headers);
    verify(handler2).startCall(method, call, headers);
    verifyNoMoreInteractions(handler);
    verifyNoMoreInteractions(handler2);
  }

  @Test
  public void callNextTwice() {
    ServerInterceptor interceptor = new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          ServerCall<RespT> call,
          Metadata headers,
          ServerCallHandler<ReqT, RespT> next) {
        // Calling next twice is permitted, although should only rarely be useful.
        assertSame(listener, next.startCall(method, call, headers));
        return next.startCall(method, call, headers);
      }
    };
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(serviceDefinition,
        interceptor);
    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(method, call, headers));
    verify(handler, times(2)).startCall(same(method), same(call), same(headers));
    verifyNoMoreInteractions(handler);
  }

  @Test
  public void ordered() {
    final List<String> order = new ArrayList<String>();
    handler = new ServerCallHandler<String, Integer>() {
          @Override
          public ServerCall.Listener<String> startCall(
              MethodDescriptor<String, Integer> method,
              ServerCall<Integer> call,
              Metadata headers) {
            order.add("handler");
            return listener;
          }
        };
    ServerInterceptor interceptor1 = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
              MethodDescriptor<ReqT, RespT> method,
              ServerCall<RespT> call,
              Metadata headers,
              ServerCallHandler<ReqT, RespT> next) {
            order.add("i1");
            return next.startCall(method, call, headers);
          }
        };
    ServerInterceptor interceptor2 = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
              MethodDescriptor<ReqT, RespT> method,
              ServerCall<RespT> call,
              Metadata headers,
              ServerCallHandler<ReqT, RespT> next) {
            order.add("i2");
            return next.startCall(method, call, headers);
          }
        };
    ServerServiceDefinition serviceDefinition = ServerServiceDefinition.builder("basic")
        .addMethod(MethodDescriptor.create(MethodType.UNKNOWN, "basic/flow",
              requestMarshaller, responseMarshaller), handler).build();
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor1, interceptor2));
    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(method, call, headers));
    assertEquals(Arrays.asList("i2", "i1", "handler"), order);
  }

  @Test
  public void argumentsPassed() {
    final MethodDescriptor<String, Integer> method2 = MethodDescriptor.create(
        MethodType.UNKNOWN, "someOtherRandom/Method", requestMarshaller, responseMarshaller);
    @SuppressWarnings("unchecked")
    final ServerCall<Integer> call2 = mock(ServerCall.class);
    @SuppressWarnings("unchecked")
    final ServerCall.Listener<String> listener2 = mock(ServerCall.Listener.class);

    ServerInterceptor interceptor = new ServerInterceptor() {
        @SuppressWarnings("unchecked") // Lot's of casting for no benefit.  Not intended use.
        @Override
        public <R1, R2> ServerCall.Listener<R1> interceptCall(
            MethodDescriptor<R1, R2> methodDescriptor,
            ServerCall<R2> call,
            Metadata headers,
            ServerCallHandler<R1, R2> next) {
          assertSame(method, methodDescriptor);
          assertSame(call, ServerInterceptorsTest.this.call);
          assertSame(listener,
              next.startCall((MethodDescriptor<R1, R2>)method2, (ServerCall<R2>)call2, headers));
          return (ServerCall.Listener<R1>) listener2;
        }
      };
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor));
    assertSame(listener2,
        getSoleMethod(intercepted).getServerCallHandler().startCall(method, call, headers));
    verify(handler).startCall(method2, call2, headers);
  }

  @SuppressWarnings("unchecked")
  private static ServerMethodDefinition<String, Integer> getSoleMethod(
      ServerServiceDefinition serviceDef) {
    if (serviceDef.getMethods().size() != 1) {
      throw new AssertionError("Not exactly one method present");
    }
    return (ServerMethodDefinition<String, Integer>) getOnlyElement(serviceDef.getMethods());
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
    public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
        MethodDescriptor<ReqT, RespT> method,
        ServerCall<RespT> call,
        Metadata headers,
        ServerCallHandler<ReqT, RespT> next) {
      return next.startCall(method, call, headers);
    }
  }
}
