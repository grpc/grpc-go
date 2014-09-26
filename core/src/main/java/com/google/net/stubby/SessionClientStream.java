package com.google.net.stubby;

import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientStreamListener;
import com.google.net.stubby.newtransport.StreamState;

import java.io.IOException;
import java.io.InputStream;

/**
 * A temporary shim layer between the new (Channel) and the old (Session). Will go away when the
 * new transport layer is created.
 */
// TODO(user): Delete this class when new transport interfaces are introduced
public class SessionClientStream implements ClientStream {
  private final ClientStreamListener listener;
  /**
   * The {@link Request} used by the stub to dispatch the call
   */
  private Request request;
  private Response response;

  public SessionClientStream(ClientStreamListener listener) {
    this.listener = listener;
  }

  public void start(Request request) {
    this.request = request;
  }

  public Response.ResponseBuilder responseBuilder() {
    return new Response.ResponseBuilder() {
      @Override
      public Response build(int id) {
        response = new SessionResponse(id);
        return response;
      }

      @Override
      public Response build() {
        response = new SessionResponse(-1);
        return response;
      }
    };
  }

  @Override
  public StreamState state() {
    boolean requestOpen = request.getPhase() != Operation.Phase.CLOSED;
    boolean responseOpen = response.getPhase() != Operation.Phase.CLOSED;
    if (requestOpen && responseOpen) {
      return StreamState.OPEN;
    } else if (requestOpen) {
      return StreamState.WRITE_ONLY;
    } else if (responseOpen) {
      return StreamState.READ_ONLY;
    } else {
      return StreamState.CLOSED;
    }
  }

  @Override
  public void halfClose() {
    request.close(Status.OK);
  }

  @Override
  public void writeMessage(InputStream message, int length, Runnable accepted) {
    request.addPayload(message, Operation.Phase.PAYLOAD);
    if (accepted != null) {
      accepted.run();
    }
  }

  @Override
  public void flush() {}

  /**
   * An error occurred while producing the request output. Cancel the request
   * and close the response stream.
   */
  @Override
  public void cancel() {
    request.close(Status.CANCELLED);
  }

  /**
   * Adapts the transport layer response to calls on the response observer or
   * recorded context state.
   */
  private class SessionResponse extends AbstractResponse {

    private SessionResponse(int id) {
      super(id);
    }

    private int available(InputStream is) {
      try {
        return is.available();
      } catch (IOException ex) {
        throw new RuntimeException(ex);
      }
    }

    @Override
    public Operation addPayload(InputStream payload, Phase nextPhase) {
      try {
        listener.messageRead(payload, available(payload));
        return super.addPayload(payload, nextPhase);
      } finally {
        if (getPhase() == Phase.CLOSED) {
          propagateClosed();
        }
      }
    }

    @Override
    public Operation close(Status status) {
      try {
        return super.close(status);
      } finally {
        propagateClosed();
      }
    }

    private void propagateClosed() {
      listener.closed(getStatus(), new Metadata.Trailers());
    }
  }
}
