package com.google.net.stubby;

import com.google.common.base.Preconditions;

import java.io.InputStream;
import java.util.concurrent.TimeUnit;

import javax.annotation.concurrent.Immutable;

/**
 * Descriptor for a single operation, used by Channel to execute a call.
 */
@Immutable
public class MethodDescriptor<RequestT, ResponseT> {
  private final String name;
  private final Marshaller<RequestT> requestMarshaller;
  private final Marshaller<ResponseT> responseMarshaller;
  private final long timeoutMicros;

  public static <RequestT, ResponseT> MethodDescriptor<RequestT, ResponseT> create(
      String name, long timeout, TimeUnit timeoutUnit,
      Marshaller<RequestT> requestMarshaller,
      Marshaller<ResponseT> responseMarshaller) {
    return new MethodDescriptor<RequestT, ResponseT>(
        name, timeoutUnit.toMicros(timeout), requestMarshaller, responseMarshaller);
  }

  private MethodDescriptor(String name, long timeoutMicros,
                           Marshaller<RequestT> requestMarshaller,
                           Marshaller<ResponseT> responseMarshaller) {
    this.name = name;
    Preconditions.checkArgument(timeoutMicros > 0);
    this.timeoutMicros = timeoutMicros;
    this.requestMarshaller = requestMarshaller;
    this.responseMarshaller = responseMarshaller;
  }

  /**
   * The fully qualified name of the method
   */
  public String getName() {
    return name;
  }

  /**
   * Timeout for the operation in microseconds
   */
  public long getTimeout() {
    return timeoutMicros;
  }

  /**
   * Parse a response payload from the given {@link InputStream}
   */
  public ResponseT parseResponse(InputStream input) {
    return responseMarshaller.parse(input);
  }

  /**
   * Convert a request message to an {@link InputStream}
   */
  public InputStream streamRequest(RequestT requestMessage) {
    return requestMarshaller.stream(requestMessage);
  }

  /**
   * Create a new descriptor with a different timeout
   */
  public MethodDescriptor withTimeout(long timeout, TimeUnit unit) {
    return new MethodDescriptor<RequestT, ResponseT>(name, unit.toMicros(timeout),
        requestMarshaller, responseMarshaller);
  }
}
