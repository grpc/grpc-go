package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.GrpcFramingUtil.PAYLOAD_FRAME;
import static com.google.net.stubby.GrpcFramingUtil.STATUS_FRAME;
import static io.netty.util.CharsetUtil.UTF_8;

import com.google.common.io.ByteStreams;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.AbstractStream;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;

import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.InputStream;

/**
 * Utility methods for supporting Netty tests.
 */
public class NettyTestUtil {

  static String toString(InputStream in) throws Exception {
    byte[] bytes = new byte[in.available()];
    ByteStreams.readFully(in, bytes);
    return new String(bytes, UTF_8);
  }

  static ByteBuf messageFrame(String message) throws Exception {
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);
    if (!AbstractStream.GRPC_V2_PROTOCOL) {
      dos.write(PAYLOAD_FRAME);
      dos.writeInt(message.length());
    }
    dos.write(message.getBytes(UTF_8));
    dos.close();

    // Write the compression header followed by the context frame.
    return compressionFrame(os.toByteArray());
  }

  static ByteBuf statusFrame(Status status) throws Exception {
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);
    short code = (short) status.getCode().value();
    dos.write(STATUS_FRAME);
    int length = 2;
    dos.writeInt(length);
    dos.writeShort(code);

    // Write the compression header followed by the context frame.
    return compressionFrame(os.toByteArray());
  }

  static ByteBuf compressionFrame(byte[] data) {
    ByteBuf buf = Unpooled.buffer();
    if (AbstractStream.GRPC_V2_PROTOCOL) {
      buf.writeByte(0);
    }
    buf.writeInt(data.length);
    buf.writeBytes(data);
    return buf;
  }
}
