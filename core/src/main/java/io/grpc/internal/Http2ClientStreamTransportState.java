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

package io.grpc.internal;

import com.google.common.base.Charsets;
import com.google.common.base.Preconditions;
import io.grpc.InternalMetadata;
import io.grpc.InternalStatus;
import io.grpc.Metadata;
import io.grpc.Status;
import java.nio.charset.Charset;
import javax.annotation.Nullable;

/**
 * Base implementation for client streams using HTTP2 as the transport.
 */
public abstract class Http2ClientStreamTransportState extends AbstractClientStream.TransportState {

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

  protected Http2ClientStreamTransportState(
      int maxMessageSize,
      StatsTraceContext statsTraceCtx,
      TransportTracer transportTracer) {
    super(maxMessageSize, statsTraceCtx, transportTracer);
  }

  /**
   * Called to process a failure in HTTP/2 processing. It should notify the transport to cancel the
   * stream and call {@code transportReportStatus()}.
   */
  protected abstract void http2ProcessingFailed(
      Status status, boolean stopDelivery, Metadata trailers);

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
        http2ProcessingFailed(transportError, false, transportErrorMetadata);
      }
    } else {
      if (!headersReceived) {
        http2ProcessingFailed(
            Status.INTERNAL.withDescription("headers not received before payload"),
            false,
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
      http2ProcessingFailed(transportError, false, transportErrorMetadata);
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
    Status status = trailers.get(InternalStatus.CODE_KEY);
    if (status != null) {
      return status.withDescription(trailers.get(InternalStatus.MESSAGE_KEY));
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
      String[] split = contentType.split("charset=", 2);
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
    metadata.discardAll(InternalStatus.CODE_KEY);
    metadata.discardAll(InternalStatus.MESSAGE_KEY);
  }
}
