package com.google.net.stubby.stub;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.ServerCall;
import com.google.net.stubby.ServerCallHandler;
import com.google.net.stubby.ServerMethodDefinition;
import com.google.net.stubby.Status;

/**
 * Utility functions for adapting ServerCallHandlers to application service implementation.
 */
public class ServerCalls {

  private ServerCalls() {
  }

  public static <ReqT, RespT> ServerMethodDefinition<ReqT, RespT> createMethodDefinition(
      Method<ReqT, RespT> method, ServerCallHandler<ReqT, RespT> handler) {
    return ServerMethodDefinition.create(method.getName(), method.getRequestMarshaller(),
        method.getResponseMarshaller(), handler);
  }

  public static <ReqT, RespT> ServerCallHandler<ReqT, RespT> asyncUnaryRequestCall(
      final UnaryRequestMethod<ReqT, RespT> method) {
    return new ServerCallHandler<ReqT, RespT>() {
      @Override
      public ServerCall.Listener<ReqT> startCall(
          String fullMethodName, final ServerCall<RespT> call, Metadata.Headers headers) {
        final ResponseObserver<RespT> responseObserver = new ResponseObserver<RespT>(call);
        return new EmptyServerCallListener<ReqT>() {
          ReqT request;
          @Override
          public ListenableFuture<Void> onPayload(ReqT request) {
            if (this.request == null) {
              // We delay calling method.invoke() until onHalfClose(), because application may call
              // close(OK) inside invoke(), while close(OK) is not allowed before onHalfClose().
              this.request = request;
            } else {
              call.close(
                  Status.INVALID_ARGUMENT.withDescription(
                      "More than one request payloads for unary call or server streaming call"),
                  new Metadata.Trailers());
            }
            return null;
          }

          @Override
          public void onHalfClose() {
            if (request != null) {
              method.invoke(request, responseObserver);
            } else {
              call.close(Status.INVALID_ARGUMENT.withDescription("Half-closed without a request"),
                  new Metadata.Trailers());
            }
          }

          @Override
          public void onCancel() {
            responseObserver.cancelled = true;
          }
        };
      }
    };
  }

  public static <ReqT, RespT> ServerCallHandler<ReqT, RespT> asyncStreamingRequestCall(
      final StreamingRequestMethod<ReqT, RespT> method) {
    return new ServerCallHandler<ReqT, RespT>() {
      @Override
      public ServerCall.Listener<ReqT> startCall(String fullMethodName, ServerCall<RespT> call,
          Metadata.Headers headers) {
        final ResponseObserver<RespT> responseObserver = new ResponseObserver<RespT>(call);
        final StreamObserver<ReqT> requestObserver = method.invoke(responseObserver);
        return new EmptyServerCallListener<ReqT>() {
          boolean halfClosed = false;

          @Override
          public ListenableFuture<Void> onPayload(ReqT request) {
            requestObserver.onValue(request);
            return null;
          }

          @Override
          public void onHalfClose() {
            halfClosed = true;
            requestObserver.onCompleted();
          }

          @Override
          public void onCancel() {
            if (!halfClosed) {
              requestObserver.onError(Status.CANCELLED.asException());
            }
            responseObserver.cancelled = true;
          }
        };
      }
    };
  }

  /**
   * Adaptor to a unary call or server streaming method.
   */
  public static interface UnaryRequestMethod<ReqT, RespT> {
    void invoke(ReqT request, StreamObserver<RespT> responseObserver);
  }

  /**
   * Adaptor to a client stremaing or bi-directional stremaing method.
   */
  public static interface StreamingRequestMethod<ReqT, RespT> {
    StreamObserver<ReqT> invoke(StreamObserver<RespT> responseObserver);
  }

  private static class ResponseObserver<RespT> implements StreamObserver<RespT> {
    final ServerCall<RespT> call;
    volatile boolean cancelled;

    ResponseObserver(ServerCall<RespT> call) {
      this.call = call;
    }

    @Override
    public void onValue(RespT response) {
      if (cancelled) {
        throw Status.CANCELLED.asRuntimeException();
      }
      call.sendPayload(response);
    }

    @Override
    public void onError(Throwable t) {
      call.close(Status.fromThrowable(t), new Metadata.Trailers());
    }

    @Override
    public void onCompleted() {
      if (cancelled) {
        throw Status.CANCELLED.asRuntimeException();
      } else {
        call.close(Status.OK, new Metadata.Trailers());
      }
    }
  }

  private static class EmptyServerCallListener<ReqT> extends ServerCall.Listener<ReqT> {
    @Override
    public ListenableFuture<Void> onPayload(ReqT request) {
      return null;
    }

    @Override
    public void onHalfClose() {
    }

    @Override
    public void onCancel() {
    }

    @Override
    public void onComplete() {
    }
  }
}
