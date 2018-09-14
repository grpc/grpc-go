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

import com.google.common.base.Preconditions;
import io.grpc.MethodDescriptor.Marshaller;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/**
 * Utility methods for working with {@link ClientInterceptor}s.
 */
public class ClientInterceptors {

  // Prevent instantiation
  private ClientInterceptors() {}

  /**
   * Create a new {@link Channel} that will call {@code interceptors} before starting a call on the
   * given channel. The first interceptor will have its {@link ClientInterceptor#interceptCall}
   * called first.
   *
   * @param channel the underlying channel to intercept.
   * @param interceptors array of interceptors to bind to {@code channel}.
   * @return a new channel instance with the interceptors applied.
   */
  public static Channel interceptForward(Channel channel, ClientInterceptor... interceptors) {
    return interceptForward(channel, Arrays.asList(interceptors));
  }

  /**
   * Create a new {@link Channel} that will call {@code interceptors} before starting a call on the
   * given channel. The first interceptor will have its {@link ClientInterceptor#interceptCall}
   * called first.
   *
   * @param channel the underlying channel to intercept.
   * @param interceptors a list of interceptors to bind to {@code channel}.
   * @return a new channel instance with the interceptors applied.
   */
  public static Channel interceptForward(Channel channel,
                                         List<? extends ClientInterceptor> interceptors) {
    List<? extends ClientInterceptor> copy = new ArrayList<>(interceptors);
    Collections.reverse(copy);
    return intercept(channel, copy);
  }

  /**
   * Create a new {@link Channel} that will call {@code interceptors} before starting a call on the
   * given channel. The last interceptor will have its {@link ClientInterceptor#interceptCall}
   * called first.
   *
   * @param channel the underlying channel to intercept.
   * @param interceptors array of interceptors to bind to {@code channel}.
   * @return a new channel instance with the interceptors applied.
   */
  public static Channel intercept(Channel channel, ClientInterceptor... interceptors) {
    return intercept(channel, Arrays.asList(interceptors));
  }

  /**
   * Create a new {@link Channel} that will call {@code interceptors} before starting a call on the
   * given channel. The last interceptor will have its {@link ClientInterceptor#interceptCall}
   * called first.
   *
   * @param channel the underlying channel to intercept.
   * @param interceptors a list of interceptors to bind to {@code channel}.
   * @return a new channel instance with the interceptors applied.
   */
  public static Channel intercept(Channel channel, List<? extends ClientInterceptor> interceptors) {
    Preconditions.checkNotNull(channel, "channel");
    for (ClientInterceptor interceptor : interceptors) {
      channel = new InterceptorChannel(channel, interceptor);
    }
    return channel;
  }

