package com.google.net.stubby;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.same;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import com.google.net.stubby.ServerInterceptor;
import com.google.net.stubby.ServerCall;
import com.google.net.stubby.ServerCallHandler;
import com.google.net.stubby.ServerServiceDefinition;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mockito;

import java.util.Arrays;
import java.util.ArrayList;
import java.util.List;

/** Unit tests for {@link ServerInterceptors}. */
@RunWith(JUnit4.class)
public class ServerInterceptorsTest {
  private Marshaller<String> requestMarshaller = mock(Marshaller.class);
  private Marshaller<Integer> responseMarshaller = mock(Marshaller.class);
  private ServerCallHandler<String, Integer> handler = mock(ServerCallHandler.class);
  private ServerCall.Listener<String> listener = mock(ServerCall.Listener.class);
  private MethodDescriptor<String, Integer> methodDescriptor = mock(MethodDescriptor.class);
  private ServerCall<Integer> call = mock(ServerCall.class);
  private ServerServiceDefinition serviceDefinition = ServerServiceDefinition.builder("basic")
      .addMethod("flow", requestMarshaller, responseMarshaller, handler).build();

  @Before
  public void setUp() {
    Mockito.when(handler.startCall(
          Mockito.<MethodDescriptor<String, Integer>>any(), Mockito.<ServerCall<Integer>>any()))
        .thenReturn(listener);
  }

  @After
  public void makeSureExpectedMocksUnused() {
    verifyZeroInteractions(requestMarshaller);
    verifyZeroInteractions(responseMarshaller);
    verifyZeroInteractions(listener);
    verifyZeroInteractions(methodDescriptor);
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullServiceDefinition() {
    ServerInterceptors.intercept(null, Arrays.<ServerInterceptor>asList());
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullInterceptorList() {
    ServerInterceptors.intercept(serviceDefinition, null);
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
        intercepted.getMethods().get(0).getServerCallHandler().startCall(methodDescriptor, call));
    verify(interceptor).interceptCall(same(methodDescriptor), same(call), anyCallHandler());
    verify(handler).startCall(methodDescriptor, call);
    verifyNoMoreInteractions(interceptor, handler);

    assertSame(listener,
        intercepted.getMethods().get(0).getServerCallHandler().startCall(methodDescriptor, call));
    verify(interceptor, times(2))
        .interceptCall(same(methodDescriptor), same(call), anyCallHandler());
    verify(handler, times(2)).startCall(methodDescriptor, call);
    verifyNoMoreInteractions(interceptor, handler);
  }

  @Test
  public void correctHandlerCalled() {
    ServerCallHandler<String, Integer> handler2 = Mockito.mock(ServerCallHandler.class);
    serviceDefinition = ServerServiceDefinition.builder("basic")
        .addMethod("flow", requestMarshaller, responseMarshaller, handler)
        .addMethod("flow2", requestMarshaller, responseMarshaller, handler2).build();
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.<ServerInterceptor>asList(new NoopInterceptor()));
    intercepted.getMethod("flow").getServerCallHandler().startCall(methodDescriptor, call);
    verify(handler).startCall(methodDescriptor, call);
    verifyNoMoreInteractions(handler);
    verifyZeroInteractions(handler2);

    intercepted.getMethod("flow2").getServerCallHandler().startCall(methodDescriptor, call);
    verify(handler2).startCall(methodDescriptor, call);
    verifyNoMoreInteractions(handler);
    verifyNoMoreInteractions(handler2);
  }

  @Test
  public void ordered() {
    final List<String> order = new ArrayList<String>();
    handler = new ServerCallHandler<String, Integer>() {
          @Override
          public ServerCall.Listener<String> startCall(MethodDescriptor<String, Integer> method,
              ServerCall<Integer> call) {
            order.add("handler");
            return listener;
          }
        };
    ServerInterceptor interceptor1 = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
              MethodDescriptor<ReqT, RespT> method, ServerCall<RespT> call,
              ServerCallHandler<ReqT, RespT> next) {
            order.add("i1");
            return next.startCall(method, call);
          }
        };
    ServerInterceptor interceptor2 = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
              MethodDescriptor<ReqT, RespT> method, ServerCall<RespT> call,
              ServerCallHandler<ReqT, RespT> next) {
            order.add("i2");
            return next.startCall(method, call);
          }
        };
    ServerServiceDefinition serviceDefinition = ServerServiceDefinition.builder("basic")
        .addMethod("flow", requestMarshaller, responseMarshaller, handler).build();
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor1, interceptor2));
    assertSame(listener,
        intercepted.getMethods().get(0).getServerCallHandler().startCall(methodDescriptor, call));
    assertEquals(Arrays.asList("i1", "i2", "handler"), order);
  }

  @Test
  public void argumentsPassed() {
    final MethodDescriptor<String, Integer> method2 = mock(MethodDescriptor.class);
    @SuppressWarnings("unchecked")
    final ServerCall<Integer> call2 = mock(ServerCall.class);
    final ServerCall.Listener<String> listener2 = mock(ServerCall.Listener.class);
    ServerInterceptor interceptor = new ServerInterceptor() {
          @Override
          public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
              MethodDescriptor<ReqT, RespT> method, ServerCall<RespT> call,
              ServerCallHandler<ReqT, RespT> next) {
            assertSame(method, methodDescriptor);
            assertSame(call, ServerInterceptorsTest.this.call);
            assertSame(listener, next.startCall((MethodDescriptor) method2, (ServerCall) call2));
            return (ServerCall.Listener) listener2;
          }
        };
    ServerServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor));
    assertSame(listener2,
        intercepted.getMethods().get(0).getServerCallHandler().startCall(methodDescriptor, call));
    verify(handler).startCall(method2, call2);
  }

  private ServerCallHandler<String, Integer> anyCallHandler() {
    return Mockito.<ServerCallHandler<String, Integer>>any();
  }

  private static class NoopInterceptor implements ServerInterceptor {
    @Override
    public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
        MethodDescriptor<ReqT, RespT> method, ServerCall<RespT> call,
        ServerCallHandler<ReqT, RespT> next) {
      return next.startCall(method, call);
    }
  }
}
