/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static io.grpc.internal.GrpcUtil.ACCEPT_ENCODING_SPLITTER;
import static io.grpc.internal.GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY;
import static io.grpc.internal.GrpcUtil.MESSAGE_ENCODING_KEY;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.Attributes;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.CompressorRegistry;
import io.grpc.Context;
import io.grpc.Decompressor;
import io.grpc.DecompressorRegistry;
import io.grpc.InternalDecompressorRegistry;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCall;
import io.grpc.Status;
import java.io.IOException;
import java.io.InputStream;
import java.util.List;

final class ServerCallImpl<ReqT, RespT> extends ServerCall<ReqT, RespT> {
  private final ServerStream stream;
  private final MethodDescriptor<ReqT, RespT> method;
  private final Context.CancellableContext context;
  private final byte[] messageAcceptEncoding;
  private final DecompressorRegistry decompressorRegistry;
  private final CompressorRegistry compressorRegistry;

  // state
  private volatile boolean cancelled;
  private boolean sendHeadersCalled;
  private boolean closeCalled;
  private Compressor compressor;

  ServerCallImpl(ServerStream stream, MethodDescriptor<ReqT, RespT> method,
      Metadata inboundHeaders, Context.CancellableContext context,
      DecompressorRegistry decompressorRegistry, CompressorRegistry compressorRegistry) {
    this.stream = stream;
    this.method = method;
    this.context = context;
    this.messageAcceptEncoding = inboundHeaders.get(MESSAGE_ACCEPT_ENCODING_KEY);
    this.decompressorRegistry = decompressorRegistry;
    this.compressorRegistry = compressorRegistry;

    if (inboundHeaders.containsKey(MESSAGE_ENCODING_KEY)) {
      String encoding = inboundHeaders.get(MESSAGE_ENCODING_KEY);
      Decompressor decompressor = decompressorRegistry.lookupDecompressor(encoding);
      if (decompressor == null) {
        throw Status.UNIMPLEMENTED
            .withDescription(String.format("Can't find decompressor for %s", encoding))
            .asRuntimeException();
      }
      stream.setDecompressor(decompressor);
    }
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
        List<String> acceptedEncodingsList = ACCEPT_ENCODING_SPLITTER.splitToList(
            new String(messageAcceptEncoding, GrpcUtil.US_ASCII));
        if (!acceptedEncodingsList.contains(compressor.getMessageEncoding())) {
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
    try {
      InputStream resp = method.streamResponse(message);
      stream.writeMessage(resp);
      stream.flush();
    } catch (RuntimeException e) {
      close(Status.fromThrowable(e), new Metadata());
      throw e;
    } catch (Throwable t) {
      close(Status.fromThrowable(t), new Metadata());
      throw new RuntimeException(t);
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
    closeCalled = true;
    stream.close(status, trailers);
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
   * All of these callbacks are assumed to called on an application thread, and the caller is
   * responsible for handling thrown exceptions.
   */
  @VisibleForTesting
  static final class ServerStreamListenerImpl<ReqT> implements ServerStreamListener {
    private final ServerCallImpl<ReqT, ?> call;
    private final ServerCall.Listener<ReqT> listener;
    private final Context.CancellableContext context;
    private boolean messageReceived;

    public ServerStreamListenerImpl(
        ServerCallImpl<ReqT, ?> call, ServerCall.Listener<ReqT> listener,
        Context.CancellableContext context) {
      this.call = checkNotNull(call, "call");
      this.listener = checkNotNull(listener, "listener must not be null");
      this.context = checkNotNull(context, "context");
    }

    @SuppressWarnings("Finally") // The code avoids suppressing the exception thrown from try
    @Override
    public void messageRead(final InputStream message) {
      Throwable t = null;
      try {
        if (call.cancelled) {
          return;
        }
        // Special case for unary calls.
        if (messageReceived && call.method.getType() == MethodType.UNARY) {
          call.stream.close(Status.INTERNAL.withDescription(
                  "More than one request messages for unary call or server streaming call"),
              new Metadata());
          return;
        }
        messageReceived = true;

        listener.onMessage(call.method.parseRequest(message));
      } catch (Throwable e) {
        t = e;
      } finally {
        try {
          message.close();
        } catch (IOException e) {
          throw new RuntimeException(e);
        } finally {
          if (t != null) {
            // TODO(carl-mastrangelo): Maybe log e here.
            MoreThrowables.throwIfUnchecked(t);
            throw new RuntimeException(t);
          }
        }
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
