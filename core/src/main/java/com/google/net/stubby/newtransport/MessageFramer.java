package com.google.net.stubby.newtransport;

import com.google.net.stubby.GrpcFramingUtil;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Transport;
import com.google.protobuf.CodedOutputStream;
import com.google.protobuf.WireFormat;

import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;
import java.nio.charset.Charset;
import java.util.Arrays;

/**
 * Default {@link Framer} implementation.
 */
public class MessageFramer implements Framer {

  /**
   * Size of the GRPC message frame header which consists of
   * 1 byte for the type (payload, context, status)
   * 4 bytes for the length of the message
   */
  private static final int MESSAGE_HEADER_SIZE = 5;

  /**
   * UTF-8 charset which is used for key name encoding in context values
   */
  private static final Charset UTF_8 = Charset.forName("UTF-8");

  /**
   * Precomputed protobuf tags for ContextValue
   */
  private static final byte[] VALUE_TAG;
  private static final byte[] KEY_TAG;


  static {
    // Initialize constants for serializing context-value in a protobuf compatible manner
    try {
      byte[] buf = new byte[8];
      CodedOutputStream coded = CodedOutputStream.newInstance(buf);
      coded.writeTag(Transport.ContextValue.KEY_FIELD_NUMBER, WireFormat.WIRETYPE_LENGTH_DELIMITED);
      coded.flush();
      KEY_TAG = Arrays.copyOf(buf, coded.getTotalBytesWritten());
      coded = CodedOutputStream.newInstance(buf);
      coded.writeTag(Transport.ContextValue.VALUE_FIELD_NUMBER,
          WireFormat.WIRETYPE_LENGTH_DELIMITED);
      coded.flush();
      VALUE_TAG = Arrays.copyOf(buf, coded.getTotalBytesWritten());
    } catch (IOException ioe) {
      // Unrecoverable
      throw new RuntimeException(ioe);
    }
  }

  private CompressionFramer framer;
  private final ByteBuffer scratch = ByteBuffer.allocate(16);

  public MessageFramer(Sink<ByteBuffer> sink, int maxFrameSize) {
    // TODO(user): maxFrameSize should probably come from a 'Platform' class
    framer = new CompressionFramer(sink, maxFrameSize, false, maxFrameSize / 16);
  }

  /**
   * Sets whether compression is encouraged.
   */
  public void setAllowCompression(boolean enable) {
    framer.setAllowCompression(enable);
  }

  /**
   * Set the preferred compression level for when compression is enabled.
   * @param level the preferred compression level, or {@code -1} to use the framing default
   * @see java.util.zip.Deflater#setLevel
   */
  public void setCompressionLevel(int level) {
    framer.setCompressionLevel(level);
  }

  @Override
  public void writePayload(InputStream message, int messageLength) {
    try {
      scratch.clear();
      scratch.put(GrpcFramingUtil.PAYLOAD_FRAME);
      scratch.putInt(messageLength);
      framer.write(scratch.array(), 0, scratch.position());
      if (messageLength != framer.write(message)) {
        throw new RuntimeException("Message length was inaccurate");
      }
      framer.endOfMessage();
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }


  @Override
  public void writeContext(String key, InputStream message, int messageLen) {
    try {
      scratch.clear();
      scratch.put(GrpcFramingUtil.CONTEXT_VALUE_FRAME);
      byte[] keyBytes = key.getBytes(UTF_8);
      int lenKeyPrefix = KEY_TAG.length +
          CodedOutputStream.computeRawVarint32Size(keyBytes.length);
      int lenValPrefix = VALUE_TAG.length + CodedOutputStream.computeRawVarint32Size(messageLen);
      int totalLen = lenKeyPrefix + keyBytes.length + lenValPrefix + messageLen;
      scratch.putInt(totalLen);
      framer.write(scratch.array(), 0, scratch.position());

      // Write key
      scratch.clear();
      scratch.put(KEY_TAG);
      writeRawVarInt32(keyBytes.length, scratch);
      framer.write(scratch.array(), 0, scratch.position());
      framer.write(keyBytes, 0, keyBytes.length);

      // Write value
      scratch.clear();
      scratch.put(VALUE_TAG);
      writeRawVarInt32(messageLen, scratch);
      framer.write(scratch.array(), 0, scratch.position());
      if (messageLen != framer.write(message)) {
        throw new RuntimeException("Message length was inaccurate");
      }
      framer.endOfMessage();
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }

  @Override
  public void writeStatus(Status status) {
    short code = (short) status.getCode().ordinal();
    scratch.clear();
    scratch.put(GrpcFramingUtil.STATUS_FRAME);
    int length = 2;
    scratch.putInt(length);
    scratch.putShort(code);
    framer.write(scratch.array(), 0, scratch.position());
    framer.endOfMessage();
  }

  @Override
  public void flush() {
    framer.flush();
  }

  @Override
  public void close() {
    // TODO(user): Returning buffer to a pool would go here
    framer.close();
    framer = null;
  }

  @Override
  public void dispose() {
    // TODO(user): Returning buffer to a pool would go here
    framer = null;
  }

  /**
   * Write a raw VarInt32 to the buffer
   */
  private static void writeRawVarInt32(int value, ByteBuffer bytebuf) {
    while (true) {
      if ((value & ~0x7F) == 0) {
        bytebuf.put((byte) value);
        return;
      } else {
        bytebuf.put((byte) ((value & 0x7F) | 0x80));
        value >>>= 7;
      }
    }
  }
}
