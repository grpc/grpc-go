package com.google.net.stubby;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.same;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.verifyZeroInteractions;

import com.google.net.stubby.Server.Interceptor;
import com.google.net.stubby.Server.Call;
import com.google.net.stubby.Server.CallHandler;
import com.google.net.stubby.Server.MethodDefinition;
import com.google.net.stubby.Server.ServiceDefinition;

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
  private CallHandler<String, Integer> handler = mock(CallHandler.class);
  private Call.Listener<String> listener = mock(Call.Listener.class);
  private MethodDescriptor<String, Integer> methodDescriptor = mock(MethodDescriptor.class);
  private Call<String, Integer> call = mock(Call.class);
  private ServiceDefinition serviceDefinition = ServiceDefinition.builder("basic")
      .addMethod("flow", requestMarshaller, responseMarshaller, handler).build();

  @Before
  public void setUp() {
    Mockito.when(handler.startCall(
          Mockito.<MethodDescriptor<String, Integer>>any(), Mockito.<Call<String, Integer>>any()))
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
    ServerInterceptors.intercept(null, Arrays.<Interceptor>asList());
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullInterceptorList() {
    ServerInterceptors.intercept(serviceDefinition, null);
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullInterceptor() {
    ServerInterceptors.intercept(serviceDefinition, Arrays.asList((Interceptor) null));
  }

  @Test
  public void noop() {
    assertSame(serviceDefinition,
        ServerInterceptors.intercept(serviceDefinition, Arrays.<Interceptor>asList()));
  }

  @Test
  public void multipleInvocationsOfHandler() {
    Interceptor interceptor = Mockito.spy(new NoopInterceptor());
    ServiceDefinition intercepted
        = ServerInterceptors.intercept(serviceDefinition, Arrays.asList(interceptor));
    assertSame(listener,
        intercepted.getMethods().get(0).getCallHandler().startCall(methodDescriptor, call));
    verify(interceptor).interceptCall(same(methodDescriptor), same(call), anyCallHandler());
    verify(handler).startCall(methodDescriptor, call);
    verifyNoMoreInteractions(interceptor, handler);

    assertSame(listener,
        intercepted.getMethods().get(0).getCallHandler().startCall(methodDescriptor, call));
    verify(interceptor, times(2))
        .interceptCall(same(methodDescriptor), same(call), anyCallHandler());
    verify(handler, times(2)).startCall(methodDescriptor, call);
    verifyNoMoreInteractions(interceptor, handler);
  }

  @Test
  public void correctHandlerCalled() {
    CallHandler<String, Integer> handler2 = Mockito.mock(CallHandler.class);
    serviceDefinition = ServiceDefinition.builder("basic")
        .addMethod("flow", requestMarshaller, responseMarshaller, handler)
        .addMethod("flow2", requestMarshaller, responseMarshaller, handler2).build();
    ServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.<Interceptor>asList(new NoopInterceptor()));
    intercepted.getMethod("flow").getCallHandler().startCall(methodDescriptor, call);
    verify(handler).startCall(methodDescriptor, call);
    verifyNoMoreInteractions(handler);
    verifyZeroInteractions(handler2);

    intercepted.getMethod("flow2").getCallHandler().startCall(methodDescriptor, call);
    verify(handler2).startCall(methodDescriptor, call);
    verifyNoMoreInteractions(handler);
    verifyNoMoreInteractions(handler2);
  }

  @Test
  public void ordered() {
    final List<String> order = new ArrayList<String>();
    handler = new CallHandler<String, Integer>() {
          @Override
          public Call.Listener<String> startCall(MethodDescriptor<String, Integer> method,
              Call<String, Integer> call) {
            order.add("handler");
            return listener;
          }
        };
    Interceptor interceptor1 = new Interceptor() {
          @Override
          public <ReqT, RespT> Call.Listener<ReqT> interceptCall(
              MethodDescriptor<ReqT, RespT> method, Call<ReqT, RespT> call,
              CallHandler<ReqT, RespT> next) {
            order.add("i1");
            return next.startCall(method, call);
          }
        };
    Interceptor interceptor2 = new Interceptor() {
          @Override
          public <ReqT, RespT> Call.Listener<ReqT> interceptCall(
              MethodDescriptor<ReqT, RespT> method, Call<ReqT, RespT> call,
              CallHandler<ReqT, RespT> next) {
            order.add("i2");
            return next.startCall(method, call);
          }
        };
    ServiceDefinition serviceDefinition = ServiceDefinition.builder("basic")
        .addMethod("flow", requestMarshaller, responseMarshaller, handler).build();
    ServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor1, interceptor2));
    assertSame(listener,
        intercepted.getMethods().get(0).getCallHandler().startCall(methodDescriptor, call));
    assertEquals(Arrays.asList("i1", "i2", "handler"), order);
  }

  @Test
  public void argumentsPassed() {
    final MethodDescriptor<String, Integer> method2 = mock(MethodDescriptor.class);
    final Call<String, Integer> call2 = mock(Call.class);
    final Call.Listener<String> listener2 = mock(Call.Listener.class);
    Interceptor interceptor = new Interceptor() {
          @Override
          public <ReqT, RespT> Call.Listener<ReqT> interceptCall(
              MethodDescriptor<ReqT, RespT> method, Call<ReqT, RespT> call,
              CallHandler<ReqT, RespT> next) {
            assertSame(method, methodDescriptor);
            assertSame(call, ServerInterceptorsTest.this.call);
            assertSame(listener, next.startCall((MethodDescriptor) method2, (Call) call2));
            return (Call.Listener) listener2;
          }
        };
    ServiceDefinition intercepted = ServerInterceptors.intercept(
        serviceDefinition, Arrays.asList(interceptor));
    assertSame(listener2,
        intercepted.getMethods().get(0).getCallHandler().startCall(methodDescriptor, call));
    verify(handler).startCall(method2, call2);
  }

  private CallHandler<String, Integer> anyCallHandler() {
    return Mockito.<CallHandler<String, Integer>>any();
  }

  private static class NoopInterceptor implements Interceptor {
    @Override
    public <ReqT, RespT> Call.Listener<ReqT> interceptCall(MethodDescriptor<ReqT, RespT> method,
        Call<ReqT, RespT> call, CallHandler<ReqT, RespT> next) {
      return next.startCall(method, call);
    }
  }
}
