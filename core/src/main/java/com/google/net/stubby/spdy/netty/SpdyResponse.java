package com.google.net.stubby.spdy.netty;

import com.google.net.stubby.Response;
import com.google.net.stubby.transport.Framer;

import io.netty.channel.Channel;
import io.netty.handler.codec.spdy.DefaultSpdySynReplyFrame;
import io.netty.handler.codec.spdy.SpdyHeaders;

/**
 * A SPDY based implementation of a {@link Response}.
 */
class SpdyResponse extends SpdyOperation implements Response {

  public static ResponseBuilder builder(final int id, final Channel channel,
                                        final Framer framer) {
    return new ResponseBuilder() {
      @Override
      public Response build(int id) {
        throw new UnsupportedOperationException();
      }

      @Override
      public Response build() {
        return new SpdyResponse(id, channel, framer);
      }
    };
  }

  public static DefaultSpdySynReplyFrame createSynReply(int id) {
    DefaultSpdySynReplyFrame synReplyFrame = new DefaultSpdySynReplyFrame(id);
    // TODO(user): Need to review status code handling
    synReplyFrame.headers().add(SpdyHeaders.HttpNames.STATUS, "200");
    return synReplyFrame;
  }

  private SpdyResponse(int id, Channel channel, Framer framer) {
    super(createSynReply(id), channel, framer);
  }

}
