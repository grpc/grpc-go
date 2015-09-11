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

import io.grpc.ExperimentalApi;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.Status;

/**
 * Utility functions for adapting {@link ServerCallHandler}s to application service implementation,
 * meant to be used by the generated code.
 */
@ExperimentalApi
public class ServerCalls {

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
          MethodDescriptor<ReqT, RespT> methodDescriptor,
          final ServerCall<RespT> call,
          Metadata headers) {
        final ResponseObserver<RespT> responseObserver = new ResponseObserver<RespT>(call);
        // We expect only 1 request, but we ask for 2 requests here so that if a misbehaving client
        // sends more than 1 requests, we will catch it in onMessage() and emit INVALID_ARGUMENT.
        call.request(2);
        return new EmptyServerCallListener<ReqT>() {
          ReqT request;
          @Override
          public void onMessage(ReqT request) {
            if (this.request == null) {
              // We delay calling method.invoke() until onHalfClose(), because application may call
              // close(OK) inside invoke(), while close(OK) is not allowed before onHalfClose().
              this.request = request;
            } else {
              call.close(
                  Status.INVALID_ARGUMENT.withDescription(
                      "More than one request messages for unary call or server streaming call"),
                  new Metadata());
            }
          }

          @Override
          public void onHalfClose() {
            if (request != null) {
              method.invoke(request, responseObserver);
            } else {
              call.close(Status.INVALID_ARGUMENT.withDescription("Half-closed without a request"),
                  new Metadata());
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
          MethodDescriptor<ReqT, RespT> methodDescriptor,
          final ServerCall<RespT> call,
          Metadata headers) {
        call.request(1);
        final ResponseObserver<RespT> responseObserver = new ResponseObserver<RespT>(call);
        final StreamObserver<ReqT> requestObserver = method.invoke(responseObserver);
        return new EmptyServerCallListener<ReqT>() {
          boolean halfClosed = false;

          @Override
          public void onMessage(ReqT request) {
            requestObserver.onNext(request);

            // Request delivery of the next inbound message.
            call.request(1);
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

  private static interface UnaryRequestMethod<ReqT, RespT> {
    void invoke(ReqT request, StreamObserver<RespT> responseObserver);
  }

  private static interface StreamingRequestMethod<ReqT, RespT> {
    StreamObserver<ReqT> invoke(StreamObserver<RespT> responseObserver);
  }

  private static class ResponseObserver<RespT> implements StreamObserver<RespT> {
    final ServerCall<RespT> call;
    volatile boolean cancelled;
    private boolean sentHeaders;

    ResponseObserver(ServerCall<RespT> call) {
      this.call = call;
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
      call.close(Status.fromThrowable(t), new Metadata());
    }

    @Override
    public void onCompleted() {
      if (cancelled) {
        throw Status.CANCELLED.asRuntimeException();
      } else {
        call.close(Status.OK, new Metadata());
      }
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
}