  /**
   * Creates a new ClientInterceptor that transforms requests into {@code WReqT} and responses into
   * {@code WRespT} before passing them into the {@code interceptor}.
   */
  static <WReqT, WRespT> ClientInterceptor wrapClientInterceptor(
      final ClientInterceptor interceptor,
      final Marshaller<WReqT> reqMarshaller,
      final Marshaller<WRespT> respMarshaller) {
    return new ClientInterceptor() {
      @Override
      public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          final MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
        final MethodDescriptor<WReqT, WRespT> wrappedMethod =
            method.toBuilder(reqMarshaller, respMarshaller).build();
        final ClientCall<WReqT, WRespT> wrappedCall =
            interceptor.interceptCall(wrappedMethod, callOptions, next);
        return new PartialForwardingClientCall<ReqT, RespT>() {
          @Override
          public void start(final Listener<RespT> responseListener, Metadata headers) {
            wrappedCall.start(new PartialForwardingClientCallListener<WRespT>() {
              @Override
              public void onMessage(WRespT wMessage) {
                InputStream bytes = respMarshaller.stream(wMessage);
                RespT message = method.getResponseMarshaller().parse(bytes);
                responseListener.onMessage(message);
              }

              @Override
              protected Listener<?> delegate() {
                return responseListener;
              }
            }, headers);
          }

          @Override
          public void sendMessage(ReqT message) {
            InputStream bytes = method.getRequestMarshaller().stream(message);
            WReqT wReq = reqMarshaller.parse(bytes);
            wrappedCall.sendMessage(wReq);
          }

          @Override
          protected ClientCall<?, ?> delegate() {
            return wrappedCall;
          }
        };
      }
    };
  }

  private static class InterceptorChannel extends Channel {
    private final Channel channel;
    private final ClientInterceptor interceptor;

    private InterceptorChannel(Channel channel, ClientInterceptor interceptor) {
      this.channel = channel;
      this.interceptor = Preconditions.checkNotNull(interceptor, "interceptor");
    }

    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(
        MethodDescriptor<ReqT, RespT> method, CallOptions callOptions) {
      return interceptor.interceptCall(method, callOptions, channel);
    }

    @Override
    public String authority() {
      return channel.authority();
    }
  }

  private static final ClientCall<Object, Object> NOOP_CALL = new ClientCall<Object, Object>() {
    @Override
    public void start(Listener<Object> responseListener, Metadata headers) {}

    @Override
    public void request(int numMessages) {}

    @Override
    public void cancel(String message, Throwable cause) {}

    @Override
    public void halfClose() {}

    @Override
    public void sendMessage(Object message) {}

    /**
     * Always returns {@code false}, since this is only used when the startup of the {@link
     * ClientCall} fails (i.e. the {@link ClientCall} is closed).
     */
    @Override
    public boolean isReady() {
      return false;
    }
  };

  /**
   * A {@link io.grpc.ForwardingClientCall} that delivers exceptions from its start logic to the
   * call listener.
   *
   * <p>{@link ClientCall#start(ClientCall.Listener, Metadata)} should not throw any
   * exception other than those caused by misuse, e.g., {@link IllegalStateException}.  {@code
   * CheckedForwardingClientCall} provides {@code checkedStart()} in which throwing exceptions is
   * allowed.
   */
  public abstract static class CheckedForwardingClientCall<ReqT, RespT>
      extends io.grpc.ForwardingClientCall<ReqT, RespT> {

    private ClientCall<ReqT, RespT> delegate;

    /**
     * Subclasses implement the start logic here that would normally belong to {@code start()}.
     *
     * <p>Implementation should call {@code this.delegate().start()} in the normal path. Exceptions
     * may safely be thrown prior to calling {@code this.delegate().start()}. Such exceptions will
     * be handled by {@code CheckedForwardingClientCall} and be delivered to {@code
     * responseListener}.  Exceptions <em>must not</em> be thrown after calling {@code
     * this.delegate().start()}, as this can result in {@link ClientCall.Listener#onClose} being
     * called multiple times.
     */
    protected abstract void checkedStart(Listener<RespT> responseListener, Metadata headers)
        throws Exception;

    protected CheckedForwardingClientCall(ClientCall<ReqT, RespT> delegate) {
      this.delegate = delegate;
    }

    @Override
    protected final ClientCall<ReqT, RespT> delegate() {
      return delegate;
    }

    @Override
    @SuppressWarnings("unchecked")
    public final void start(Listener<RespT> responseListener, Metadata headers) {
      try {
        checkedStart(responseListener, headers);
      } catch (Exception e) {
        // Because start() doesn't throw, the caller may still try to call other methods on this
        // call object. Passing these invocations to the original delegate will cause
        // IllegalStateException because delegate().start() was not called. We switch the delegate
        // to a NO-OP one to prevent the IllegalStateException. The user will finally get notified
        // about the error through the listener.
        delegate = (ClientCall<ReqT, RespT>) NOOP_CALL;
        responseListener.onClose(Status.fromThrowable(e), new Metadata());
      }
    }
  }
}
