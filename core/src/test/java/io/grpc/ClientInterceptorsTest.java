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
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.ClientInterceptors.ForwardingCall;
import io.grpc.ClientInterceptors.ForwardingListener;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/** Unit tests for {@link ClientInterceptors}. */
@RunWith(JUnit4.class)
public class ClientInterceptorsTest {

  @Mock
  private Channel channel;

  @Mock
  private Call<String, Integer> call;

  @Mock
  private MethodDescriptor<String, Integer> method;

  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(channel.newCall(Mockito.<MethodDescriptor<String, Integer>>any())).thenReturn(call);
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
    // First call
    assertSame(call, intercepted.newCall(method));
    verify(channel).newCall(same(method));
    verify(interceptor).interceptCall(same(method), Mockito.<Channel>any());
    verifyNoMoreInteractions(channel, interceptor);
    // Second call
    assertSame(call, intercepted.newCall(method));
    verify(channel, times(2)).newCall(same(method));
    verify(interceptor, times(2)).interceptCall(same(method), Mockito.<Channel>any());
    verifyNoMoreInteractions(channel, interceptor);
  }

  @Test(expected = IllegalStateException.class)
  public void callNextTwice() {
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        next.newCall(method);
        return next.newCall(method);
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    intercepted.newCall(method);
  }

  @Test
  public void ordered() {
    final List<String> order = new ArrayList<String>();
    channel = new Channel() {
      @SuppressWarnings("unchecked")
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method) {
        order.add("channel");
        return (Call<ReqT, RespT>) call;
      }
    };
    ClientInterceptor interceptor1 = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        order.add("i1");
        return next.newCall(method);
      }
    };
    ClientInterceptor interceptor2 = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        order.add("i2");
        return next.newCall(method);
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor1, interceptor2);
    assertSame(call, intercepted.newCall(method));
    assertEquals(Arrays.asList("i1", "i2", "channel"), order);
  }

  @Test
  public void addOutboundHeaders() {
    final Metadata.Key<String> credKey = Metadata.Key.of("Cred", Metadata.ASCII_STRING_MARSHALLER);
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        Call<ReqT, RespT> call = next.newCall(method);
        return new ForwardingCall<ReqT, RespT>(call) {
          @Override
          public void start(Call.Listener<RespT> responseListener, Metadata.Headers headers) {
            headers.put(credKey, "abcd");
            super.start(responseListener, headers);
          }
        };
      }
    };
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    @SuppressWarnings("unchecked")
    Call.Listener<Integer> listener = mock(Call.Listener.class);
    Call<String, Integer> interceptedCall = intercepted.newCall(method);
    // start() on the intercepted call will eventually reach the call created by the real channel
    interceptedCall.start(listener, new Metadata.Headers());
    ArgumentCaptor<Metadata.Headers> captor = ArgumentCaptor.forClass(Metadata.Headers.class);
    // The headers passed to the real channel call will contain the information inserted by the
    // interceptor.
    verify(call).start(same(listener), captor.capture());
    assertEquals("abcd", captor.getValue().get(credKey));
  }

  @Test
  public void examineInboundHeaders() {
    final List<Metadata.Headers> examinedHeaders = new ArrayList<Metadata.Headers>();
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
          Channel next) {
        Call<ReqT, RespT> call = next.newCall(method);
        return new ForwardingCall<ReqT, RespT>(call) {
          @Override
          public void start(Call.Listener<RespT> responseListener, Metadata.Headers headers) {
            super.start(new ForwardingListener<RespT>(responseListener) {
              @Override
              public void onHeaders(Metadata.Headers headers) {
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
    Call.Listener<Integer> listener = mock(Call.Listener.class);
    Call<String, Integer> interceptedCall = intercepted.newCall(method);
    interceptedCall.start(listener, new Metadata.Headers());
    // Capture the underlying call listener that will receive headers from the transport.
    ArgumentCaptor<Call.Listener<Integer>> captor = ArgumentCaptor.forClass(null);
    verify(call).start(captor.capture(), Mockito.<Metadata.Headers>any());
    Metadata.Headers inboundHeaders = new Metadata.Headers();
    // Simulate that a headers arrives on the underlying call listener.
    captor.getValue().onHeaders(inboundHeaders);
    assertEquals(Arrays.asList(inboundHeaders), examinedHeaders);
  }

  private static class NoopInterceptor implements ClientInterceptor {
    @Override
    public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
        Channel next) {
      return next.newCall(method);
    }
  }

}
