/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.testing.integration;

import com.google.common.base.Preconditions;
import com.google.common.collect.Queues;
import com.google.protobuf.ByteString;
import io.grpc.ForwardingServerCall.SimpleForwardingServerCall;
import io.grpc.Metadata;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.Status;
import io.grpc.internal.LogExceptionRunnable;
import io.grpc.stub.ServerCallStreamObserver;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.integration.Messages.Payload;
import io.grpc.testing.integration.Messages.ResponseParameters;
import io.grpc.testing.integration.Messages.SimpleRequest;
import io.grpc.testing.integration.Messages.SimpleResponse;
import io.grpc.testing.integration.Messages.StreamingInputCallRequest;
import io.grpc.testing.integration.Messages.StreamingInputCallResponse;
import io.grpc.testing.integration.Messages.StreamingOutputCallRequest;
import io.grpc.testing.integration.Messages.StreamingOutputCallResponse;
import java.util.ArrayDeque;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Queue;
import java.util.Random;
import java.util.Set;
import java.util.concurrent.Future;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import javax.annotation.concurrent.GuardedBy;

/**
 * Implementation of the business logic for the TestService. Uses an executor to schedule chunks
 * sent in response streams.
 */
public class TestServiceImpl extends TestServiceGrpc.TestServiceImplBase {
  private final Random random = new Random();

  private final ScheduledExecutorService executor;
  private final ByteString compressableBuffer;

  /**
   * Constructs a controller using the given executor for scheduling response stream chunks.
   */
  public TestServiceImpl(ScheduledExecutorService executor) {
    this.executor = executor;
    this.compressableBuffer = ByteString.copyFrom(new byte[1024]);
  }

  @Override
  public void emptyCall(EmptyProtos.Empty request,
      StreamObserver<EmptyProtos.Empty> responseObserver) {
    responseObserver.onNext(EmptyProtos.Empty.getDefaultInstance());
    responseObserver.onCompleted();
  }

  /**
   * Immediately responds with a payload of the type and size specified in the request.
   */
  @Override
  public void unaryCall(SimpleRequest req, StreamObserver<SimpleResponse> responseObserver) {
    ServerCallStreamObserver<SimpleResponse> obs =
        (ServerCallStreamObserver<SimpleResponse>) responseObserver;
    SimpleResponse.Builder responseBuilder = SimpleResponse.newBuilder();
    try {
      if (req.hasResponseCompressed() && req.getResponseCompressed().getValue()) {
        obs.setCompression("gzip");
      } else {
        obs.setCompression("identity");
      }
    } catch (IllegalArgumentException e) {
      obs.onError(Status.UNIMPLEMENTED
          .withDescription("compression not supported.")
          .withCause(e)
          .asRuntimeException());
      return;
    }

    if (req.getResponseSize() != 0) {
      // For consistency with the c++ TestServiceImpl, use a random offset for unary calls.
      // TODO(wonderfly): whether or not this is a good approach needs further discussion.
      int offset = random.nextInt(compressableBuffer.size());
      ByteString payload = generatePayload(compressableBuffer, offset, req.getResponseSize());
      responseBuilder.setPayload(
          Payload.newBuilder()
              .setBody(payload));
    }

    if (req.hasResponseStatus()) {
      obs.onError(Status.fromCodeValue(req.getResponseStatus().getCode())
          .withDescription(req.getResponseStatus().getMessage())
          .asRuntimeException());
      return;
    }

    responseObserver.onNext(responseBuilder.build());
    responseObserver.onCompleted();
  }

  /**
   * Given a request that specifies chunk size and interval between responses, creates and schedules
   * the response stream.
   */
  @Override
  public void streamingOutputCall(StreamingOutputCallRequest request,
      StreamObserver<StreamingOutputCallResponse> responseObserver) {
    // Create and start the response dispatcher.
    new ResponseDispatcher(responseObserver).enqueue(toChunkQueue(request)).completeInput();
  }

