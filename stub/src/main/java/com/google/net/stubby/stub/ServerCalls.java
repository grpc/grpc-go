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

package com.google.net.stubby.stub;

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
        call.request(1);
        return new EmptyServerCallListener<ReqT>() {
          ReqT request;
          @Override
          public void onPayload(ReqT request) {
            if (this.request == null) {
              // We delay calling method.invoke() until onHalfClose(), because application may call
              // close(OK) inside invoke(), while close(OK) is not allowed before onHalfClose().
              this.request = request;

              // Request delivery of the next inbound message.
              call.request(1);
            } else {
              call.close(
                  Status.INVALID_ARGUMENT.withDescription(
                      "More than one request payloads for unary call or server streaming call"),
                  new Metadata.Trailers());
            }
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
      public ServerCall.Listener<ReqT> startCall(String fullMethodName,
          final ServerCall<RespT> call, Metadata.Headers headers) {
        call.request(1);
        final ResponseObserver<RespT> responseObserver = new ResponseObserver<RespT>(call);
        final StreamObserver<ReqT> requestObserver = method.invoke(responseObserver);
        return new EmptyServerCallListener<ReqT>() {
          boolean halfClosed = false;

          @Override
          public void onPayload(ReqT request) {
            requestObserver.onValue(request);

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

      // Request delivery of the next inbound message.
      call.request(1);
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
    public void onPayload(ReqT request) {
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
