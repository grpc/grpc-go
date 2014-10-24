package com.google.net.stubby.context;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.Call;
import com.google.net.stubby.Channel;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

import javax.annotation.Nullable;

/**
 * A {@link Channel} which forwards all of it's methods to another {@link Channel}. Implementations
 * should override methods and make use of {@link ForwardingListener} and {@link ForwardingCall}
 * to augment the behavior of the underlying {@link Channel}.
 */
public abstract class ForwardingChannel implements Channel {

  protected final Channel delegate;

  public ForwardingChannel(Channel channel) {
    this.delegate = channel;
  }

  /**
   * A {@link Call} which forwards all of it's methods to another {@link Call}.
   */
  public static class ForwardingCall<RequestT,ResponseT> extends Call<RequestT,ResponseT> {

    protected final Call<RequestT, ResponseT> delegate;

    public ForwardingCall(Call<RequestT, ResponseT> delegate) {
      this.delegate = delegate;
    }

    @Override
    public void start(Listener<ResponseT> responseListener, Metadata.Headers headers) {
      this.delegate.start(responseListener, headers);
    }

    @Override
    public void cancel() {
      this.delegate.cancel();
    }

    @Override
    public void halfClose() {
      this.delegate.halfClose();
    }

    @Override
    public void sendPayload(RequestT payload, @Nullable SettableFuture<Void> accepted) {
      this.delegate.sendPayload(payload, accepted);
    }
  }

  /**
   * A {@link com.google.net.stubby.Call.Listener} which forwards all of its methods to another
   * {@link com.google.net.stubby.Call.Listener}.
   */
  public static class ForwardingListener<T> extends Call.Listener<T> {

    Call.Listener<T> delegate;

    public ForwardingListener(Call.Listener<T> delegate) {
      this.delegate = delegate;
    }

    @Override
    public ListenableFuture<Void> onHeaders(Metadata.Headers headers) {
      return delegate.onHeaders(headers);
    }

    @Override
    public ListenableFuture<Void> onPayload(T payload) {
      return delegate.onPayload(payload);
    }

    @Override
    public void onClose(Status status, Metadata.Trailers trailers) {
      delegate.onClose(status, trailers);
    }
  }
}
