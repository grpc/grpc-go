package com.google.net.stubby;

import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.ClientTransportFactory;
import com.google.net.stubby.newtransport.StreamListener;

/**
 * Shim between Session and Channel. Will be removed when Session is removed.
 *
 * <p>This factory always returns the same instance, which does not adhere to the API.
 */
public class SessionClientTransportFactory implements ClientTransportFactory {
  private final SessionClientTransport transport;

  public SessionClientTransportFactory(Session session) {
    transport = new SessionClientTransport(session);
  }

  @Override
  public ClientTransport newClientTransport() {
    return transport;
  }

  private static class SessionClientTransport extends AbstractService implements ClientTransport {
    private final Session session;

    public SessionClientTransport(Session session) {
      this.session = session;
    }

    @Override
    protected void doStart() {}

    @Override
    public void doStop() {}

    @Override
    public ClientStream newStream(MethodDescriptor<?, ?> method, StreamListener listener) {
      final SessionClientStream stream = new SessionClientStream(listener);
      Request request = session.startRequest(method.getName(), stream.responseBuilder());
      stream.start(request);
      return stream;
    }
  }
}
