package com.google.net.stubby;

/**
 * A response {@link Operation} passed by a client to
 * {@link Session#startRequest(String, ResponseBuilder)}
 * when starting a remote call.
 */
public interface Response extends Operation {

  public static interface ResponseBuilder {
    /**
     * Build the response with the specified id
     */
    public Response build(int id);

    /**
     * Build the response
     */
    public Response build();
  }
}
