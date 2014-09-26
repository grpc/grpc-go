package com.google.net.stubby.proto;

import com.google.common.io.ByteStreams;
import com.google.net.stubby.DeferredInputStream;
import com.google.protobuf.CodedOutputStream;
import com.google.protobuf.MessageLite;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.OutputStream;

import javax.annotation.Nullable;

/**
 * Implementation of {@link com.google.net.stubby.DeferredInputStream} backed by a protobuf.
 */
public class DeferredProtoInputStream extends DeferredInputStream<MessageLite> {

  // DeferredProtoInputStream is first initialized with a *message*. *partial* is initially null.
  // Once there has been a read operation on this stream, *message* is serialized to *partial* and
  // set to null.
  @Nullable private MessageLite message;
  @Nullable private ByteArrayInputStream partial;

  public DeferredProtoInputStream(MessageLite message) {
    this.message = message;
  }

  /**
   * Returns the original protobuf message. Returns null after this stream has been read.
   */
  @Nullable
  public MessageLite getDeferred() {
    return message;
  }

  @Override
  public int flushTo(OutputStream target) throws IOException {
    int written;
    if (message != null) {
      written = message.getSerializedSize();
      message.writeTo(target);
      message = null;
    } else {
      written = (int) ByteStreams.copy(partial, target);
      partial = null;
    }
    return written;
  }

  @Override
  public int read() throws IOException {
    if (message != null) {
      partial = new ByteArrayInputStream(message.toByteArray());
      message = null;
    }
    if (partial != null) {
      return partial.read();
    }
    return -1;
  }

  @Override
  public int read(byte[] b, int off, int len) throws IOException {
    if (message != null) {
      int size = message.getSerializedSize();
      if (len >= size) {
        // This is the only case that is zero-copy.
        CodedOutputStream stream = CodedOutputStream.newInstance(b, off, size);
        message.writeTo(stream);
        stream.flush();
        stream.checkNoSpaceLeft();

        message = null;
        partial = null;
        return size;
      }

      partial = new ByteArrayInputStream(message.toByteArray());
      message = null;
    }
    if (partial != null) {
      return partial.read(b, off, len);
    }
    return -1;
  }

  @Override
  public int available() throws IOException {
    if (message != null) {
      return message.getSerializedSize();
    } else if (partial != null) {
      return partial.available();
    }
    return 0;
  }
}
