package com.google.net.stubby.newtransport.okhttp;

import com.google.net.stubby.newtransport.AbstractBuffer;
import com.google.net.stubby.newtransport.Buffer;

import java.io.EOFException;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;

/**
 * A {@link Buffer} implementation that is backed by an {@link okio.Buffer}.
 */
class OkHttpBuffer extends AbstractBuffer {
  private final okio.Buffer buffer;

  OkHttpBuffer(okio.Buffer buffer) {
    this.buffer = buffer;
  }

  @Override
  public int readableBytes() {
    return (int) buffer.size();
  }

  @Override
  public int readUnsignedByte() {
    return buffer.readByte() & 0x000000FF;
  }

  @Override
  public void skipBytes(int length) {
    try {
      buffer.skip(length);
    } catch (EOFException e) {
      throw new IndexOutOfBoundsException(e.getMessage());
    }
  }

  @Override
  public void readBytes(byte[] dest, int destOffset, int length) {
    buffer.read(dest, destOffset, length);
  }

  @Override
  public void readBytes(ByteBuffer dest) {
    // We are not using it.
    throw new UnsupportedOperationException();
  }

  @Override
  public void readBytes(OutputStream dest, int length) throws IOException {
    buffer.writeTo(dest, length);
  }

  @Override
  public Buffer readBytes(int length) {
    okio.Buffer buf = new okio.Buffer();
    buf.write(buffer, length);
    return new OkHttpBuffer(buf);
  }

  @Override
  public void close() {
    buffer.clear();
  }
}
