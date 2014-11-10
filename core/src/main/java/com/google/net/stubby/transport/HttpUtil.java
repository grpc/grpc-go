package com.google.net.stubby.transport;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

import java.net.HttpURLConnection;

/**
 * Constants for GRPC-over-HTTP (or HTTP/2)
 */
public final class HttpUtil {
  /**
   * The Content-Type header name. Defined here since it is not explicitly defined by the HTTP/2
   * spec.
   */
  public static final Metadata.Key<String> CONTENT_TYPE =
      Metadata.Key.of("content-type", Metadata.STRING_MARSHALLER);

  /**
   * Content-Type used for GRPC-over-HTTP/2.
   */
  public static final String CONTENT_TYPE_GRPC = "application/grpc";

  /**
   * The HTTP method used for GRPC requests.
   */
  public static final String HTTP_METHOD = "POST";

  /**
   * Maps HTTP error response status codes to transport codes.
   */
  public static Status httpStatusToGrpcStatus(int httpStatusCode) {
    // Specific HTTP code handling.
    switch (httpStatusCode) {
      case HttpURLConnection.HTTP_UNAUTHORIZED: // 401
        return Status.UNAUTHENTICATED;
      case HttpURLConnection.HTTP_FORBIDDEN: // 403
        return Status.PERMISSION_DENIED;
      default:
    }
    // Generic HTTP code handling.
    if (httpStatusCode < 300) {
      return Status.OK;
    }
    if (httpStatusCode < 400) {
      return Status.UNAVAILABLE;
    }
    if (httpStatusCode < 500) {
      return Status.INVALID_ARGUMENT;
    }
    if (httpStatusCode < 600) {
      return Status.FAILED_PRECONDITION;
    }
    return Status.INTERNAL;
  }

  private HttpUtil() {}
}
