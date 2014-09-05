package com.google.net.stubby;

import com.google.common.base.Preconditions;

import java.io.InputStream;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.Map.Entry;
import java.util.concurrent.TimeUnit;

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

  private final Type type;
  private final String name;
  private final Marshaller<RequestT> requestMarshaller;
  private final Marshaller<ResponseT> responseMarshaller;
  private final long timeoutMicros;
  private final Map<String, Provider<String>> headers;

  public static <RequestT, ResponseT> MethodDescriptor<RequestT, ResponseT> create(
      Type type, String name, long timeout, TimeUnit timeoutUnit,
      Marshaller<RequestT> requestMarshaller,
      Marshaller<ResponseT> responseMarshaller) {
    return new MethodDescriptor<RequestT, ResponseT>(
        type, name, timeoutUnit.toMicros(timeout), requestMarshaller, responseMarshaller,
        Collections.<String, Provider<String>>emptyMap());
  }

  private MethodDescriptor(Type type, String name, long timeoutMicros,
                           Marshaller<RequestT> requestMarshaller,
                           Marshaller<ResponseT> responseMarshaller,
                           Map<String, Provider<String>> headers) {
    this.type = Preconditions.checkNotNull(type);
    this.name = name;
    Preconditions.checkArgument(timeoutMicros > 0);
    this.timeoutMicros = timeoutMicros;
    this.requestMarshaller = requestMarshaller;
    this.responseMarshaller = responseMarshaller;
    this.headers = Collections.unmodifiableMap(headers);
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
      return Collections.emptyMap();
    }
    Map<String, String> snapshot = new HashMap<String, String>();
    for (Entry<String, Provider<String>> entry : headers.entrySet()) {
      snapshot.put(entry.getKey(), entry.getValue().get());
    }
    return Collections.unmodifiableMap(snapshot);
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
    Map<String, Provider<String>> newHeaders = new HashMap<String, Provider<String>>(headers);
    newHeaders.put(headerName, headerValueProvider);
    return new MethodDescriptor<RequestT, ResponseT>(type, name, timeoutMicros,
        requestMarshaller, responseMarshaller, newHeaders);
  }

  /**
   * Creates a new descriptor with additional bound headers.
   */
  public MethodDescriptor<RequestT, ResponseT> withHeaders(
      Map<String, Provider<String>> additionalHeaders) {
    Map<String, Provider<String>> newHeaders = new HashMap<String, Provider<String>>(headers);
    newHeaders.putAll(additionalHeaders);
    return new MethodDescriptor<RequestT, ResponseT>(type, name, timeoutMicros,
        requestMarshaller, responseMarshaller, newHeaders);
  }
}
