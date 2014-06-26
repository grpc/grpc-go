package com.google.net.stubby.newtransport;

/**
 * Abstract base class for {@link Buffer} implementations.
 */
public abstract class AbstractBuffer implements Buffer {

  @Override
  public final int readUnsignedMedium() {
    checkReadable(3);
    int b1 = readUnsignedByte();
    int b2 = readUnsignedByte();
    int b3 = readUnsignedByte();
    return b1 << 16 | b2 << 8 | b3;
  }


  @Override
  public final int readUnsignedShort() {
    checkReadable(2);
    int b1 = readUnsignedByte();
    int b2 = readUnsignedByte();
    return b1 << 8 | b2;
  }

  @Override
  public final int readInt() {
    checkReadable(4);
    int b1 = readUnsignedByte();
    int b2 = readUnsignedByte();
    int b3 = readUnsignedByte();
    int b4 = readUnsignedByte();
    return (b1 << 24) + (b2 << 16) + (b3 << 8) + b4;
  }

  @Override
  public boolean hasArray() {
    return false;
  }

  @Override
  public byte[] array() {
    throw new UnsupportedOperationException();
  }

  @Override
  public int arrayOffset() {
    throw new UnsupportedOperationException();
  }

  @Override
  public void close() {}

  protected final void checkReadable(int length) {
    if (readableBytes() < length) {
      throw new IndexOutOfBoundsException();
    }
  }
}
