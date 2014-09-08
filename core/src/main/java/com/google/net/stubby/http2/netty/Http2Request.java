package com.google.net.stubby.http2.netty;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Request;
import com.google.net.stubby.Response;
import com.google.net.stubby.transport.Framer;

import io.netty.handler.codec.http2.DefaultHttp2Headers;

import java.net.InetAddress;
import java.net.UnknownHostException;

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
                      Metadata.Headers headers,
                      Http2Codec.Http2Writer writer, Framer framer) {
    super(response.getId(), writer, framer);
    DefaultHttp2Headers.Builder headersBuilder = DefaultHttp2Headers.newBuilder();
    // TODO(user) Switch the ASCII requirement to false once Netty supports binary
    // headers.
    String[] headerValues = headers.serializeAscii();
    for (int i = 0; i < headerValues.length; i++) {
      headersBuilder.add(headerValues[i], headerValues[++i]);
    }
    headersBuilder.method("POST")
        .path("/" + operationName)
        .authority(HOST_NAME)
        .scheme("https")
        .add("content-type", Http2Session.PROTORPC);
    writer.writeHeaders(response.getId(), headersBuilder.build(), false);
    this.response = response;
  }

  @Override
  public Response getResponse() {
    return response;
  }
}