  /**
   * Waits until we have received all of the request messages and then returns the aggregate payload
   * size for all of the received requests.
   */
  @Override
  public StreamObserver<Messages.StreamingInputCallRequest> streamingInputCall(
      final StreamObserver<Messages.StreamingInputCallResponse> responseObserver) {
    return new StreamObserver<StreamingInputCallRequest>() {
      private int totalPayloadSize;

      @Override
      public void onNext(StreamingInputCallRequest message) {
        totalPayloadSize += message.getPayload().getBody().size();
      }

      @Override
      public void onCompleted() {
        responseObserver.onNext(StreamingInputCallResponse.newBuilder()
            .setAggregatedPayloadSize(totalPayloadSize).build());
        responseObserver.onCompleted();
      }

      @Override
      public void onError(Throwable cause) {
        responseObserver.onError(cause);
      }
    };
  }

  /**
   * True bi-directional streaming. Processes requests as they come in. Begins streaming results
   * immediately.
   */
  @Override
  public StreamObserver<Messages.StreamingOutputCallRequest> fullDuplexCall(
      final StreamObserver<Messages.StreamingOutputCallResponse> responseObserver) {
    final ResponseDispatcher dispatcher = new ResponseDispatcher(responseObserver);
    return new StreamObserver<StreamingOutputCallRequest>() {
      @Override
      public void onNext(StreamingOutputCallRequest request) {
        if (request.hasResponseStatus()) {
          dispatcher.cancel();
          dispatcher.onError(Status.fromCodeValue(request.getResponseStatus().getCode())
              .withDescription(request.getResponseStatus().getMessage())
              .asRuntimeException());
          return;
        }
        dispatcher.enqueue(toChunkQueue(request));
      }

      @Override
      public void onCompleted() {
        if (!dispatcher.isCancelled()) {
          // Tell the dispatcher that all input has been received.
          dispatcher.completeInput();
        }
      }

      @Override
      public void onError(Throwable cause) {
        dispatcher.onError(cause);
      }
    };
  }

  /**
   * Similar to {@link #fullDuplexCall}, except that it waits for all streaming requests to be
   * received before starting the streaming responses.
   */
  @Override
  public StreamObserver<Messages.StreamingOutputCallRequest> halfDuplexCall(
      final StreamObserver<Messages.StreamingOutputCallResponse> responseObserver) {
    final ResponseDispatcher dispatcher = new ResponseDispatcher(responseObserver);
    final Queue<Chunk> chunks = new ArrayDeque<>();
    return new StreamObserver<StreamingOutputCallRequest>() {
      @Override
      public void onNext(StreamingOutputCallRequest request) {
        chunks.addAll(toChunkQueue(request));
      }

      @Override
      public void onCompleted() {
        // Dispatch all of the chunks in one shot.
        dispatcher.enqueue(chunks).completeInput();
      }

      @Override
      public void onError(Throwable cause) {
        dispatcher.onError(cause);
      }
    };
  }

  /**
   * Schedules the dispatch of a queue of chunks. Whenever chunks are added or input is completed,
   * the next response chunk is scheduled for delivery to the client. When no more chunks are
   * available, the stream is half-closed.
   */
  private class ResponseDispatcher {
    private final Chunk completionChunk = new Chunk(0, 0, 0);
    private final Queue<Chunk> chunks;
    private final StreamObserver<StreamingOutputCallResponse> responseStream;
    private boolean scheduled;
    @GuardedBy("this") private boolean cancelled;
    private Throwable failure;
    private Runnable dispatchTask = new Runnable() {
      @Override
      @SuppressWarnings("CatchAndPrintStackTrace")
      public void run() {
        try {

          // Dispatch the current chunk to the client.
          try {
            dispatchChunk();
          } catch (RuntimeException e) {
            // Indicate that nothing is scheduled and re-throw.
            synchronized (ResponseDispatcher.this) {
              scheduled = false;
            }
            throw e;
          }

          // Schedule the next chunk if there is one.
          synchronized (ResponseDispatcher.this) {
            // Indicate that nothing is scheduled.
            scheduled = false;
            scheduleNextChunk();
          }
        } catch (Throwable t) {
          t.printStackTrace();
        }
      }
    };

