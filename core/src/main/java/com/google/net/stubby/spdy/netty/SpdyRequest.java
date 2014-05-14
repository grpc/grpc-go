package com.google.net.stubby.spdy.netty;

import com.google.net.stubby.Request;
import com.google.net.stubby.Response;
import com.google.net.stubby.transport.Framer;

import io.netty.channel.Channel;
import io.netty.handler.codec.spdy.DefaultSpdySynStreamFrame;
import io.netty.handler.codec.spdy.SpdyHeaders;

import java.net.InetAddress;
import java.net.UnknownHostException;

/**
 * A SPDY based implementation of {@link Request}
 */
class SpdyRequest extends SpdyOperation implements Request {

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

  private static DefaultSpdySynStreamFrame createHeadersFrame(int id, String operationName) {
    DefaultSpdySynStreamFrame headersFrame = new DefaultSpdySynStreamFrame(id, 0, (byte) 0);
    headersFrame.headers().add(SpdyHeaders.HttpNames.METHOD, "POST");
    // TODO(user) Convert operation names to URIs
    headersFrame.headers().add(SpdyHeaders.HttpNames.PATH, "/"  + operationName);
    headersFrame.headers().add(SpdyHeaders.HttpNames.VERSION, "HTTP/1.1");
    headersFrame.headers().add(SpdyHeaders.HttpNames.HOST, HOST_NAME);
    headersFrame.headers().add(SpdyHeaders.HttpNames.SCHEME, "https");
    headersFrame.headers().add("content-type", SpdySession.PROTORPC);
    return headersFrame;
  }

  private final Response response;

  public SpdyRequest(Response response, Channel channel, String operationName,
                    Framer framer) {
    super(createHeadersFrame(response.getId(), operationName), channel, framer);
    this.response = response;
  }

  @Override
  public Response getResponse() {
    return response;
  }
}
