package com.google.net.stubby.newtransport;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Status;

import java.io.InputStream;

/**
 * A decorator around another {@link StreamListener}.
 */
public class ForwardingStreamListener implements StreamListener {

  private final StreamListener delegate;

  public ForwardingStreamListener(StreamListener delegate) {
    this.delegate = delegate;
  }

  @Override
  public ListenableFuture<Void> contextRead(String name, InputStream value, int length) {
    return delegate.contextRead(name, value, length);
  }

  @Override
  public ListenableFuture<Void> messageRead(InputStream message, int length) {
    return delegate.messageRead(message, length);
  }

  @Override
  public void closed(Status status) {
    delegate.closed(status);
  }
}
