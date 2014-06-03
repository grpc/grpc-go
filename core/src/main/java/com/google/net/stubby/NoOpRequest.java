package com.google.net.stubby;

/**
 * A request that does no work.
 */
public class NoOpRequest extends AbstractRequest {

  public NoOpRequest(Response response) {
    super(response);
  }
}
