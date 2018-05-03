/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.internal.GrpcUtil.ACCEPT_ENCODING_SPLITTER;
import static io.grpc.internal.GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.Attributes;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.DecompressorRegistry;
import io.grpc.InternalDecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.Status;
import java.io.InputStream;
import java.util.logging.Level;
import java.util.logging.Logger;

final class ServerCallImpl<ReqT, RespT> extends ServerCall<ReqT, RespT> {

  private static final Logger log = Logger.getLogger(ServerCallImpl.class.getName());

  @VisibleForTesting
  static final String TOO_MANY_RESPONSES = "Too many responses";
  @VisibleForTesting
  static final String MISSING_RESPONSE = "Completed without a response";

  private final ServerStream stream;
  private final MethodDescriptor<ReqT, RespT> method;
  private final Context.CancellableContext context;
  private final byte[] messageAcceptEncoding;
  private final DecompressorRegistry decompressorRegistry;
  private final CompressorRegistry compressorRegistry;
  private CallTracer serverCallTracer;

  // state
  private volatile boolean cancelled;
  private boolean sendHeadersCalled;
  private boolean closeCalled;
  private Compressor compressor;
  private boolean messageSent;

  ServerCallImpl(ServerStream stream, MethodDescriptor<ReqT, RespT> method,
      Metadata inboundHeaders, Context.CancellableContext context,
      DecompressorRegistry decompressorRegistry, CompressorRegistry compressorRegistry,
      CallTracer serverCallTracer) {
    this.stream = stream;
    this.method = method;
    this.context = context;
    this.messageAcceptEncoding = inboundHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY);
    this.decompressorRegistry = decompressorRegistry;
    this.compressorRegistry = compressorRegistry;
    this.serverCallTracer = serverCallTracer;
    this.serverCallTracer.reportCallStarted();
  }

  @Override
  public void request(int numMessages) {
    stream.request(numMessages);
  }

  @Override
  public void sendHeaders(Metadata headers) {
    checkState(!sendHeadersCalled, "sendHeaders has already been called");
    checkState(!closeCalled, "call is closed");

    headers.discardAll(MESSAGE_ENCODING_KEY);
    if (compressor == null) {
      compressor = Codec.Identity.NONE;
    } else {
      if (messageAcceptEncoding != null) {
        // TODO(carl-mastrangelo): remove the string allocation.
        if (!GrpcUtil.iterableContains(
            ACCEPT_ENCODING_SPLITTER.split(new String(messageAcceptEncoding, GrpcUtil.US_ASCII)),
            compressor.getMessageEncoding())) {
          // resort to using no compression.
          compressor = Codec.Identity.NONE;
        }
      } else {
        compressor = Codec.Identity.NONE;
      }
    }

    // Always put compressor, even if it's identity.
    headers.put(MESSAGE_ENCODING_KEY, compressor.getMessageEncoding());

    stream.setCompressor(compressor);

    headers.discardAll(MESSAGE_ACCEPT_ENCODING_KEY);
    byte[] advertisedEncodings =
        InternalDecompressorRegistry.getRawAdvertisedMessageEncodings(decompressorRegistry);
    if (advertisedEncodings.length != 0) {
      headers.put(MESSAGE_ACCEPT_ENCODING_KEY, advertisedEncodings);
    }

    // Don't check if sendMessage has been called, since it requires that sendHeaders was already
    // called.
    sendHeadersCalled = true;
    stream.writeHeaders(headers);
  }

  @Override
  public void sendMessage(RespT message) {
    checkState(sendHeadersCalled, "sendHeaders has not been called");
    checkState(!closeCalled, "call is closed");

    if (method.getType().serverSendsOneMessage() && messageSent) {
      internalClose(Status.INTERNAL.withDescription(TOO_MANY_RESPONSES));
      return;
    }

    messageSent = true;
    try {
      InputStream resp = method.streamResponse(message);
      stream.writeMessage(resp);
      stream.flush();
    } catch (RuntimeException e) {
      close(Status.fromThrowable(e), new Metadata());
    } catch (Error e) {
      close(
          Status.CANCELLED.withDescription("Server sendMessage() failed with Error"),
          new Metadata());
      throw e;
    }
  }

  @Override
  public void setMessageCompression(boolean enable) {
    stream.setMessageCompression(enable);
  }

  @Override
  public void setCompression(String compressorName) {
    // Added here to give a better error message.
    checkState(!sendHeadersCalled, "sendHeaders has been called");

    compressor = compressorRegistry.lookupCompressor(compressorName);
    checkArgument(compressor != null, "Unable to find compressor by name %s", compressorName);
  }

  @Override
  public boolean isReady() {
    return stream.isReady();
  }

  @Override
  public void close(Status status, Metadata trailers) {
    checkState(!closeCalled, "call already closed");
    try {
      closeCalled = true;

      if (status.isOk() && method.getType().serverSendsOneMessage() && !messageSent) {
        internalClose(Status.INTERNAL.withDescription(MISSING_RESPONSE));
        return;
      }

      stream.close(status, trailers);
    } finally {
      serverCallTracer.reportCallEnded(status.isOk());
    }
  }

  @Override
  public boolean isCancelled() {
    return cancelled;
  }

  ServerStreamListener newServerStreamListener(ServerCall.Listener<ReqT> listener) {
    return new ServerStreamListenerImpl<ReqT>(this, listener, context);
  }

  @Override
  public Attributes getAttributes() {
    return stream.getAttributes();
  }

  @Override
  public String getAuthority() {
    return stream.getAuthority();
  }

  @Override
  public MethodDescriptor<ReqT, RespT> getMethodDescriptor() {
    return method;
  }

  /**
   * Close the {@link ServerStream} because an internal error occurred. Allow the application to
   * run until completion, but silently ignore interactions with the {@link ServerStream} from now
   * on.
   */
  private void internalClose(Status internalError) {
    log.log(Level.WARNING, "Cancelling the stream with status {0}", new Object[] {internalError});
    stream.cancel(internalError);
    serverCallTracer.reportCallEnded(internalError.isOk()); // error so always false
  }

  /**
   * All of these callbacks are assumed to called on an application thread, and the caller is
   * responsible for handling thrown exceptions.
   */
  @VisibleForTesting
  static final class ServerStreamListenerImpl<ReqT> implements ServerStreamListener {
    private final ServerCallImpl<ReqT, ?> call;
    private final ServerCall.Listener<ReqT> listener;
    private final Context.CancellableContext context;

    public ServerStreamListenerImpl(
        ServerCallImpl<ReqT, ?> call, ServerCall.Listener<ReqT> listener,
        Context.CancellableContext context) {
      this.call = checkNotNull(call, "call");
      this.listener = checkNotNull(listener, "listener must not be null");
      this.context = checkNotNull(context, "context");
      // Wire ourselves up so that if the context is cancelled, our flag call.cancelled also
      // reflects the new state. Use a DirectExecutor so that it happens in the same thread
      // as the caller of {@link Context#cancel}.
      this.context.addListener(
          new Context.CancellationListener() {
            @Override
            public void cancelled(Context context) {
              ServerStreamListenerImpl.this.call.cancelled = true;
            }
          },
          MoreExecutors.directExecutor());
    }

    @SuppressWarnings("Finally") // The code avoids suppressing the exception thrown from try
    @Override
    public void messagesAvailable(final MessageProducer producer) {
      if (call.cancelled) {
        GrpcUtil.closeQuietly(producer);
        return;
      }

      InputStream message;
      try {
        while ((message = producer.next()) != null) {
          try {
            listener.onMessage(call.method.parseRequest(message));
          } catch (Throwable t) {
            GrpcUtil.closeQuietly(message);
            throw t;
          }
          message.close();
        }
      } catch (Throwable t) {
        GrpcUtil.closeQuietly(producer);
        MoreThrowables.throwIfUnchecked(t);
        throw new RuntimeException(t);
      }
    }

    @Override
    public void halfClosed() {
      if (call.cancelled) {
        return;
      }

      listener.onHalfClose();
    }

    @Override
    public void closed(Status status) {
      try {
        if (status.isOk()) {
          listener.onComplete();
        } else {
          call.cancelled = true;
          listener.onCancel();
        }
      } finally {
        // Cancel context after delivering RPC closure notification to allow the application to
        // clean up and update any state based on whether onComplete or onCancel was called.
        context.cancel(null);
      }
    }

    @Override
    public void onReady() {
      if (call.cancelled) {
        return;
      }
      listener.onReady();
    }
  }
}
