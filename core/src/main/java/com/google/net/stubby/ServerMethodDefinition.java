package com.google.net.stubby;

/** Definition of a method supported by a service. */
public final class ServerMethodDefinition<RequestT, ResponseT> {
  private final String name;
  private final Marshaller<RequestT> requestMarshaller;
  private final Marshaller<ResponseT> responseMarshaller;
  private final ServerCallHandler<RequestT, ResponseT> handler;

  // ServerMethodDefinition has no form of public construction. It is only created within the
  // context of a ServerServiceDefinition.Builder.
  ServerMethodDefinition(String name, Marshaller<RequestT> requestMarshaller,
      Marshaller<ResponseT> responseMarshaller, ServerCallHandler<RequestT, ResponseT> handler) {
    this.name = name;
    this.requestMarshaller = requestMarshaller;
    this.responseMarshaller = responseMarshaller;
    this.handler = handler;
  }

  /** The simple name of the method. It is not an absolute path. */
  public String getName() {
    return name;
  }

  /** Marshaller for deserializing incoming requests. */
  public Marshaller<RequestT> getRequestMarshaller() {
    return requestMarshaller;
  }

  /** Marshaller for serializing outgoing responses. */
  public Marshaller<ResponseT> getResponseMarshaller() {
    return responseMarshaller;
  }

  /** Handler for incoming calls. */
  public ServerCallHandler<RequestT, ResponseT> getServerCallHandler() {
    return handler;
  }
}
