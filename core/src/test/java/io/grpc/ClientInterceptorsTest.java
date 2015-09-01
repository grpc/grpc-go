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
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.ClientInterceptors.CheckedForwardingClientCall;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import io.grpc.ForwardingClientCallListener.SimpleForwardingClientCallListener;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/** Unit tests for {@link ClientInterceptors}. */
@RunWith(JUnit4.class)
public class ClientInterceptorsTest {

  @Mock
  private Channel channel;

  @Mock
  private ClientCall<String, Integer> call;

  @Mock
  private MethodDescriptor<String, Integer> method;

  /**
   * Sets up mocks.
   */
  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(channel.newCall(
        Mockito.<MethodDescriptor<String, Integer>>any(), any(CallOptions.class)))
        .thenReturn(call);

    // Emulate the precondition checks in ChannelImpl.CallImpl
    Answer<Void> checkStartCalled = new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) {
        verify(call).start(Mockito.<ClientCall.Listener<Integer>>any(), Mockito.<Metadata>any());
        return null;
      }
    };
    doAnswer(checkStartCalled).when(call).request(anyInt());
    doAnswer(checkStartCalled).when(call).halfClose();
    doAnswer(checkStartCalled).when(call).sendMessage(Mockito.<String>any());
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullChannel() {
    ClientInterceptors.intercept(null, Arrays.<ClientInterceptor>asList());
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullInterceptorList() {
    ClientInterceptors.intercept(channel, (List<ClientInterceptor>) null);
  }

  @Test(expected = NullPointerException.class)
  public void npeForNullInterceptor() {
    ClientInterceptors.intercept(channel, (ClientInterceptor) null);
  }

  @Test
  public void noop() {
    assertSame(channel, ClientInterceptors.intercept(channel, Arrays.<ClientInterceptor>asList()));
  }

  @Test
  public void channelAndInterceptorCalled() {
    ClientInterceptor interceptor = spy(new NoopInterceptor());
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    CallOptions callOptions = CallOptions.DEFAULT;
    // First call
    assertSame(call, intercepted.newCall(method, callOptions));
    verify(channel).newCall(same(method), same(callOptions));
    verify(interceptor).interceptCall(same(method), same(callOptions), Mockito.<Channel>any());
    verifyNoMoreInteractions(channel, interceptor);
    // Second call
    assertSame(call, intercepted.newCall(method, callOptions));
    verify(channel, times(2)).newCall(same(method), same(callOptions));
    verify(interceptor, times(2))
        .interceptCall(same(method), same(callOptions), Mockito.<Channel>any());
    verifyNoMoreInteractions(channel, interceptor);
  }

  @Test
  public void callNextTwice() {
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        // Calling next twice is permitted, although should only rarely be useful.
        assertSame(call, next.newCall(method, callOptions));
        return next.newCall(method, callOptions);
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    assertSame(call, intercepted.newCall(method, CallOptions.DEFAULT));
    verify(channel, times(2)).newCall(same(method), same(CallOptions.DEFAULT));
    verifyNoMoreInteractions(channel);
  }

  @Test
  public void ordered() {
    final List<String> order = new ArrayList<String>();
    channel = new Channel() {
      @SuppressWarnings("unchecked")
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(
          MethodDescriptor<ReqT, RespT> method, CallOptions callOptions) {
        order.add("channel");
        return (ClientCall<ReqT, RespT>) call;
      }

      @Override
      public String authority() {
        return null;
      }
    };
    ClientInterceptor interceptor1 = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        order.add("i1");
        return next.newCall(method, callOptions);
      }
    };
    ClientInterceptor interceptor2 = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        order.add("i2");
        return next.newCall(method, callOptions);
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor1, interceptor2);
    assertSame(call, intercepted.newCall(method, CallOptions.DEFAULT));
    assertEquals(Arrays.asList("i2", "i1", "channel"), order);
  }

  @Test
  public void callOptions() {
    final CallOptions initialCallOptions = CallOptions.DEFAULT.withDeadlineNanoTime(100L);
    final CallOptions newCallOptions = initialCallOptions.withDeadlineNanoTime(300L);
    assertNotSame(initialCallOptions, newCallOptions);
    ClientInterceptor interceptor = spy(new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        return next.newCall(method, newCallOptions);
      }
    });
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    intercepted.newCall(method, initialCallOptions);
    verify(interceptor).interceptCall(
        same(method), same(initialCallOptions), Mockito.<Channel>any());
    verify(channel).newCall(same(method), same(newCallOptions));
  }

  @Test
  public void addOutboundHeaders() {
    final Metadata.Key<String> credKey = Metadata.Key.of("Cred", Metadata.ASCII_STRING_MARSHALLER);
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        ClientCall<ReqT, RespT> call = next.newCall(method, callOptions);
        return new SimpleForwardingClientCall<ReqT, RespT>(call) {
          @Override
          public void start(ClientCall.Listener<RespT> responseListener, Metadata headers) {
            headers.put(credKey, "abcd");
            super.start(responseListener, headers);
          }
        };
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    @SuppressWarnings("unchecked")
    ClientCall.Listener<Integer> listener = mock(ClientCall.Listener.class);
    ClientCall<String, Integer> interceptedCall = intercepted.newCall(method, CallOptions.DEFAULT);
    // start() on the intercepted call will eventually reach the call created by the real channel
    interceptedCall.start(listener, new Metadata());
    ArgumentCaptor<Metadata> captor = ArgumentCaptor.forClass(Metadata.class);
    // The headers passed to the real channel call will contain the information inserted by the
    // interceptor.
    verify(call).start(same(listener), captor.capture());
    assertEquals("abcd", captor.getValue().get(credKey));
  }

  @Test
  public void examineInboundHeaders() {
    final List<Metadata> examinedHeaders = new ArrayList<Metadata>();
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        ClientCall<ReqT, RespT> call = next.newCall(method, callOptions);
        return new SimpleForwardingClientCall<ReqT, RespT>(call) {
          @Override
          public void start(ClientCall.Listener<RespT> responseListener, Metadata headers) {
            super.start(new SimpleForwardingClientCallListener<RespT>(responseListener) {
              @Override
              public void onHeaders(Metadata headers) {
                examinedHeaders.add(headers);
                super.onHeaders(headers);
              }
            }, headers);
          }
        };
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    @SuppressWarnings("unchecked")
    ClientCall.Listener<Integer> listener = mock(ClientCall.Listener.class);
    ClientCall<String, Integer> interceptedCall = intercepted.newCall(method, CallOptions.DEFAULT);
    interceptedCall.start(listener, new Metadata());
    // Capture the underlying call listener that will receive headers from the transport.
    ArgumentCaptor<ClientCall.Listener<Integer>> captor = ArgumentCaptor.forClass(null);
    verify(call).start(captor.capture(), Mockito.<Metadata>any());
    Metadata inboundHeaders = new Metadata();
    // Simulate that a headers arrives on the underlying call listener.
    captor.getValue().onHeaders(inboundHeaders);
    assertEquals(Arrays.asList(inboundHeaders), examinedHeaders);
  }

  @Test
  public void normalCall() {
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        ClientCall<ReqT, RespT> call = next.newCall(method, callOptions);
        return new SimpleForwardingClientCall<ReqT, RespT>(call) { };
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    ClientCall<String, Integer> interceptedCall = intercepted.newCall(method, CallOptions.DEFAULT);
    assertNotSame(call, interceptedCall);
    @SuppressWarnings("unchecked")
    ClientCall.Listener<Integer> listener = mock(ClientCall.Listener.class);
    Metadata headers = new Metadata();
    interceptedCall.start(listener, headers);
    verify(call).start(same(listener), same(headers));
    interceptedCall.sendMessage("request");
    verify(call).sendMessage(eq("request"));
    interceptedCall.halfClose();
    verify(call).halfClose();
    interceptedCall.request(1);
    verify(call).request(1);
  }

  @Test
  public void exceptionInStart() {
    final Exception error = new Exception("emulated error");
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        ClientCall<ReqT, RespT> call = next.newCall(method, callOptions);
        return new CheckedForwardingClientCall<ReqT, RespT>(call) {
          @Override
          protected void checkedStart(ClientCall.Listener<RespT> responseListener, Metadata headers)
              throws Exception {
            throw error;
            // delegate().start will not be called
          }
        };
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    @SuppressWarnings("unchecked")
    ClientCall.Listener<Integer> listener = mock(ClientCall.Listener.class);
    ClientCall<String, Integer> interceptedCall = intercepted.newCall(method, CallOptions.DEFAULT);
    assertNotSame(call, interceptedCall);
    interceptedCall.start(listener, new Metadata());
    interceptedCall.sendMessage("request");
    interceptedCall.halfClose();
    interceptedCall.request(1);
    verifyNoMoreInteractions(call);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).onClose(captor.capture(), any(Metadata.class));
    assertSame(error, captor.getValue().getCause());
  }

  private static class NoopInterceptor implements ClientInterceptor {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
        CallOptions callOptions, Channel next) {
      return next.newCall(method, callOptions);
    }
  }

}
