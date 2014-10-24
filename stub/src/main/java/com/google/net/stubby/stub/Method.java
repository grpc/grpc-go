package com.google.net.stubby.stub;

import com.google.net.stubby.Marshaller;
import com.google.net.stubby.MethodType;

import javax.annotation.concurrent.Immutable;

/**
 * A description of a method exposed by a service. Typically instances are created via code
 * generation.
 */
@Immutable
public class Method<RequestT, ResponseT> {

  private final MethodType type;
  private final String name;
  private final Marshaller<RequestT> requestMarshaller;
  private final Marshaller<ResponseT> responseMarshaller;

  /**
   * Constructor.
   *
   * @param type the call type of the method
   * @param name the name of the method, not including the service name
   * @param requestMarshaller used to serialize/deserialize the request
   * @param responseMarshaller used to serialize/deserialize the response
   */
  public static <RequestT, ResponseT> Method<RequestT, ResponseT> create(
      MethodType type, String name,
      Marshaller<RequestT> requestMarshaller, Marshaller<ResponseT> responseMarshaller) {
    return new Method<RequestT, ResponseT>(type, name, requestMarshaller, responseMarshaller);
  }

  private Method(MethodType type, String name, Marshaller<RequestT> requestMarshaller,
      Marshaller<ResponseT> responseMarshaller) {
    this.type = type;
    this.name = name;
    this.requestMarshaller = requestMarshaller;
    this.responseMarshaller = responseMarshaller;
  }

  /**
   * The call type of the method.
   */
  public MethodType getType() {
    return type;
  }

  /**
   * The name of the method, not including the service name
   */
  public String getName() {
    return name;
  }

  public Marshaller<RequestT> getRequestMarshaller() {
    return requestMarshaller;
  }

  public Marshaller<ResponseT> getResponseMarshaller() {
    return responseMarshaller;
  }
}
