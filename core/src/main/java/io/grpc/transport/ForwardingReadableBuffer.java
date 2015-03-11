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

package io.grpc.transport;

import com.google.common.base.Preconditions;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;

/**
 * Base class for a wrapper around another {@link ReadableBuffer}.
 *
 * <p>This class just passes every operation through to the underlying buffer. Subclasses may
 * override methods to intercept concertain operations.
 */
public abstract class ForwardingReadableBuffer implements ReadableBuffer {

  private final ReadableBuffer buf;

  /**
   * Constructor.
   *
   * @param buf the underlying buffer
   */
  public ForwardingReadableBuffer(ReadableBuffer buf) {
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
  public ReadableBuffer readBytes(int length) {
    return buf.readBytes(length);
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
