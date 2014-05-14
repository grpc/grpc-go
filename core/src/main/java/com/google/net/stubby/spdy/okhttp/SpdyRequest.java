package com.google.net.stubby.spdy.okhttp;

import com.google.net.stubby.Request;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Response;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;
import com.google.net.stubby.transport.Transport;

import com.squareup.okhttp.internal.spdy.FrameWriter;

import java.io.IOException;

/**
 * A SPDY based implementation of {@link Request}
 */
public class SpdyRequest extends SpdyOperation implements Request {
  private final Response response;

  public SpdyRequest(FrameWriter frameWriter, String operationName,
                     Response response, RequestRegistry requestRegistry,
                     Framer framer) {
    super(response.getId(), frameWriter, framer);
    this.response = response;
    try {
      // Register this request.
      requestRegistry.register(this);

      frameWriter.synStream(false,
          false,
          getId(),
          0,
          0,
          0,
          Headers.createRequestHeaders(operationName));
    } catch (IOException ioe) {
      close(new Status(Transport.Code.UNKNOWN, ioe));
    }
  }

  @Override
  public Response getResponse() {
    return response;
  }
}