package com.google.net.stubby.transport.netty;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.net.stubby.transport.AbstractBuffer;
import com.google.net.stubby.transport.Buffer;

import io.netty.buffer.ByteBuf;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;

/**
 * A {@link Buffer} implementation that is backed by a Netty {@link ByteBuf}. This class does not
 * call {@link ByteBuf#retain}, so if that is needed it should be called prior to creating this
 * buffer.
 */
class NettyBuffer extends AbstractBuffer {
  private final ByteBuf buffer;
  private boolean closed;

  NettyBuffer(ByteBuf buffer) {
    this.buffer = Preconditions.checkNotNull(buffer, "buffer");
  }

  ByteBuf buffer() {
    return buffer;
  }

  @Override
  public int readableBytes() {
    return buffer.readableBytes();
  }

  @Override
  public void skipBytes(int length) {
    buffer.skipBytes(length);
  }

  @Override
  public int readUnsignedByte() {
    return buffer.readUnsignedByte();
  }

  @Override
  public void readBytes(byte[] dest, int index, int length) {
    buffer.readBytes(dest, index, length);
  }

  @Override
  public void readBytes(ByteBuffer dest) {
    buffer.readBytes(dest);
  }

  @Override
  public void readBytes(OutputStream dest, int length) {
    try {
      buffer.readBytes(dest, length);
    } catch (IOException e) {
      throw Throwables.propagate(e);
    }
  }

  @Override
  public NettyBuffer readBytes(int length) {
    // The ByteBuf returned by readSlice() stores a reference to buffer but does not call retain().
    return new NettyBuffer(buffer.readSlice(length).retain());
  }

  @Override
  public boolean hasArray() {
    return buffer.hasArray();
  }

  @Override
  public byte[] array() {
    return buffer.array();
  }

  @Override
  public int arrayOffset() {
    return buffer.arrayOffset() + buffer.readerIndex();
  }

  /**
   * If the first call to close, calls {@link ByteBuf#release} to release the internal Netty buffer.
   */
  @Override
  public void close() {
    // Don't allow slices to close. Also, only allow close to be called once.
    if (!closed) {
      closed = true;
      buffer.release();
    }
  }
}
