package com.google.net.stubby.newtransport;

import com.google.common.util.concurrent.Service;
import com.google.net.stubby.stub.MethodDescriptor;

/**
 * The client-side transport encapsulating a single connection to a remote server. Allows creation
 * of new {@link Stream} instances for communication with the server.
 * <p>
 * Transport life cycle (i.e. startup/shutdown) is exposed via the {@link Service} interface.
 * Observers of the transport life-cycle may be added via {@link Service#addListener}.
 */
public interface ClientTransport extends Service {

  /**
   * Creates a new stream for sending messages to the remote end-point. If the service is already
   * stopped, throws an {@link IllegalStateException}.
   * TODO(user): Consider also throwing for stopping.
   * <p>
   * This method returns immediately and does not wait for any validation of the request. If
   * creation fails for any reason, {@link StreamListener#closed} will be called to provide the
   * error information. Any sent messages for this stream will be buffered until creation has
   * completed (either successfully or unsuccessfully).
   *
   * @param method the descriptor of the remote method to be called for this stream.
   * @param listener the listener for the newly created stream.
   * @return the newly created stream.
   */
  ClientStream newStream(MethodDescriptor<?, ?> method, StreamListener listener);
}
