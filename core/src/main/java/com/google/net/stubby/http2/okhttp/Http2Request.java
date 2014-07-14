package com.google.net.stubby.http2.okhttp;

import com.google.net.stubby.Request;
import com.google.net.stubby.RequestRegistry;
import com.google.net.stubby.Response;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;
import com.google.net.stubby.transport.Transport;

import com.squareup.okhttp.internal.spdy.FrameWriter;
import com.squareup.okhttp.internal.spdy.Header;

import java.io.IOException;
import java.util.List;
import java.util.Map;

/**
 * A HTTP2 based implementation of {@link Request}
 */
public class Http2Request extends Http2Operation implements Request {
  private final Response response;

  public Http2Request(FrameWriter frameWriter, String operationName,
                     Map<String, String> headers,
                     Response response, RequestRegistry requestRegistry,
                     Framer framer) {
    super(response.getId(), frameWriter, framer);
    this.response = response;
    try {
      // Register this request.
      requestRegistry.register(this);

      List<Header> requestHeaders = Headers.createRequestHeaders(operationName);
      for (Map.Entry<String, String> entry : headers.entrySet()) {
        requestHeaders.add(new Header(entry.getKey(), entry.getValue()));
      }
      frameWriter.synStream(false,
          false,
          getId(),
          0,
          0,
          0,
          requestHeaders);
    } catch (IOException ioe) {
      close(new Status(Transport.Code.UNKNOWN, ioe));
    }
  }

  @Override
  public Response getResponse() {
    return response;
  }
}