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
      case HttpURLConnection.HTTP_BAD_REQUEST:  // 400
        return Status.INVALID_ARGUMENT;
      case HttpURLConnection.HTTP_UNAUTHORIZED:  // 401
        return Status.UNAUTHENTICATED;
      case HttpURLConnection.HTTP_FORBIDDEN:  // 403
        return Status.PERMISSION_DENIED;
      case HttpURLConnection.HTTP_NOT_FOUND:  // 404
        return Status.NOT_FOUND;
      case HttpURLConnection.HTTP_CONFLICT:  // 409
        return Status.ABORTED;
      case 416:  // Requested range not satisfiable
        return Status.OUT_OF_RANGE;
      case 429:  // Too many requests
        return Status.RESOURCE_EXHAUSTED;
      case 499:  // Client closed request
        return Status.CANCELLED;
      case HttpURLConnection.HTTP_NOT_IMPLEMENTED:  // 501
        return Status.UNIMPLEMENTED;
      case HttpURLConnection.HTTP_UNAVAILABLE:  // 503
        return Status.UNAVAILABLE;
      case HttpURLConnection.HTTP_GATEWAY_TIMEOUT:  // 504
        return Status.DEADLINE_EXCEEDED;
    }
    // Generic HTTP code handling.
    if (httpStatusCode < 200) {
      // 1xx and below
      return Status.UNKNOWN;
    }
    if (httpStatusCode < 300) {
      // 2xx
      return Status.OK;
    }
    if (httpStatusCode < 400) {
      // 3xx
      return Status.UNKNOWN;
    }
    if (httpStatusCode < 500) {
      // 4xx
      return Status.FAILED_PRECONDITION;
    }
    if (httpStatusCode < 600) {
      // 5xx
      return Status.INTERNAL;
    }
    return Status.UNKNOWN;
  }

  private HttpUtil() {}
}
