/*
 * Copyright 2018 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.alts.internal;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.fail;

import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.security.GeneralSecurityException;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AltsFraming}. */
@RunWith(JUnit4.class)
public class AltsFramingTest {

  @Test
  public void parserFrameLengthNegativeFails() throws GeneralSecurityException {
    AltsFraming.Parser parser = new AltsFraming.Parser();
    // frame length + one remaining byte (required)
    ByteBuffer buffer = ByteBuffer.allocate(AltsFraming.getFrameLengthHeaderSize() + 1);
    buffer.order(ByteOrder.LITTLE_ENDIAN);
    buffer.putInt(-1); // write invalid length
    buffer.put((byte) 0); // write some byte
    buffer.flip();

    try {
      parser.readBytes(buffer);
      fail("Exception expected");
    } catch (IllegalArgumentException ex) {
      assertThat(ex).hasMessageThat().contains("Invalid frame length");
    }
  }

  @Test
  public void parserFrameLengthSmallerMessageTypeFails() throws GeneralSecurityException {
    AltsFraming.Parser parser = new AltsFraming.Parser();
    // frame length + one remaining byte (required)
    ByteBuffer buffer = ByteBuffer.allocate(AltsFraming.getFrameLengthHeaderSize() + 1);
    buffer.order(ByteOrder.LITTLE_ENDIAN);
    buffer.putInt(AltsFraming.getFrameMessageTypeHeaderSize() - 1); // write invalid length
    buffer.put((byte) 0); // write some byte
    buffer.flip();

    try {
      parser.readBytes(buffer);
      fail("Exception expected");
    } catch (IllegalArgumentException ex) {
      assertThat(ex).hasMessageThat().contains("Invalid frame length");
    }
  }

  @Test
  public void parserFrameLengthTooLargeFails() throws GeneralSecurityException {
    AltsFraming.Parser parser = new AltsFraming.Parser();
    // frame length + one remaining byte (required)
    ByteBuffer buffer = ByteBuffer.allocate(AltsFraming.getFrameLengthHeaderSize() + 1);
    buffer.order(ByteOrder.LITTLE_ENDIAN);
    buffer.putInt(AltsFraming.getMaxDataLength() + 1); // write invalid length
    buffer.put((byte) 0); // write some byte
    buffer.flip();

    try {
      parser.readBytes(buffer);
      fail("Exception expected");
    } catch (IllegalArgumentException ex) {
      assertThat(ex).hasMessageThat().contains("Invalid frame length");
    }
  }

  @Test
  public void parserFrameLengthMaxOk() throws GeneralSecurityException {
    AltsFraming.Parser parser = new AltsFraming.Parser();
    // length of type header + data
    int dataLength = AltsFraming.getMaxDataLength();
    // complete frame + 1 byte
    ByteBuffer buffer =
        ByteBuffer.allocate(AltsFraming.getFrameLengthHeaderSize() + dataLength + 1);
    buffer.order(ByteOrder.LITTLE_ENDIAN);
    buffer.putInt(dataLength); // write invalid length
    buffer.putInt(6); // default message type
    buffer.put(new byte[dataLength - AltsFraming.getFrameMessageTypeHeaderSize()]); // write data
    buffer.put((byte) 0);
    buffer.flip();

    parser.readBytes(buffer);

    assertThat(parser.isComplete()).isTrue();
    assertThat(buffer.remaining()).isEqualTo(1);
  }

  @Test
  public void parserFrameLengthZeroOk() throws GeneralSecurityException {
    AltsFraming.Parser parser = new AltsFraming.Parser();
    int dataLength = AltsFraming.getFrameMessageTypeHeaderSize();
    // complete frame + 1 byte
    ByteBuffer buffer =
        ByteBuffer.allocate(AltsFraming.getFrameLengthHeaderSize() + dataLength + 1);
    buffer.order(ByteOrder.LITTLE_ENDIAN);
    buffer.putInt(dataLength); // write invalid length
    buffer.putInt(6); // default message type
    buffer.put((byte) 0);
    buffer.flip();

    parser.readBytes(buffer);

    assertThat(parser.isComplete()).isTrue();
    assertThat(buffer.remaining()).isEqualTo(1);
  }
}
