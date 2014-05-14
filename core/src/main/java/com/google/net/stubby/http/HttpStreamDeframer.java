package com.google.net.stubby.http;

import com.google.net.stubby.Operation;
import com.google.net.stubby.transport.InputStreamDeframer;

import java.io.InputStream;

/**
 * Simple deframer which does not have to deal with transport level framing.
 */
public class HttpStreamDeframer extends InputStreamDeframer {

  public HttpStreamDeframer() {
  }

  @Override
  public int deframe(InputStream frame, Operation target) {
    int remaining = super.deframe(frame, target);
    if (remaining > 0) {
      throw new IllegalStateException("GRPC stream not correctly aligned");
    }
    return 0;
  }
}
