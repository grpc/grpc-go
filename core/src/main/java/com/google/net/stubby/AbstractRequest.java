package com.google.net.stubby;

/**
 * Common implementation for {@link Request} objects.
 */
public abstract class AbstractRequest extends AbstractOperation implements Request {

  private final Response response;

  /**
   * Constructor that takes a pre-built {@link Response} and uses it's id
   */
  public AbstractRequest(Response response) {
    super(response.getId());
    this.response = response;
  }

  /**
   * Constructor that takes a {@link Response.ResponseBuilder} to
   * be built with the same id as this request
   */
  public AbstractRequest(int id, Response.ResponseBuilder responseBuilder) {
    super(id);
    this.response = responseBuilder.build(id);
  }

  @Override
  public Response getResponse() {
    return response;
  }
}
