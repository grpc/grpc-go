package com.google.net.stubby;

import java.io.InputStream;

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

  /** Deserialize an incoming request message. */
  public RequestT parseRequest(InputStream input) {
    return requestMarshaller.parse(input);
  }

  /** Serialize an outgoing response message. */
  public InputStream streamResponse(ResponseT response) {
    return responseMarshaller.stream(response);
  }

  /** Handler for incoming calls. */
  public ServerCallHandler<RequestT, ResponseT> getServerCallHandler() {
    return handler;
  }

  /** Create a new method definition with a different call handler. */
  public ServerMethodDefinition<RequestT, ResponseT> withServerCallHandler(
      ServerCallHandler<RequestT, ResponseT> handler) {
    return new ServerMethodDefinition<RequestT, ResponseT>(
        name, requestMarshaller, responseMarshaller, handler);
  }
}
