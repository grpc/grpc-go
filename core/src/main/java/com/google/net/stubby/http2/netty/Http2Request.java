package com.google.net.stubby.http2.netty;

import com.google.net.stubby.Request;
import com.google.net.stubby.Response;
import com.google.net.stubby.transport.Framer;

import io.netty.channel.Channel;
import io.netty.handler.codec.http2.draft10.DefaultHttp2Headers;
import io.netty.handler.codec.http2.draft10.Http2Headers;
import io.netty.handler.codec.http2.draft10.frame.DefaultHttp2HeadersFrame;

import java.net.InetAddress;
import java.net.UnknownHostException;

/**
 * A SPDY based implementation of {@link Request}
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

  private static DefaultHttp2HeadersFrame createHeadersFrame(int id, String operationName) {
    Http2Headers headers = DefaultHttp2Headers.newBuilder()
        .setMethod("POST")
        .setPath("/" + operationName)
        .setAuthority(HOST_NAME)
        .setScheme("https")
        .add("content-type", Http2Session.PROTORPC)
        .build();
    return new DefaultHttp2HeadersFrame.Builder().setStreamId(id).setHeaders(headers).build();
  }

  private final Response response;

  public Http2Request(Response response, Channel channel, String operationName, Framer framer) {
    super(response.getId(), channel, framer);
    channel.write(createHeadersFrame(response.getId(), operationName));
    this.response = response;
  }

  @Override
  public Response getResponse() {
    return response;
  }
}
