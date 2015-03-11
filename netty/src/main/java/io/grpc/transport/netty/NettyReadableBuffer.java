/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.transport.netty;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;

import io.grpc.transport.AbstractReadableBuffer;
import io.netty.buffer.ByteBuf;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;

/**
 * A {@link Buffer} implementation that is backed by a Netty {@link ByteBuf}. This class does not
 * call {@link ByteBuf#retain}, so if that is needed it should be called prior to creating this
 * buffer.
 */
class NettyReadableBuffer extends AbstractReadableBuffer {
  private final ByteBuf buffer;
  private boolean closed;

  NettyReadableBuffer(ByteBuf buffer) {
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
  public NettyReadableBuffer readBytes(int length) {
    // The ByteBuf returned by readSlice() stores a reference to buffer but does not call retain().
    return new NettyReadableBuffer(buffer.readSlice(length).retain());
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
