package com.google.net.stubby.newtransport;

import com.google.common.base.Preconditions;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;

/**
 * Base class for a wrapper around another {@link Buffer}.
 */
public abstract class ForwardingBuffer implements Buffer {

  private final Buffer buf;

  public ForwardingBuffer(Buffer buf) {
    this.buf = Preconditions.checkNotNull(buf, "buf");
  }

  @Override
  public int readableBytes() {
    return buf.readableBytes();
  }

  @Override
  public int readUnsignedByte() {
    return buf.readUnsignedByte();
  }

  @Override
  public int readUnsignedMedium() {
    return buf.readUnsignedMedium();
  }

  @Override
  public int readUnsignedShort() {
    return buf.readUnsignedShort();
  }

  @Override
  public int readInt() {
    return buf.readInt();
  }

  @Override
  public void skipBytes(int length) {
    buf.skipBytes(length);
  }

  @Override
  public void readBytes(byte[] dest, int destOffset, int length) {
    buf.readBytes(dest, destOffset, length);
  }

  @Override
  public void readBytes(ByteBuffer dest) {
    buf.readBytes(dest);
  }

  @Override
  public void readBytes(OutputStream dest, int length) throws IOException {
    buf.readBytes(dest, length);
  }

  @Override
  public boolean hasArray() {
    return buf.hasArray();
  }

  @Override
  public byte[] array() {
    return buf.array();
  }

  @Override
  public int arrayOffset() {
    return buf.arrayOffset();
  }

  @Override
  public void close() {
    buf.close();
  }
}