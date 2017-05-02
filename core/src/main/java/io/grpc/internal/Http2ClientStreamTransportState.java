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

package io.grpc.internal;

import com.google.common.base.Charsets;
import com.google.common.base.Preconditions;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.Status;
import java.nio.charset.Charset;
import javax.annotation.Nullable;

/**
 * Base implementation for client streams using HTTP2 as the transport.
 */
public abstract class Http2ClientStreamTransportState extends AbstractClientStream2.TransportState {

  /**
   * Metadata marshaller for HTTP status lines.
   */
  private static final InternalMetadata.TrustedAsciiMarshaller<Integer> HTTP_STATUS_MARSHALLER =
      new InternalMetadata.TrustedAsciiMarshaller<Integer>() {
        @Override
        public byte[] toAsciiString(Integer value) {
          throw new UnsupportedOperationException();
        }

        /**
         * RFC 7231 says status codes are 3 digits long.
         *
         * @see <a href="https://tools.ietf.org/html/rfc7231#section-6">RFC 7231</a>
         */
        @Override
        public Integer parseAsciiString(byte[] serialized) {
          if (serialized.length >= 3) {
            return (serialized[0] - '0') * 100 + (serialized[1] - '0') * 10 + (serialized[2] - '0');
          }
          throw new NumberFormatException(
              "Malformed status code " + new String(serialized, InternalMetadata.US_ASCII));
        }
      };

  private static final Metadata.Key<Integer> HTTP2_STATUS = InternalMetadata.keyOf(":status",
      HTTP_STATUS_MARSHALLER);

  /** When non-{@code null}, {@link #transportErrorMetadata} must also be non-{@code null}. */
  private Status transportError;
  private Metadata transportErrorMetadata;
  private Charset errorCharset = Charsets.UTF_8;
  private boolean headersReceived;

  protected Http2ClientStreamTransportState(int maxMessageSize, StatsTraceContext statsTraceCtx) {
    super(maxMessageSize, statsTraceCtx);
  }

  /**
   * Called to process a failure in HTTP/2 processing. It should notify the transport to cancel the
   * stream and call {@code transportReportStatus()}.
   */
  protected abstract void http2ProcessingFailed(Status status, Metadata trailers);

  /**
   * Called by subclasses whenever {@code Headers} are received from the transport.
   *
   * @param headers the received headers
   */
  protected void transportHeadersReceived(Metadata headers) {
    Preconditions.checkNotNull(headers, "headers");
    if (transportError != null) {
      // Already received a transport error so just augment it. Something is really, really strange.
      transportError = transportError.augmentDescription("headers: " + headers);
      return;
    }
    try {
      if (headersReceived) {
        transportError = Status.INTERNAL.withDescription("Received headers twice");
        return;
      }
      Integer httpStatus = headers.get(HTTP2_STATUS);
      if (httpStatus != null && httpStatus >= 100 && httpStatus < 200) {
        // Ignore the headers. See RFC 7540 §8.1
        return;
      }
      headersReceived = true;

      transportError = validateInitialMetadata(headers);
      if (transportError != null) {
        return;
      }

      stripTransportDetails(headers);
      inboundHeadersReceived(headers);
    } finally {
      if (transportError != null) {
        // Note we don't immediately report the transport error, instead we wait for more data on
        // the stream so we can accumulate more detail into the error before reporting it.
        transportError = transportError.augmentDescription("headers: " + headers);
        transportErrorMetadata = headers;
        errorCharset = extractCharset(headers);
      }
    }
  }

  /**
   * Called by subclasses whenever a data frame is received from the transport.
   *
   * @param frame the received data frame
   * @param endOfStream {@code true} if there will be no more data received for this stream
   */
  protected void transportDataReceived(ReadableBuffer frame, boolean endOfStream) {
    if (transportError != null) {
      // We've already detected a transport error and now we're just accumulating more detail
      // for it.
      transportError = transportError.augmentDescription("DATA-----------------------------\n"
          + ReadableBuffers.readAsString(frame, errorCharset));
      frame.close();
      if (transportError.getDescription().length() > 1000 || endOfStream) {
        http2ProcessingFailed(transportError, transportErrorMetadata);
      }
    } else {
      if (!headersReceived) {
        http2ProcessingFailed(
            Status.INTERNAL.withDescription("headers not received before payload"),
            new Metadata());
        return;
      }
      inboundDataReceived(frame);
      if (endOfStream) {
        // This is a protocol violation as we expect to receive trailers.
        transportError =
            Status.INTERNAL.withDescription("Received unexpected EOS on DATA frame from server.");
        transportErrorMetadata = new Metadata();
        transportReportStatus(transportError, false, transportErrorMetadata);
      }
    }
  }

  /**
   * Called by subclasses for the terminal trailer metadata on a stream.
   *
   * @param trailers the received terminal trailer metadata
   */
  protected void transportTrailersReceived(Metadata trailers) {
    Preconditions.checkNotNull(trailers, "trailers");
    if (transportError == null && !headersReceived) {
      transportError = validateInitialMetadata(trailers);
      if (transportError != null) {
        transportErrorMetadata = trailers;
      }
    }
    if (transportError != null) {
      transportError = transportError.augmentDescription("trailers: " + trailers);
      http2ProcessingFailed(transportError, transportErrorMetadata);
    } else {
      Status status = statusFromTrailers(trailers);
      stripTransportDetails(trailers);
      inboundTrailersReceived(trailers, status);
    }
  }

  /**
   * Extract the response status from trailers.
   */
  private Status statusFromTrailers(Metadata trailers) {
    Status status = trailers.get(Status.CODE_KEY);
    if (status != null) {
      return status.withDescription(trailers.get(Status.MESSAGE_KEY));
    }
    // No status; something is broken. Try to provide a resonanable error.
    if (headersReceived) {
      return Status.UNKNOWN.withDescription("missing GRPC status in response");
    }
    Integer httpStatus = trailers.get(HTTP2_STATUS);
    if (httpStatus != null) {
      status = GrpcUtil.httpStatusToGrpcStatus(httpStatus);
    } else {
      status = Status.INTERNAL.withDescription("missing HTTP status code");
    }
    return status.augmentDescription(
        "missing GRPC status, inferred error from HTTP status code");
  }

  /**
   * Inspect initial headers to make sure they conform to HTTP and gRPC, returning a {@code Status}
   * on failure.
   *
   * @return status with description of failure, or {@code null} when valid
   */
  @Nullable
  private Status validateInitialMetadata(Metadata headers) {
    Integer httpStatus = headers.get(HTTP2_STATUS);
    if (httpStatus == null) {
      return Status.INTERNAL.withDescription("Missing HTTP status code");
    }
    String contentType = headers.get(GrpcUtil.CONTENT_TYPE_KEY);
    if (!GrpcUtil.isGrpcContentType(contentType)) {
      return GrpcUtil.httpStatusToGrpcStatus(httpStatus)
          .augmentDescription("invalid content-type: " + contentType);
    }
    return null;
  }

  /**
   * Inspect the raw metadata and figure out what charset is being used.
   */
  private static Charset extractCharset(Metadata headers) {
    String contentType = headers.get(GrpcUtil.CONTENT_TYPE_KEY);
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
    metadata.discardAll(HTTP2_STATUS);
    metadata.discardAll(Status.CODE_KEY);
    metadata.discardAll(Status.MESSAGE_KEY);
  }
}