    /**
     * The {@link StreamObserver} will be used to send the queue of response chunks. Since calls to
     * {@link StreamObserver} must be synchronized across threads, no further calls should be made
     * directly on {@code responseStream} after it is provided to the {@link ResponseDispatcher}.
     */
    public ResponseDispatcher(StreamObserver<StreamingOutputCallResponse> responseStream) {
      this.chunks = Queues.newLinkedBlockingQueue();
      this.responseStream = responseStream;
    }

    /**
     * Adds the given chunks to the response stream and schedules the next chunk to be delivered if
     * needed.
     */
    public synchronized ResponseDispatcher enqueue(Queue<Chunk> moreChunks) {
      assertNotFailed();
      chunks.addAll(moreChunks);
      scheduleNextChunk();
      return this;
    }

    /**
     * Indicates that the input is completed and the currently enqueued response chunks are all that
     * remain to be scheduled for dispatch to the client.
     */
    public ResponseDispatcher completeInput() {
      assertNotFailed();
      chunks.add(completionChunk);
      scheduleNextChunk();
      return this;
    }

    /**
     * Allows the service to cancel the remaining responses.
     */
    public synchronized void cancel() {
      Preconditions.checkState(!cancelled, "Dispatcher already cancelled");
      chunks.clear();
      cancelled = true;
    }

    public synchronized boolean isCancelled() {
      return cancelled;
    }

    private synchronized void onError(Throwable cause) {
      responseStream.onError(cause);
    }

    /**
     * Dispatches the current response chunk to the client. This is only called by the executor. At
     * any time, a given dispatch task should only be registered with the executor once.
     */
    private synchronized void dispatchChunk() {
      if (cancelled) {
        return;
      }
      try {
        // Pop off the next chunk and send it to the client.
        Chunk chunk = chunks.remove();
        if (chunk == completionChunk) {
          responseStream.onCompleted();
        } else {
          responseStream.onNext(chunk.toResponse());
        }
      } catch (Throwable e) {
        failure = e;
        if (Status.fromThrowable(e).getCode() == Status.CANCELLED.getCode()) {
          // Stream was cancelled by client, responseStream.onError() might be called already or
          // will be called soon by inbounding StreamObserver.
          chunks.clear();
        } else {
          responseStream.onError(e);
        }
      }
    }

    /**
     * Schedules the next response chunk to be dispatched. If all input has been received and there
     * are no more chunks in the queue, the stream is closed.
     */
    private void scheduleNextChunk() {
      synchronized (this) {
        if (scheduled) {
          // Dispatch task is already scheduled.
          return;
        }

        // Schedule the next response chunk if there is one.
        Chunk nextChunk = chunks.peek();
        if (nextChunk != null) {
          scheduled = true;
          // TODO(ejona): cancel future if RPC is cancelled
          Future<?> unused = executor.schedule(new LogExceptionRunnable(dispatchTask),
              nextChunk.delayMicroseconds, TimeUnit.MICROSECONDS);
          return;
        }
      }
    }

    private void assertNotFailed() {
      if (failure != null) {
        throw new IllegalStateException("Stream already failed", failure);
      }
    }
  }

  /**
   * Breaks down the request and creates a queue of response chunks for the given request.
   */
  public Queue<Chunk> toChunkQueue(StreamingOutputCallRequest request) {
    Queue<Chunk> chunkQueue = new ArrayDeque<>();
    int offset = 0;
    for (ResponseParameters params : request.getResponseParametersList()) {
      chunkQueue.add(new Chunk(params.getIntervalUs(), offset, params.getSize()));

      // Increment the offset past this chunk. Buffer need to be circular.
      offset = (offset + params.getSize()) % compressableBuffer.size();
    }
    return chunkQueue;
  }

  /**
   * A single chunk of a response stream. Contains delivery information for the dispatcher and can
   * be converted to a streaming response proto. A chunk just references it's payload in the
   * {@link #compressableBuffer} array. The payload isn't actually created until {@link
   * #toResponse()} is called.
   */
  private class Chunk {
    private final int delayMicroseconds;
    private final int offset;
    private final int length;

    public Chunk(int delayMicroseconds, int offset, int length) {
      this.delayMicroseconds = delayMicroseconds;
      this.offset = offset;
      this.length = length;
    }

