/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.stub;

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.Status;

/**
 * Utility functions for adapting {@link ServerCallHandler}s to application service implementation,
 * meant to be used by the generated code.
 */
public final class ServerCalls {

  private ServerCalls() {
  }

  /**
   * Creates a {@code ServerCallHandler} for a unary call method of the service.
   *
   * @param method an adaptor to the actual method on the service implementation.
   */
  public static <ReqT, RespT> ServerCallHandler<ReqT, RespT> asyncUnaryCall(
      final UnaryMethod<ReqT, RespT> method) {
    return asyncUnaryRequestCall(method);
  }

  /**
   * Creates a {@code ServerCallHandler} for a server streaming method of the service.
   *
   * @param method an adaptor to the actual method on the service implementation.
   */
  public static <ReqT, RespT> ServerCallHandler<ReqT, RespT> asyncServerStreamingCall(
      final ServerStreamingMethod<ReqT, RespT> method) {
    return asyncUnaryRequestCall(method);
  }

  /**
   * Creates a {@code ServerCallHandler} for a client streaming method of the service.
   *
   * @param method an adaptor to the actual method on the service implementation.
   */
  public static <ReqT, RespT> ServerCallHandler<ReqT, RespT> asyncClientStreamingCall(
      final ClientStreamingMethod<ReqT, RespT> method) {
    return asyncStreamingRequestCall(method);
  }

  /**
   * Creates a {@code ServerCallHandler} for a bidi streaming method of the service.
   *
   * @param method an adaptor to the actual method on the service implementation.
   */
  public static <ReqT, RespT> ServerCallHandler<ReqT, RespT> asyncBidiStreamingCall(
      final BidiStreamingMethod<ReqT, RespT> method) {
    return asyncStreamingRequestCall(method);
  }

  /**
   * Adaptor to a unary call method.
   */
  public static interface UnaryMethod<ReqT, RespT> extends UnaryRequestMethod<ReqT, RespT> {
  }

  /**
   * Adaptor to a server streaming method.
   */
  public static interface ServerStreamingMethod<ReqT, RespT>
      extends UnaryRequestMethod<ReqT, RespT> {
  }

  /**
   * Adaptor to a client streaming method.
   */
  public static interface ClientStreamingMethod<ReqT, RespT>
      extends StreamingRequestMethod<ReqT, RespT> {
  }

  /**
   * Adaptor to a bi-directional streaming method.
   */
  public static interface BidiStreamingMethod<ReqT, RespT>
      extends StreamingRequestMethod<ReqT, RespT> {
  }

  /**
   * Creates a {@code ServerCallHandler} for a unary request call method of the service.
   *
   * @param method an adaptor to the actual method on the service implementation.
   */
  private static <ReqT, RespT> ServerCallHandler<ReqT, RespT> asyncUnaryRequestCall(
      final UnaryRequestMethod<ReqT, RespT> method) {
    return new ServerCallHandler<ReqT, RespT>() {
      @Override
      public ServerCall.Listener<ReqT> startCall(
          final ServerCall<ReqT, RespT> call,
          Metadata headers) {
        final ServerCallStreamObserverImpl<ReqT, RespT> responseObserver =
            new ServerCallStreamObserverImpl<ReqT, RespT>(call);
        // We expect only 1 request, but we ask for 2 requests here so that if a misbehaving client
        // sends more than 1 requests, ServerCall will catch it. Note that disabling auto
        // inbound flow control has no effect on unary calls.
        call.request(2);
        return new EmptyServerCallListener<ReqT>() {
          ReqT request;
          @Override
          public void onMessage(ReqT request) {
            // We delay calling method.invoke() until onHalfClose() to make sure the client
            // half-closes.
            this.request = request;
          }

          @Override
          public void onHalfClose() {
            if (request != null) {
              method.invoke(request, responseObserver);
              responseObserver.freeze();
              if (call.isReady()) {
                // Since we are calling invoke in halfClose we have missed the onReady
                // event from the transport so recover it here.
                onReady();
              }
            } else {
              call.close(Status.INTERNAL.withDescription("Half-closed without a request"),
                  new Metadata());
            }
          }

          @Override
          public void onCancel() {
            responseObserver.cancelled = true;
            if (responseObserver.onCancelHandler != null) {
              responseObserver.onCancelHandler.run();
            }
          }

          @Override
          public void onReady() {
            if (responseObserver.onReadyHandler != null) {
              responseObserver.onReadyHandler.run();
            }
          }
        };
      }
    };
  }

  /**
   * Creates a {@code ServerCallHandler} for a streaming request call method of the service.
   *
   * @param method an adaptor to the actual method on the service implementation.
   */
  private static <ReqT, RespT> ServerCallHandler<ReqT, RespT> asyncStreamingRequestCall(
      final StreamingRequestMethod<ReqT, RespT> method) {
    return new ServerCallHandler<ReqT, RespT>() {
      @Override
      public ServerCall.Listener<ReqT> startCall(
          final ServerCall<ReqT, RespT> call,
          Metadata headers) {
        final ServerCallStreamObserverImpl<ReqT, RespT> responseObserver =
            new ServerCallStreamObserverImpl<ReqT, RespT>(call);
        final StreamObserver<ReqT> requestObserver = method.invoke(responseObserver);
        responseObserver.freeze();
        if (responseObserver.autoFlowControlEnabled) {
          call.request(1);
        }
        return new EmptyServerCallListener<ReqT>() {
          boolean halfClosed = false;

          @Override
          public void onMessage(ReqT request) {
            requestObserver.onNext(request);

            // Request delivery of the next inbound message.
            if (responseObserver.autoFlowControlEnabled) {
              call.request(1);
            }
          }

          @Override
          public void onHalfClose() {
            halfClosed = true;
            requestObserver.onCompleted();
          }

          @Override
          public void onCancel() {
            responseObserver.cancelled = true;
            if (responseObserver.onCancelHandler != null) {
              responseObserver.onCancelHandler.run();
            }
            if (!halfClosed) {
              requestObserver.onError(Status.CANCELLED.asException());
            }
          }

          @Override
          public void onReady() {
            if (responseObserver.onReadyHandler != null) {
              responseObserver.onReadyHandler.run();
            }
          }
        };
      }
    };
  }

