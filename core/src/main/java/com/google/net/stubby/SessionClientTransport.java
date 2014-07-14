package com.google.net.stubby;

import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.StreamListener;

/**
 * Shim between Session and Channel. Will be removed when Session is removed.
 */
public class SessionClientTransport extends AbstractService implements ClientTransport {
  private final Session session;

  public SessionClientTransport(Session session) {
    this.session = session;
  }

  @Override
  protected void doStart() {
    notifyStarted();
  }

  @Override
  public void doStop() {
    notifyStopped();
  }

  @Override
  public ClientStream newStream(MethodDescriptor<?, ?> method,
                                StreamListener listener) {
    final SessionClientStream stream = new SessionClientStream(listener);
    Request request = session.startRequest(method.getName(), method.getHeaders(),
        stream.responseBuilder());
    stream.start(request);
    return stream;
  }
}
