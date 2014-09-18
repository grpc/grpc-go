package com.google.net.stubby.http2.netty;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Request;
import com.google.net.stubby.Response;
import com.google.net.stubby.transport.Framer;

import io.netty.handler.codec.AsciiString;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;

import java.net.InetAddress;
import java.net.UnknownHostException;

/**
 * A HTTP2 based implementation of {@link Request}
 */
class Http2Request extends Http2Operation implements Request {
  private static final AsciiString POST = new AsciiString("POST");
  private static final AsciiString HOST_NAME;
  private static final AsciiString HTTPS = new AsciiString("https");
  // TODO(user): Inject this
  static {
    String hostName;
    try {
      hostName = InetAddress.getLocalHost().getHostName();
    } catch (UnknownHostException uhe) {
      hostName = "localhost";
    }
    HOST_NAME = new AsciiString(hostName);
  }

  private final Response response;

  public Http2Request(Response response, String operationName,
                      Metadata.Headers headers,
                      Http2Codec.Http2Writer writer, Framer framer) {
    super(response.getId(), writer, framer);
    Http2Headers http2Headers = new DefaultHttp2Headers();
    byte[][] headerValues = headers.serialize();
    for (int i = 0; i < headerValues.length; i++) {
      http2Headers.add(new AsciiString(headerValues[i], false),
          new AsciiString(headerValues[++i], false));
    }
    http2Headers.method(POST)
        .path(new AsciiString("/" + operationName))
        .authority(HOST_NAME)
        .scheme(HTTPS)
        .add(Http2Session.CONTENT_TYPE, Http2Session.PROTORPC);
    writer.writeHeaders(response.getId(), http2Headers, false);
    this.response = response;
  }

  @Override
  public Response getResponse() {
    return response;
  }
}
