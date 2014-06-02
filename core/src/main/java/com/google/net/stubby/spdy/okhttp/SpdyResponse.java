package com.google.net.stubby.spdy.okhttp;

import com.google.net.stubby.Response;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Framer;
import com.google.net.stubby.transport.Transport;

import com.squareup.okhttp.internal.spdy.FrameWriter;

import java.io.IOException;

/**
 * A SPDY based implementation of a {@link Response}.
 */
public class SpdyResponse extends SpdyOperation implements Response {

  public static ResponseBuilder builder(final int id, final FrameWriter framewriter,
                                        final Framer framer) {
    return new ResponseBuilder() {
      @Override
      public Response build(int id) {
        throw new UnsupportedOperationException();
      }

      @Override
      public Response build() {
        return new SpdyResponse(id, framewriter, framer);
      }
    };
  }

  private SpdyResponse(int id, FrameWriter frameWriter, Framer framer) {
    super(id, frameWriter, framer);
    try {
      frameWriter.synStream(false, false, getId(), 0, 0, 0, Headers.createResponseHeaders());
    } catch (IOException ioe) {
      close(new Status(Transport.Code.INTERNAL, ioe));
    }
  }
}