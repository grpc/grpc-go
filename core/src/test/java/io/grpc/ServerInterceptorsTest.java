/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc;

import static com.google.common.collect.Iterables.getOnlyElement;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall.Listener;
import io.grpc.internal.NoopServerCall;
import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

/** Unit tests for {@link ServerInterceptors}. */
@RunWith(JUnit4.class)
public class ServerInterceptorsTest {
  @Mock
  private Marshaller<String> requestMarshaller;

  @Mock
  private Marshaller<Integer> responseMarshaller;

  @Mock
  private ServerCallHandler<String, Integer> handler;

  @Mock
  private ServerCall.Listener<String> listener;

  private MethodDescriptor<String, Integer> flowMethod;

  private ServerCall<String, Integer> call = new NoopServerCall<String, Integer>();

  private ServerServiceDefinition serviceDefinition;

  private final Metadata headers = new Metadata();

  /** Set up for test. */
  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    flowMethod = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodType.UNKNOWN)
        .setFullMethodName("basic/flow")
        .setRequestMarshaller(requestMarshaller)
        .setResponseMarshaller(responseMarshaller)
        .build();

    Mockito.when(handler.startCall(
        Mockito.<ServerCall<String, Integer>>any(), Mockito.<Metadata>any()))
            .thenReturn(listener);

    serviceDefinition = ServerServiceDefinition.builder(new ServiceDescriptor("basic", flowMethod))
        .addMethod(flowMethod, handler).build();
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
    ServerServiceDefinition serviceDef = null;
    ServerInterceptors.intercept(serviceDef, Arrays.<ServerInterceptor>asList());
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
    ServerInterceptor interceptor =
        mock(ServerInterceptor.class, delegatesTo(new NoopInterceptor()));
    ServerServiceDefinition intercepted
        = ServerInterceptors.intercept(serviceDefinition, Arrays.asList(interceptor));
    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers));
    verify(interceptor).interceptCall(same(call), same(headers), anyCallHandler());
    verify(handler).startCall(call, headers);
    verifyNoMoreInteractions(interceptor, handler);

    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers));
    verify(interceptor, times(2))
        .interceptCall(same(call), same(headers), anyCallHandler());
    verify(handler, times(2)).startCall(call, headers);
    verifyNoMoreInteractions(interceptor, handler);
  }

  @Test
  public void correctHandlerCalled() {
    @SuppressWarnings("unchecked")
    ServerCallHandler<String, Integer> handler2 = mock(ServerCallHandler.class);
    MethodDescriptor<String, Integer> flowMethod2 =
        flowMethod.toBuilder().setFullMethodName("basic/flow2").build();
    serviceDefinition = ServerServiceDefinition.builder(
        new ServiceDescriptor("basic", flowMethod, flowMethod2))
        .addMethod(flowMethod, handler)
        .addMethod(flowMethod2, handler2).build();
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.<ServerInterceptor>asList(new NoopInterceptor()));
    getMethod(intercepted, "basic/flow").getServerCallHandler().startCall(call, headers);
    verify(handler).startCall(call, headers);
    verifyNoMoreInteractions(handler);
    verifyNoMoreInteractions(handler2);

    getMethod(intercepted, "basic/flow2").getServerCallHandler().startCall(call, headers);
    verify(handler2).startCall(call, headers);
    verifyNoMoreInteractions(handler);
    verifyNoMoreInteractions(handler2);
  }

  @Test
  public void callNextTwice() {
    ServerInterceptor interceptor = new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          ServerCall<ReqT, RespT> call,
          Metadata headers,
          ServerCallHandler<ReqT, RespT> next) {
        // Calling next twice is permitted, although should only rarely be useful.
        assertSame(listener, next.startCall(call, headers));
        return next.startCall(call, headers);
      }
    };
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(serviceDefinition,
        interceptor);
    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers));
    verify(handler, times(2)).startCall(same(call), same(headers));
    verifyNoMoreInteractions(handler);
  }

  @Test
  public void ordered() {
    final List<String> order = new ArrayList<>();
    handler = new ServerCallHandler<String, Integer>() {
          @Override
          public ServerCall.Listener<String> startCall(
              ServerCall<String, Integer> call,
              Metadata headers) {
            order.add("handler");
            return listener;
          }
        };
    ServerInterceptor interceptor1 = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
              ServerCall<ReqT, RespT> call,
              Metadata headers,
              ServerCallHandler<ReqT, RespT> next) {
            order.add("i1");
            return next.startCall(call, headers);
          }
        };
    ServerInterceptor interceptor2 = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
              ServerCall<ReqT, RespT> call,
              Metadata headers,
              ServerCallHandler<ReqT, RespT> next) {
            order.add("i2");
            return next.startCall(call, headers);
          }
        };
    ServerServiceDefinition serviceDefinition = ServerServiceDefinition.builder(
        new ServiceDescriptor("basic", flowMethod))
        .addMethod(flowMethod, handler).build();
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor1, interceptor2));
    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers));
    assertEquals(Arrays.asList("i2", "i1", "handler"), order);
  }

  @Test
  public void orderedForward() {
    final List<String> order = new ArrayList<>();
    handler = new ServerCallHandler<String, Integer>() {
      @Override
      public ServerCall.Listener<String> startCall(
          ServerCall<String, Integer> call,
          Metadata headers) {
        order.add("handler");
        return listener;
      }
    };
    ServerInterceptor interceptor1 = new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          ServerCall<ReqT, RespT> call,
          Metadata headers,
          ServerCallHandler<ReqT, RespT> next) {
        order.add("i1");
        return next.startCall(call, headers);
      }
    };
    ServerInterceptor interceptor2 = new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          ServerCall<ReqT, RespT> call,
          Metadata headers,
          ServerCallHandler<ReqT, RespT> next) {
        order.add("i2");
        return next.startCall(call, headers);
      }
    };
    ServerServiceDefinition serviceDefinition = ServerServiceDefinition.builder(
        new ServiceDescriptor("basic", flowMethod))
        .addMethod(flowMethod, handler).build();
    ServerServiceDefinition intercepted = ServerInterceptors.interceptForward(
        serviceDefinition, interceptor1, interceptor2);
    assertSame(listener,
        getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers));
    assertEquals(Arrays.asList("i1", "i2", "handler"), order);
  }

  @Test
  public void argumentsPassed() {
    @SuppressWarnings("unchecked")
    final ServerCall<String, Integer> call2 = new NoopServerCall<String, Integer>();
    @SuppressWarnings("unchecked")
    final ServerCall.Listener<String> listener2 = mock(ServerCall.Listener.class);

    ServerInterceptor interceptor = new ServerInterceptor() {
        @SuppressWarnings("unchecked") // Lot's of casting for no benefit.  Not intended use.
        @Override
        public <R1, R2> ServerCall.Listener<R1> interceptCall(
            ServerCall<R1, R2> call,
            Metadata headers,
            ServerCallHandler<R1, R2> next) {
          assertSame(call, ServerInterceptorsTest.this.call);
          assertSame(listener,
              next.startCall((ServerCall<R1, R2>)call2, headers));
          return (ServerCall.Listener<R1>) listener2;
        }
      };
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor));
    assertSame(listener2,
        getSoleMethod(intercepted).getServerCallHandler().startCall(call, headers));
    verify(handler).startCall(call2, headers);
  }

  @Test
  @SuppressWarnings("unchecked")
  public void typedMarshalledMessages() {
    final List<String> order = new ArrayList<>();
    Marshaller<Holder> marshaller = new Marshaller<Holder>() {
      @Override
      public InputStream stream(Holder value) {
        return value.get();
      }

      @Override
      public Holder parse(InputStream stream) {
        return new Holder(stream);
      }
    };

    ServerCallHandler<Holder, Holder> handler2 = new ServerCallHandler<Holder, Holder>() {
      @Override
      public Listener<Holder> startCall(final ServerCall<Holder, Holder> call,
                                        final Metadata headers) {
        return new Listener<Holder>() {
          @Override
          public void onMessage(Holder message) {
            order.add("handler");
            call.sendMessage(message);
          }
        };
      }
    };

    MethodDescriptor<Holder, Holder> wrappedMethod = MethodDescriptor.<Holder, Holder>newBuilder()
        .setType(MethodType.UNKNOWN)
        .setFullMethodName("basic/wrapped")
        .setRequestMarshaller(marshaller)
        .setResponseMarshaller(marshaller)
        .build();
    ServerServiceDefinition serviceDef = ServerServiceDefinition.builder(
        new ServiceDescriptor("basic", wrappedMethod))
        .addMethod(wrappedMethod, handler2).build();

    ServerInterceptor interceptor1 = new ServerInterceptor() {
      @Override
      public <ReqT, RespT> Listener<ReqT> interceptCall(ServerCall<ReqT, RespT> call,
                                                        Metadata headers,
                                                        ServerCallHandler<ReqT, RespT> next) {
        ServerCall<ReqT, RespT> interceptedCall = new ForwardingServerCall
            .SimpleForwardingServerCall<ReqT, RespT>(call) {
          @Override
          public void sendMessage(RespT message) {
            order.add("i1sendMessage");
            assertTrue(message instanceof Holder);
            super.sendMessage(message);
          }
        };

        ServerCall.Listener<ReqT> originalListener = next
            .startCall(interceptedCall, headers);
        return new ForwardingServerCallListener
            .SimpleForwardingServerCallListener<ReqT>(originalListener) {
          @Override
          public void onMessage(ReqT message) {
            order.add("i1onMessage");
            assertTrue(message instanceof Holder);
            super.onMessage(message);
          }
        };
      }
    };

    ServerInterceptor interceptor2 = new ServerInterceptor() {
      @Override
      public <ReqT, RespT> Listener<ReqT> interceptCall(ServerCall<ReqT, RespT> call,
                                                        Metadata headers,
                                                        ServerCallHandler<ReqT, RespT> next) {
        ServerCall<ReqT, RespT> interceptedCall = new ForwardingServerCall
            .SimpleForwardingServerCall<ReqT, RespT>(call) {
          @Override
          public void sendMessage(RespT message) {
            order.add("i2sendMessage");
            assertTrue(message instanceof InputStream);
            super.sendMessage(message);
          }
        };

        ServerCall.Listener<ReqT> originalListener = next
            .startCall(interceptedCall, headers);
        return new ForwardingServerCallListener
            .SimpleForwardingServerCallListener<ReqT>(originalListener) {
          @Override
          public void onMessage(ReqT message) {
            order.add("i2onMessage");
            assertTrue(message instanceof InputStream);
            super.onMessage(message);
          }
        };
      }
    };

    ServerServiceDefinition intercepted = ServerInterceptors.intercept(serviceDef, interceptor1);
    ServerServiceDefinition inputStreamMessageService = ServerInterceptors
        .useInputStreamMessages(intercepted);
    ServerServiceDefinition intercepted2 = ServerInterceptors
        .intercept(inputStreamMessageService, interceptor2);
    ServerMethodDefinition<InputStream, InputStream> serverMethod =
        (ServerMethodDefinition<InputStream, InputStream>) intercepted2.getMethod("basic/wrapped");
    ServerCall<InputStream, InputStream> call2 = new NoopServerCall<InputStream, InputStream>();
    byte[] bytes = {};
    serverMethod
        .getServerCallHandler()
        .startCall(call2, headers)
        .onMessage(new ByteArrayInputStream(bytes));
    assertEquals(
        Arrays.asList("i2onMessage", "i1onMessage", "handler", "i1sendMessage", "i2sendMessage"),
        order);
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
        ServerCall<ReqT, RespT> call,
        Metadata headers,
        ServerCallHandler<ReqT, RespT> next) {
      return next.startCall(call, headers);
    }
  }

  private static class Holder {
    private final InputStream inputStream;

    Holder(InputStream inputStream) {
      this.inputStream = inputStream;
    }

    public InputStream get() {
      return inputStream;
    }
  }
}
