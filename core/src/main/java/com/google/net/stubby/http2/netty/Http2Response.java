package com.google.net.stubby.http2.netty;

import com.google.net.stubby.Response;
import com.google.net.stubby.transport.Framer;

import io.netty.handler.codec.http2.Http2Headers;

import io.netty.handler.codec.http2.DefaultHttp2Headers;

/**
 * A HTTP2 based implementation of a {@link Response}.
 */
class Http2Response extends Http2Operation implements Response {

  public static ResponseBuilder builder(final int id, final Http2Codec.Http2Writer writer,
      final Framer framer) {
    return new ResponseBuilder() {
      @Override
      public Response build(int id) {
        throw new UnsupportedOperationException();
      }

      @Override
      public Response build() {
        return new Http2Response(id, writer, framer);
      }
    };
  }

  private Http2Response(int id, Http2Codec.Http2Writer writer, Framer framer) {
    super(id, writer, framer);
    Http2Headers headers = DefaultHttp2Headers.newBuilder().status("200")
        .add("content-type", Http2Session.PROTORPC).build();
    writer.writeHeaders(id, headers, false);
  }
}
