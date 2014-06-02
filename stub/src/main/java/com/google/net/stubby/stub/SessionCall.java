package com.google.net.stubby.stub;

import com.google.net.stubby.AbstractResponse;
import com.google.net.stubby.Operation;
import com.google.net.stubby.Request;
import com.google.net.stubby.Response;
import com.google.net.stubby.Session;
import com.google.net.stubby.Status;

import java.io.InputStream;

/**
 * A temporary shim layer between the new (Channel) and the old (Session). Will go away when the
 * new transport layer is created.
 */
// TODO(user): Delete this class when new transport interfaces are introduced
public class SessionCall<RequestT, ResponseT>
    extends CallContext implements Call<RequestT, ResponseT> {

  /**
   * The {@link Request} used by the stub to dispatch the call
   */
  private Request request;

  private StreamObserver<ResponseT> responseObserver;

  private final MethodDescriptor<RequestT, ResponseT> methodDescriptor;
  private final Session session;

  protected SessionCall(MethodDescriptor<RequestT, ResponseT> methodDescriptor, Session session) {
    // This will go away when we introduce new transport API.... nothing to see here
    this.methodDescriptor = methodDescriptor;
    this.session = session;
  }

  @Override
  public void start(StreamObserver<ResponseT> responseObserver) {
    request = session.startRequest(methodDescriptor.getName(), new Response.ResponseBuilder() {
      @Override
      public Response build(int id) {
        return new CallResponse(id);
      }

      @Override
      public Response build() {
        return new CallResponse(-1);
      }
    });
    this.responseObserver = responseObserver;
  }

  @Override
  public void onValue(RequestT value) {
    request.addPayload(methodDescriptor.streamRequest(value), Operation.Phase.PAYLOAD);
  }

  /**
   * An error occurred while producing the request output. Cancel the request
   * and close the response stream.
   */
  @Override
  public void onError(Throwable t) {
    request.close(Status.fromThrowable(t));
    this.responseObserver.onError(t);
  }

  @Override
  public void onCompleted() {
    request.close(Status.OK);
  }

  /**
   * Adapts the transport layer response to calls on the response observer or
   * recorded context state.
   */
  private class CallResponse extends AbstractResponse {

    private CallResponse(int id) {
      super(id);
    }

    @Override
    public Operation addContext(String type, InputStream message, Phase nextPhase) {
      try {
        SessionCall.this.addResponseContext(type, message);
        return super.addContext(type, message, nextPhase);
      } finally {
        if (getPhase() == Phase.CLOSED) {
          propagateClosed();
        }
      }
    }

    @Override
    public Operation addPayload(InputStream payload, Phase nextPhase) {
      try {
        SessionCall.this.responseObserver.onValue(methodDescriptor.parseResponse(payload));
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
      if (Status.OK.getCode() == getStatus().getCode()) {
        SessionCall.this.responseObserver.onCompleted();
      } else {
        SessionCall.this.responseObserver.onError(getStatus().asRuntimeException());
      }
    }
  }
}
