package com.google.net.stubby.newtransport;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.MethodDescriptor;

/**
 * Abstract base class for all {@link ClientTransport} implementations. Implements the
 * {@link #newStream} method to perform a state check on the service before allowing stream
 * creation.
 */
public abstract class AbstractClientTransport extends AbstractService implements ClientTransport {

  @Override
  public final ClientStream newStream(MethodDescriptor<?, ?> method,
                                      Metadata.Headers headers,
                                      StreamListener listener) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(listener, "listener");
    if (state() == State.STARTING) {
      // Wait until the transport is running before creating the new stream.
      awaitRunning();
    }

    if (state() != State.RUNNING) {
      throw new IllegalStateException("Invalid state for creating new stream: " + state());
    }

    // Create the stream.
    return newStreamInternal(method, headers, listener);
  }

  /**
   * Called by {@link #newStream} to perform the actual creation of the new {@link ClientStream}.
   * This is only called after the transport has successfully transitioned to the {@code RUNNING}
   * state.
   *
   * @param method the RPC method to be invoked on the server by the new stream.
   * @param listener the listener for events on the new stream.
   * @return the new stream.
   */
  protected abstract ClientStream newStreamInternal(MethodDescriptor<?, ?> method,
      Metadata.Headers headers,
      StreamListener listener);
}
