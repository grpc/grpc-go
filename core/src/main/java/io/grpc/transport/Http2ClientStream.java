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

package io.grpc.transport;

import com.google.common.base.Charsets;
import com.google.common.base.Preconditions;

import io.grpc.Metadata;
import io.grpc.Status;

import java.nio.charset.Charset;

import javax.annotation.Nullable;

/**
 * Base implementation for client streams using HTTP2 as the transport.
 */
public abstract class Http2ClientStream extends AbstractClientStream<Integer> {

  private static final boolean TEMP_CHECK_CONTENT_TYPE = false;
  /**
   * Metadata marshaller for HTTP status lines.
   */
  private static final Metadata.AsciiMarshaller<Integer> HTTP_STATUS_LINE_MARSHALLER =
      new Metadata.AsciiMarshaller<Integer>() {
        @Override
        public String toAsciiString(Integer value) {
          return value.toString();
        }

        @Override
        public Integer parseAsciiString(String serialized) {
          return Integer.parseInt(serialized.split(" ", 2)[0]);
        }
      };

  private static final Metadata.Key<Integer> HTTP2_STATUS = Metadata.Key.of(":status",
      HTTP_STATUS_LINE_MARSHALLER);

  private Status transportError;
  private Charset errorCharset = Charsets.UTF_8;
  private boolean contentTypeChecked;

  protected Http2ClientStream(WritableBufferAllocator bufferAllocator,
                              ClientStreamListener listener) {
    super(bufferAllocator, listener);
  }

  /**
   * Called by subclasses whenever {@code Headers} are received from the transport.
   *
   * @param headers the received headers
   */
  protected void transportHeadersReceived(Metadata.Headers headers) {
    Preconditions.checkNotNull(headers);
    if (transportError != null) {
      // Already received a transport error so just augment it.
      transportError = transportError.augmentDescription(headers.toString());
      return;
    }
    Status httpStatus = statusFromHttpStatus(headers);
    if (httpStatus == null) {
      transportError = Status.INTERNAL.withDescription(
          "received non-terminal headers with no :status");
    } else if (!httpStatus.isOk()) {
      transportError = httpStatus;
    } else {
      transportError = checkContentType(headers);
    }
    if (transportError != null) {
      // Note we don't immediately report the transport error, instead we wait for more data on the
      // stream so we can accumulate more detail into the error before reporting it.
      transportError = transportError.withDescription("\n" + headers.toString());
      errorCharset = extractCharset(headers);
    } else {
      stripTransportDetails(headers);
      inboundHeadersReceived(headers);
    }
  }

  /**
   * Called by subclasses whenever a data frame is received from the transport.
   *
   * @param frame the received data frame
   * @param endOfStream {@code true} if there will be no more data received for this stream
   */
  protected void transportDataReceived(ReadableBuffer frame, boolean endOfStream) {
    if (transportError == null && inboundPhase() == Phase.HEADERS) {
      // Must receive headers prior to receiving any payload as we use headers to check for
      // protocol correctness.
      transportError = Status.INTERNAL.withDescription("no headers received prior to data");
    }
    if (transportError != null) {
      // We've already detected a transport error and now we're just accumulating more detail
      // for it.
      transportError = transportError.augmentDescription("DATA-----------------------------\n" +
          ReadableBuffers.readAsString(frame, errorCharset));
      frame.close();
      if (transportError.getDescription().length() > 1000 || endOfStream) {
        inboundTransportError(transportError);
        if (!endOfStream) {
          // We have enough error detail so lets cancel.
          sendCancel();
        }
      }
    } else {
      inboundDataReceived(frame);
      if (endOfStream) {
        // This is a protocol violation as we expect to receive trailers.
        transportError = Status.INTERNAL.withDescription("Recevied EOS on DATA frame");
        inboundTransportError(transportError);
      }
    }
  }

  /**
   * Called by subclasses for the terminal trailer metadata on a stream.
   *
   * @param trailers the received terminal trailer metadata
   */
  protected void transportTrailersReceived(Metadata.Trailers trailers) {
    Preconditions.checkNotNull(trailers);
    if (transportError != null) {
      // Already received a transport error so just augment it.
      transportError = transportError.augmentDescription(trailers.toString());
    } else {
      transportError = checkContentType(trailers);
    }
    if (transportError != null) {
      inboundTransportError(transportError);
    } else {
      Status status = statusFromTrailers(trailers);
      stripTransportDetails(trailers);
      inboundTrailersReceived(trailers, status);
    }
  }

  private static Status statusFromHttpStatus(Metadata metadata) {
    Integer httpStatus = metadata.get(HTTP2_STATUS);
    if (httpStatus != null) {
      Status status = HttpUtil.httpStatusToGrpcStatus(httpStatus);
      if (!status.isOk()) {
        status.augmentDescription("extracted status from HTTP :status " + httpStatus);
      }
      return status;
    }
    return null;
  }

  /**
   * Extract the response status from trailers.
   */
  private Status statusFromTrailers(Metadata.Trailers trailers) {
    Status status = trailers.get(Status.CODE_KEY);
    if (status == null) {
      status = statusFromHttpStatus(trailers);
      if (status == null || status.isOk()) {
        status = Status.INTERNAL.withDescription("missing GRPC status in response");
      } else {
        status = status.withDescription(
            "missing GRPC status, inferred error from HTTP status code");
      }
    }
    String message = trailers.get(Status.MESSAGE_KEY);
    if (message != null) {
      status = status.augmentDescription(message);
    }
    return status;
  }

  /**
   * Inspect the content type field from received headers or trailers and return an error Status if
   * content type is invalid or not present. Returns null if no error was found.
   */
  @Nullable
  private Status checkContentType(Metadata headers) {
    if (contentTypeChecked) {
      return null;
    }
    contentTypeChecked = true;
    String contentType = headers.get(HttpUtil.CONTENT_TYPE);
    if (TEMP_CHECK_CONTENT_TYPE && !HttpUtil.CONTENT_TYPE_GRPC.equalsIgnoreCase(contentType)) {
      // Malformed content-type so report an error
      return Status.INTERNAL.withDescription("invalid content-type " + contentType);
    }
    return null;
  }

  /**
   * Inspect the raw metadata and figure out what charset is being used.
   */
  private static Charset extractCharset(Metadata headers) {
    String contentType = headers.get(HttpUtil.CONTENT_TYPE);
    if (contentType != null) {
      String[] split = contentType.split("charset=");
      try {
        return Charset.forName(split[split.length - 1].trim());
      } catch (Exception t) {
        // Ignore and assume UTF-8
      }
    }
    return Charsets.UTF_8;
  }

  /**
   * Strip HTTP transport implementation details so they don't leak via metadata into
   * the application layer.
   */
  private static void stripTransportDetails(Metadata metadata) {
    metadata.removeAll(HTTP2_STATUS);
    metadata.removeAll(Status.CODE_KEY);
    metadata.removeAll(Status.MESSAGE_KEY);
  }
}
