package com.google.net.stubby.http2.netty;

import com.google.net.stubby.Request;
import com.google.net.stubby.Response;
import com.google.net.stubby.transport.Framer;

import java.net.InetAddress;
import java.net.UnknownHostException;
import java.util.Map;

import io.netty.handler.codec.http2.DefaultHttp2Headers;

/**
 * A HTTP2 based implementation of {@link Request}
 */
class Http2Request extends Http2Operation implements Request {

  // TODO(user): Inject this
  private static final String HOST_NAME;
  static {
    String hostName;
    try {
      hostName = InetAddress.getLocalHost().getHostName();
    } catch (UnknownHostException uhe) {
      hostName = "localhost";
    }
    HOST_NAME = hostName;
  }

  private final Response response;

  public Http2Request(Response response, String operationName,
                      Map<String, String> headers,
                      Http2Codec.Http2Writer writer, Framer framer) {
    super(response.getId(), writer, framer);
    DefaultHttp2Headers.Builder headersBuilder = DefaultHttp2Headers.newBuilder();
    for (Map.Entry<String, String> entry : headers.entrySet()) {
      headersBuilder.add(entry.getKey(), entry.getValue());
    }
    headersBuilder.method("POST")
        .path("/" + operationName)
        .authority(HOST_NAME)
        .scheme("https")
        .add("content-type", Http2Session.PROTORPC);
    writer.writeHeaders(response.getId(), headersBuilder.build(), false, true);
    this.response = response;
  }

  @Override
  public Response getResponse() {
    return response;
  }
}
