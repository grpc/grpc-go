package com.google.net.stubby;

/**
 * A request {@link Operation} created by a client by calling
 * {@link Session#startRequest(String, Response.ResponseBuilder)}
 */
public interface Request extends Operation {
  /**
   *  Reference to the response operation that consumes replies to this request.
   */
  public Response getResponse();
}
