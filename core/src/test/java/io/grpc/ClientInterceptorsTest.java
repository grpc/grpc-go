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

import static com.google.common.truth.Truth.assertThat;
import static java.util.concurrent.TimeUnit.NANOSECONDS;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.isA;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import io.grpc.ClientInterceptors.CheckedForwardingClientCall;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import io.grpc.ForwardingClientCallListener.SimpleForwardingClientCallListener;
import io.grpc.testing.TestMethodDescriptors;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

/** Unit tests for {@link ClientInterceptors}. */
@RunWith(JUnit4.class)
public class ClientInterceptorsTest {

  @Mock
  private Channel channel;

  private BaseClientCall call = new BaseClientCall();

  private final MethodDescriptor<Void, Void> method = TestMethodDescriptors.voidMethod();

  /**
   * Sets up mocks.
   */
  @Before public void setUp() {
    MockitoAnnotations.initMocks(this);
    when(channel.newCall(
        Mockito.<MethodDescriptor<String, Integer>>any(), any(CallOptions.class)))
        .thenReturn(call);
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
    ClientInterceptor interceptor =
        mock(ClientInterceptor.class, delegatesTo(new NoopInterceptor()));
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
    final List<String> order = new ArrayList<>();
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
  public void orderedForward() {
    final List<String> order = new ArrayList<>();
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
    Channel intercepted = ClientInterceptors.interceptForward(channel, interceptor1, interceptor2);
    assertSame(call, intercepted.newCall(method, CallOptions.DEFAULT));
    assertEquals(Arrays.asList("i1", "i2", "channel"), order);
  }

  @Test
  public void callOptions() {
    final CallOptions initialCallOptions = CallOptions.DEFAULT.withDeadlineAfter(100, NANOSECONDS);
    final CallOptions newCallOptions = initialCallOptions.withDeadlineAfter(300, NANOSECONDS);
    assertNotSame(initialCallOptions, newCallOptions);
    ClientInterceptor interceptor =
        mock(ClientInterceptor.class, delegatesTo(new ClientInterceptor() {
          @Override
          public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
              MethodDescriptor<ReqT, RespT> method,
              CallOptions callOptions,
              Channel next) {
            return next.newCall(method, newCallOptions);
          }
        }));
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
    ClientCall.Listener<Void> listener = mock(ClientCall.Listener.class);
    ClientCall<Void, Void> interceptedCall = intercepted.newCall(method, CallOptions.DEFAULT);
    // start() on the intercepted call will eventually reach the call created by the real channel
    interceptedCall.start(listener, new Metadata());
    // The headers passed to the real channel call will contain the information inserted by the
    // interceptor.
    assertSame(listener, call.listener);
    assertEquals("abcd", call.headers.get(credKey));
  }

  @Test
  public void examineInboundHeaders() {
    final List<Metadata> examinedHeaders = new ArrayList<>();
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
    ClientCall.Listener<Void> listener = mock(ClientCall.Listener.class);
    ClientCall<Void, Void> interceptedCall = intercepted.newCall(method, CallOptions.DEFAULT);
    interceptedCall.start(listener, new Metadata());
    // Capture the underlying call listener that will receive headers from the transport.

    Metadata inboundHeaders = new Metadata();
    // Simulate that a headers arrives on the underlying call listener.
    call.listener.onHeaders(inboundHeaders);
    assertThat(examinedHeaders).contains(inboundHeaders);
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
    ClientCall<Void, Void> interceptedCall = intercepted.newCall(method, CallOptions.DEFAULT);
    assertNotSame(call, interceptedCall);
    @SuppressWarnings("unchecked")
    ClientCall.Listener<Void> listener = mock(ClientCall.Listener.class);
    Metadata headers = new Metadata();
    interceptedCall.start(listener, headers);
    assertSame(listener, call.listener);
    assertSame(headers, call.headers);
    interceptedCall.sendMessage(null /*request*/);
    assertThat(call.messages).containsExactly((Void) null /*request*/);
    interceptedCall.halfClose();
    assertTrue(call.halfClosed);
    interceptedCall.request(1);
    assertThat(call.requests).containsExactly(1);
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
    ClientCall.Listener<Void> listener = mock(ClientCall.Listener.class);
    ClientCall<Void, Void> interceptedCall = intercepted.newCall(method, CallOptions.DEFAULT);
    assertNotSame(call, interceptedCall);
    interceptedCall.start(listener, new Metadata());
    interceptedCall.sendMessage(null /*request*/);
    interceptedCall.halfClose();
    interceptedCall.request(1);
    call.done = true;
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).onClose(captor.capture(), any(Metadata.class));
    assertSame(error, captor.getValue().getCause());

    // Make sure nothing bad happens after the exception.
    ClientCall<?, ?> noop = ((CheckedForwardingClientCall<?, ?>)interceptedCall).delegate();
    // Should not throw, even on bad input
    noop.cancel("Cancel for test", null);
    noop.start(null, null);
    noop.request(-1);
    noop.halfClose();
    noop.sendMessage(null);
    assertFalse(noop.isReady());
  }

  @Test
  public void authorityIsDelegated() {
    ClientInterceptor interceptor = new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          CallOptions callOptions,
          Channel next) {
        return next.newCall(method, callOptions);
      }
    };

    when(channel.authority()).thenReturn("auth");
    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);
    assertEquals("auth", intercepted.authority());
  }

  @Test
  public void customOptionAccessible() {
    CallOptions.Key<String> customOption = CallOptions.Key.create("custom");
    CallOptions callOptions = CallOptions.DEFAULT.withOption(customOption, "value");
    ArgumentCaptor<CallOptions> passedOptions = ArgumentCaptor.forClass(CallOptions.class);
    ClientInterceptor interceptor =
        mock(ClientInterceptor.class, delegatesTo(new NoopInterceptor()));

    Channel intercepted = ClientInterceptors.intercept(channel, interceptor);

    assertSame(call, intercepted.newCall(method, callOptions));
    verify(channel).newCall(same(method), same(callOptions));

    verify(interceptor).interceptCall(same(method), passedOptions.capture(), isA(Channel.class));
    assertSame("value", passedOptions.getValue().getOption(customOption));
  }

  private static class NoopInterceptor implements ClientInterceptor {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
        CallOptions callOptions, Channel next) {
      return next.newCall(method, callOptions);
    }
  }

  private static class BaseClientCall extends ClientCall<String, Integer> {
    private boolean started;
    private boolean done;
    private ClientCall.Listener<Integer> listener;
    private Metadata headers;
    private List<Integer> requests = new ArrayList<>();
    private List<String> messages = new ArrayList<>();
    private boolean halfClosed;
    private Throwable cancelCause;
    private String cancelMessage;

    @Override
    public void start(ClientCall.Listener<Integer> listener, Metadata headers) {
      checkNotDone();
      started = true;
      this.listener = listener;
      this.headers = headers;
    }

    @Override
    public void request(int numMessages) {
      checkNotDone();
      checkStarted();
      requests.add(numMessages);
    }

    @Override
    public void cancel(String message, Throwable cause) {
      checkNotDone();
      this.cancelMessage = message;
      this.cancelCause = cause;
    }

    @Override
    public void halfClose() {
      checkNotDone();
      checkStarted();
      this.halfClosed = true;
    }

    @Override
    public void sendMessage(String message) {
      checkNotDone();
      checkStarted();
      messages.add(message);
    }

    private void checkNotDone() {
      if (done) {
        throw new IllegalStateException("no more methods should be called");
      }
    }

    private void checkStarted() {
      if (!started) {
        throw new IllegalStateException("should have called start");
      }
    }
  }
}
