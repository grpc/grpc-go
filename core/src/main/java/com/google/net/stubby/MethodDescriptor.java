package com.google.net.stubby;

import com.google.common.base.Function;
import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableMap;
import com.google.common.collect.Maps;

import java.io.InputStream;
import java.util.Map;
import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;
import javax.inject.Provider;

/**
 * Descriptor for a single operation, used by Channel to execute a call.
 */
@Immutable
public class MethodDescriptor<RequestT, ResponseT> {

  public enum Type {
    UNARY,
    CLIENT_STREAMING,
    SERVER_STREAMING,
    DUPLEX_STREAMING,
    UNKNOWN
  }

  private  static final Function<Provider<String>,String> HEADER_SNAPSHOT =
      new Function<Provider<String>, String>() {
    @Override
    public String apply(@Nullable Provider<String> headerProvider) {
      return headerProvider == null ? null : headerProvider.get();
    }
  };

  private final Type type;
  private final String name;
  private final Marshaller<RequestT> requestMarshaller;
  private final Marshaller<ResponseT> responseMarshaller;
  private final long timeoutMicros;
  private final ImmutableMap<String, Provider<String>> headers;

  public static <RequestT, ResponseT> MethodDescriptor<RequestT, ResponseT> create(
      Type type, String name, long timeout, TimeUnit timeoutUnit,
      Marshaller<RequestT> requestMarshaller,
      Marshaller<ResponseT> responseMarshaller) {
    return new MethodDescriptor<RequestT, ResponseT>(
        type, name, timeoutUnit.toMicros(timeout), requestMarshaller, responseMarshaller,
        ImmutableMap.<String, Provider<String>>of());
  }

  private MethodDescriptor(Type type, String name, long timeoutMicros,
                           Marshaller<RequestT> requestMarshaller,
                           Marshaller<ResponseT> responseMarshaller,
                           ImmutableMap<String, Provider<String>> headers) {
    this.type = Preconditions.checkNotNull(type);
    this.name = name;
    Preconditions.checkArgument(timeoutMicros > 0);
    this.timeoutMicros = timeoutMicros;
    this.requestMarshaller = requestMarshaller;
    this.responseMarshaller = responseMarshaller;
    this.headers = headers;
  }

  /**
   * The call type of the method.
   */
  public Type getType() {
    return type;
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
   * Return a snapshot of the headers.
   */
  public Map<String, String> getHeaders() {
    if (headers.isEmpty()) {
      return ImmutableMap.of();
    }
    return ImmutableMap.copyOf(Maps.transformValues(headers, HEADER_SNAPSHOT));
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
  public MethodDescriptor<RequestT, ResponseT> withTimeout(long timeout, TimeUnit unit) {
    return new MethodDescriptor<RequestT, ResponseT>(type, name, unit.toMicros(timeout),
        requestMarshaller, responseMarshaller, headers);
  }

  /**
   * Create a new descriptor with an additional bound header.
   */
  public MethodDescriptor<RequestT, ResponseT> withHeader(String headerName,
      Provider<String> headerValueProvider) {
    return new MethodDescriptor<RequestT, ResponseT>(type, name, timeoutMicros,
        requestMarshaller, responseMarshaller,
        ImmutableMap.<String, Provider<String>>builder().
            putAll(headers).put(headerName, headerValueProvider).build());
  }

  /**
   * Creates a new descriptor with additional bound headers.
   */
  public MethodDescriptor<RequestT, ResponseT> withHeaders(
      ImmutableMap<String, Provider<String>> additionalHeaders) {
    return new MethodDescriptor<RequestT, ResponseT>(type, name, timeoutMicros,
        requestMarshaller, responseMarshaller,
        ImmutableMap.<String, Provider<String>>builder().
            putAll(headers).putAll(additionalHeaders).build());
  }
}