  private static interface UnaryRequestMethod<ReqT, RespT> {
    void invoke(ReqT request, StreamObserver<RespT> responseObserver);
  }

  private static interface StreamingRequestMethod<ReqT, RespT> {
    StreamObserver<ReqT> invoke(StreamObserver<RespT> responseObserver);
  }

  private static final class ServerCallStreamObserverImpl<ReqT, RespT>
      extends ServerCallStreamObserver<RespT> {
    final ServerCall<ReqT, RespT> call;
    volatile boolean cancelled;
    private boolean frozen;
    private boolean autoFlowControlEnabled = true;
    private boolean sentHeaders;
    private Runnable onReadyHandler;
    private Runnable onCancelHandler;

    ServerCallStreamObserverImpl(ServerCall<ReqT, RespT> call) {
      this.call = call;
    }

    private void freeze() {
      this.frozen = true;
    }

    @Override
    public void setMessageCompression(boolean enable) {
      call.setMessageCompression(enable);
    }

    @Override
    public void setCompression(String compression) {
      call.setCompression(compression);
    }

    @Override
    public void onNext(RespT response) {
      if (cancelled) {
        throw Status.CANCELLED.asRuntimeException();
      }
      if (!sentHeaders) {
        call.sendHeaders(new Metadata());
        sentHeaders = true;
      }
      call.sendMessage(response);
    }

    @Override
    public void onError(Throwable t) {
      Metadata metadata = Status.trailersFromThrowable(t);
      if (metadata == null) {
        metadata = new Metadata();
      }
      call.close(Status.fromThrowable(t), metadata);
    }

    @Override
    public void onCompleted() {
      if (cancelled) {
        throw Status.CANCELLED.asRuntimeException();
      } else {
        call.close(Status.OK, new Metadata());
      }
    }

    @Override
    public boolean isReady() {
      return call.isReady();
    }

    @Override
    public void setOnReadyHandler(Runnable r) {
      if (frozen) {
        throw new IllegalStateException("Cannot alter onReadyHandler after initialization");
      }
      this.onReadyHandler = r;
    }

    @Override
    public boolean isCancelled() {
      return call.isCancelled();
    }

    @Override
    public void setOnCancelHandler(Runnable onCancelHandler) {
      if (frozen) {
        throw new IllegalStateException("Cannot alter onCancelHandler after initialization");
      }
      this.onCancelHandler = onCancelHandler;
    }

    @Override
    public void disableAutoInboundFlowControl() {
      if (frozen) {
        throw new IllegalStateException("Cannot disable auto flow control after initialization");
      } else {
        autoFlowControlEnabled = false;
      }
    }

    @Override
    public void request(int count) {
      call.request(count);
    }
  }

  private static class EmptyServerCallListener<ReqT> extends ServerCall.Listener<ReqT> {
    @Override
    public void onMessage(ReqT request) {
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

  /**
   * Sets unimplemented status for method on given response stream for unary call.
   *
   * @param methodDescriptor of method for which error will be thrown.
   * @param responseObserver on which error will be set.
   */
  public static void asyncUnimplementedUnaryCall(MethodDescriptor<?, ?> methodDescriptor,
      StreamObserver<?> responseObserver) {
    checkNotNull(methodDescriptor, "methodDescriptor");
    checkNotNull(responseObserver, "responseObserver");
    responseObserver.onError(Status.UNIMPLEMENTED
        .withDescription(String.format("Method %s is unimplemented",
            methodDescriptor.getFullMethodName()))
        .asException());
  }

  /**
   * Sets unimplemented status for streaming call.
   *
   * @param methodDescriptor of method for which error will be thrown.
   * @param responseObserver on which error will be set.
   */
  public static <T> StreamObserver<T> asyncUnimplementedStreamingCall(
      MethodDescriptor<?, ?> methodDescriptor, StreamObserver<?> responseObserver) {
    // NB: For streaming call we want to do the same as for unary call. Fail-fast by setting error
    // on responseObserver and then return no-op observer.
    asyncUnimplementedUnaryCall(methodDescriptor, responseObserver);
    return new NoopStreamObserver<T>();
  }

  /**
   * No-op implementation of StreamObserver. Used in abstract stubs for default implementations of
   * methods which throws UNIMPLEMENTED error and tests.
   */
  static class NoopStreamObserver<V> implements StreamObserver<V> {
    @Override
    public void onNext(V value) {
    }

    @Override
    public void onError(Throwable t) {
    }

    @Override
    public void onCompleted() {
    }
  }
}
