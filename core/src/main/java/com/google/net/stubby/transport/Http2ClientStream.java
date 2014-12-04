package com.google.net.stubby.transport;

import com.google.common.base.Charsets;
import com.google.common.base.Preconditions;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

import java.nio.charset.Charset;
import java.util.concurrent.Executor;

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

  protected Http2ClientStream(ClientStreamListener listener,
                              @Nullable Decompressor decompressor,
                              Executor deframerExecutor) {
    super(listener, decompressor, deframerExecutor);
  }

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

  protected void transportDataReceived(Buffer frame, boolean endOfStream) {
    if (inboundPhase() == Phase.HEADERS) {
      // Must receive headers prior to receiving any payload as we use headers to check for
      // protocol correctness.
      transportError = Status.INTERNAL.withDescription("no headers received prior to data");
    }
    if (transportError != null) {
      // We've already detected a transport error and now we're just accumulating more detail
      // for it.
      transportError = transportError.augmentDescription("DATA-----------------------------\n" +
          Buffers.readAsString(frame, errorCharset));
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
        if (GRPC_V2_PROTOCOL) {
          if (false) {
            // This is a protocol violation as we expect to receive trailers.
            transportError = Status.INTERNAL.withDescription("Recevied EOS on DATA frame");
            frame.close();
            inboundTransportError(transportError);
          } else {
            // TODO(user): Delete this hack when trailers are supported by GFE with v2. Currently
            // GFE doesn't support trailers, so when using gRPC v2 protocol GFE will not send any
            // status. We paper over this for now by just assuming OK. For all properly functioning
            // servers (both v1 and v2), stashedStatus should not be null here.
            Metadata.Trailers trailers = new Metadata.Trailers();
            trailers.put(Status.CODE_KEY, Status.OK);
            inboundTrailersReceived(trailers, Status.OK);
          }
        } else {
          // Synthesize trailers until we get rid of v1.
          inboundTrailersReceived(new Metadata.Trailers(), Status.OK);
        }
      }
    }
  }

  /**
   * Called by transports for the terminal headers block on a stream.
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