    /**
     * Convert this chunk into a streaming response proto.
     */
    private StreamingOutputCallResponse toResponse() {
      StreamingOutputCallResponse.Builder responseBuilder =
          StreamingOutputCallResponse.newBuilder();
      ByteString payload = generatePayload(compressableBuffer, offset, length);
      responseBuilder.setPayload(
          Payload.newBuilder()
              .setBody(payload));
      return responseBuilder.build();
    }
  }

  /**
   * Generates a payload of desired type and size. Reads compressableBuffer or
   * uncompressableBuffer as a circular buffer.
   */
  private ByteString generatePayload(ByteString dataBuffer, int offset, int size) {
    ByteString payload = ByteString.EMPTY;
    // This offset would never pass the array boundary.
    int begin = offset;
    int end = 0;
    int bytesLeft = size;
    while (bytesLeft > 0) {
      end = Math.min(begin + bytesLeft, dataBuffer.size());
      // ByteString.substring returns the substring from begin, inclusive, to end, exclusive.
      payload = payload.concat(dataBuffer.substring(begin, end));
      bytesLeft -= (end - begin);
      begin = end % dataBuffer.size();
    }
    return payload;
  }

  /** Returns interceptors necessary for full service implementation. */
  public static List<ServerInterceptor> interceptors() {
    return Arrays.asList(
        echoRequestHeadersInterceptor(Util.METADATA_KEY),
        echoRequestMetadataInHeaders(Util.ECHO_INITIAL_METADATA_KEY),
        echoRequestMetadataInTrailers(Util.ECHO_TRAILING_METADATA_KEY));
  }

  /**
   * Echo the request headers from a client into response headers and trailers. Useful for
   * testing end-to-end metadata propagation.
   */
  private static ServerInterceptor echoRequestHeadersInterceptor(final Metadata.Key<?>... keys) {
    final Set<Metadata.Key<?>> keySet = new HashSet<>(Arrays.asList(keys));
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          ServerCall<ReqT, RespT> call,
          final Metadata requestHeaders,
          ServerCallHandler<ReqT, RespT> next) {
        return next.startCall(new SimpleForwardingServerCall<ReqT, RespT>(call) {
              @Override
              public void sendHeaders(Metadata responseHeaders) {
                responseHeaders.merge(requestHeaders, keySet);
                super.sendHeaders(responseHeaders);
              }

              @Override
              public void close(Status status, Metadata trailers) {
                trailers.merge(requestHeaders, keySet);
                super.close(status, trailers);
              }
            }, requestHeaders);
      }
    };
  }

  /**
   * Echoes request headers with the specified key(s) from a client into response headers only.
   */
  private static ServerInterceptor echoRequestMetadataInHeaders(final Metadata.Key<?>... keys) {
    final Set<Metadata.Key<?>> keySet = new HashSet<>(Arrays.asList(keys));
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          ServerCall<ReqT, RespT> call,
          final Metadata requestHeaders,
          ServerCallHandler<ReqT, RespT> next) {
        return next.startCall(new SimpleForwardingServerCall<ReqT, RespT>(call) {
          @Override
          public void sendHeaders(Metadata responseHeaders) {
            responseHeaders.merge(requestHeaders, keySet);
            super.sendHeaders(responseHeaders);
          }

          @Override
          public void close(Status status, Metadata trailers) {
            super.close(status, trailers);
          }
        }, requestHeaders);
      }
    };
  }

  /**
   * Echoes request headers with the specified key(s) from a client into response trailers only.
   */
  private static ServerInterceptor echoRequestMetadataInTrailers(final Metadata.Key<?>... keys) {
    final Set<Metadata.Key<?>> keySet = new HashSet<>(Arrays.asList(keys));
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          ServerCall<ReqT, RespT> call,
          final Metadata requestHeaders,
          ServerCallHandler<ReqT, RespT> next) {
        return next.startCall(new SimpleForwardingServerCall<ReqT, RespT>(call) {
          @Override
          public void sendHeaders(Metadata responseHeaders) {
            super.sendHeaders(responseHeaders);
          }

          @Override
          public void close(Status status, Metadata trailers) {
            trailers.merge(requestHeaders, keySet);
            super.close(status, trailers);
          }
        }, requestHeaders);
      }
    };
  }
}
