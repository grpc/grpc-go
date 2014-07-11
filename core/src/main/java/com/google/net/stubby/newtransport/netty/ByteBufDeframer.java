package com.google.net.stubby.newtransport.netty;

import com.google.net.stubby.newtransport.Deframer;
import com.google.net.stubby.newtransport.Framer;
import com.google.net.stubby.newtransport.TransportFrameUtil;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.buffer.ByteBufInputStream;
import io.netty.buffer.CompositeByteBuf;
import io.netty.buffer.UnpooledByteBufAllocator;

import java.io.DataInputStream;
import java.io.IOException;
import java.nio.ByteOrder;

/**
 * Parse a sequence of {@link ByteBuf} instances that represent the frames of a GRPC call
 */
public class ByteBufDeframer extends Deframer<ByteBuf> {

  private final CompositeByteBuf buffer;

  public ByteBufDeframer(Framer target) {
    this(UnpooledByteBufAllocator.DEFAULT, target);
  }

  public ByteBufDeframer(ByteBufAllocator alloc, Framer target) {
    super(target);
    buffer = alloc.compositeBuffer();
  }

  public void dispose() {
    // Remove the components from the composite buffer. This should set the reference
    // count on all buffers to zero.
    buffer.removeComponents(0, buffer.numComponents());

    // Release the composite buffer
    buffer.release();
  }

  @Override
  protected DataInputStream prefix(ByteBuf frame) throws IOException {
    buffer.addComponent(frame);
    buffer.writerIndex(buffer.writerIndex() + frame.writerIndex() - frame.readerIndex());
    return new DataInputStream(new ByteBufInputStream(buffer));
  }

  @Override
  protected int consolidate() {
    buffer.consolidate();
    return buffer.readableBytes();
  }

  @Override
  protected ByteBuf decompress(ByteBuf frame) throws IOException {
    frame = frame.order(ByteOrder.BIG_ENDIAN);
    int compressionType = frame.readUnsignedByte();
    int frameLength = frame.readUnsignedMedium();
    if (frameLength != frame.readableBytes()) {
      throw new IllegalArgumentException("GRPC and buffer lengths misaligned. Frame length="
          + frameLength + ", readableBytes=" + frame.readableBytes());
    }
    if (TransportFrameUtil.isNotCompressed(compressionType)) {
      // Need to retain the frame as we may be holding it over channel events
      frame.retain();
      return frame;
    }
    throw new IOException("Unknown compression type " + compressionType);
  }
}
