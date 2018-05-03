/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.protobuf.lite;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.fail;

import com.google.common.io.ByteStreams;
import com.google.protobuf.ByteString;
import com.google.protobuf.Empty;
import com.google.protobuf.Enum;
import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.Type;
import io.grpc.Drainable;
import io.grpc.KnownLength;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.PrototypeMarshaller;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.internal.GrpcUtil;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.Arrays;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ProtoLiteUtils}. */
@RunWith(JUnit4.class)
public class ProtoLiteUtilsTest {

  @Rule public final ExpectedException thrown = ExpectedException.none();

  private Marshaller<Type> marshaller = ProtoLiteUtils.marshaller(Type.getDefaultInstance());
  private Type proto = Type.newBuilder().setName("name").build();

  @Test
  public void testPassthrough() {
    assertSame(proto, marshaller.parse(marshaller.stream(proto)));
  }

  @Test
  public void testRoundtrip() throws Exception {
    InputStream is = marshaller.stream(proto);
    is = new ByteArrayInputStream(ByteStreams.toByteArray(is));
    assertEquals(proto, marshaller.parse(is));
  }

  @Test
  public void testInvalidatedMessage() throws Exception {
    InputStream is = marshaller.stream(proto);
    // Invalidates message, and drains all bytes
    byte[] unused = ByteStreams.toByteArray(is);
    try {
      ((ProtoInputStream) is).message();
      fail("Expected exception");
    } catch (IllegalStateException ex) {
      // expected
    }
    // Zero bytes is the default message
    assertEquals(Type.getDefaultInstance(), marshaller.parse(is));
  }

  @Test
  public void parseInvalid() throws Exception {
    InputStream is = new ByteArrayInputStream(new byte[] {-127});
    try {
      marshaller.parse(is);
      fail("Expected exception");
    } catch (StatusRuntimeException ex) {
      assertEquals(Status.Code.INTERNAL, ex.getStatus().getCode());
      assertNotNull(((InvalidProtocolBufferException) ex.getCause()).getUnfinishedMessage());
    }
  }

  @Test
  public void testMismatch() throws Exception {
    Marshaller<Enum> enumMarshaller = ProtoLiteUtils.marshaller(Enum.getDefaultInstance());
    // Enum's name and Type's name are both strings with tag 1.
    Enum altProto = Enum.newBuilder().setName(proto.getName()).build();
    assertEquals(proto, marshaller.parse(enumMarshaller.stream(altProto)));
  }

  @Test
  public void introspection() throws Exception {
    Marshaller<Enum> enumMarshaller = ProtoLiteUtils.marshaller(Enum.getDefaultInstance());
    PrototypeMarshaller<Enum> prototypeMarshaller = (PrototypeMarshaller<Enum>) enumMarshaller;
    assertSame(Enum.getDefaultInstance(), prototypeMarshaller.getMessagePrototype());
    assertSame(Enum.class, prototypeMarshaller.getMessageClass());
  }

  @Test
  public void marshallerShouldNotLimitProtoSize() throws Exception {
    // The default limit is 64MB. Using a larger proto to verify that the limit is not enforced.
    byte[] bigName = new byte[70 * 1024 * 1024];
    Arrays.fill(bigName, (byte) 32);

    proto = Type.newBuilder().setNameBytes(ByteString.copyFrom(bigName)).build();

    // Just perform a round trip to verify that it works.
    testRoundtrip();
  }

  @Test
  public void testAvailable() throws Exception {
    InputStream is = marshaller.stream(proto);
    assertEquals(proto.getSerializedSize(), is.available());
    is.read();
    assertEquals(proto.getSerializedSize() - 1, is.available());
    while (is.read() != -1) {}
    assertEquals(-1, is.read());
    assertEquals(0, is.available());
  }

  @Test
  public void testEmpty() throws IOException {
    Marshaller<Empty> marshaller = ProtoLiteUtils.marshaller(Empty.getDefaultInstance());
    InputStream is = marshaller.stream(Empty.getDefaultInstance());
    assertEquals(0, is.available());
    byte[] b = new byte[10];
    assertEquals(-1, is.read(b));
    assertArrayEquals(new byte[10], b);
    // Do the same thing again, because the internal state may be different
    assertEquals(-1, is.read(b));
    assertArrayEquals(new byte[10], b);
    assertEquals(-1, is.read());
    assertEquals(0, is.available());
  }

  @Test
  public void testDrainTo_all() throws Exception {
    byte[] golden = ByteStreams.toByteArray(marshaller.stream(proto));
    InputStream is = marshaller.stream(proto);
    Drainable d = (Drainable) is;
    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    int drained = d.drainTo(baos);
    assertEquals(baos.size(), drained);
    assertArrayEquals(golden, baos.toByteArray());
    assertEquals(0, is.available());
  }

  @Test
  public void testDrainTo_partial() throws Exception {
    final byte[] golden;
    {
      InputStream is = marshaller.stream(proto);
      is.read();
      golden = ByteStreams.toByteArray(is);
    }
    InputStream is = marshaller.stream(proto);
    is.read();
    Drainable d = (Drainable) is;
    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    int drained = d.drainTo(baos);
    assertEquals(baos.size(), drained);
    assertArrayEquals(golden, baos.toByteArray());
    assertEquals(0, is.available());
  }

  @Test
  public void testDrainTo_none() throws Exception {
    InputStream is = marshaller.stream(proto);
    byte[] unused = ByteStreams.toByteArray(is);
    Drainable d = (Drainable) is;
    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    assertEquals(0, d.drainTo(baos));
    assertArrayEquals(new byte[0], baos.toByteArray());
    assertEquals(0, is.available());
  }

  @Test
  public void metadataMarshaller_roundtrip() {
    Metadata.BinaryMarshaller<Type> metadataMarshaller =
        ProtoLiteUtils.metadataMarshaller(Type.getDefaultInstance());
    assertEquals(proto, metadataMarshaller.parseBytes(metadataMarshaller.toBytes(proto)));
  }

  @Test
  public void metadataMarshaller_invalid() {
    Metadata.BinaryMarshaller<Type> metadataMarshaller =
        ProtoLiteUtils.metadataMarshaller(Type.getDefaultInstance());
    try {
      metadataMarshaller.parseBytes(new byte[] {-127});
      fail("Expected exception");
    } catch (IllegalArgumentException ex) {
      assertNotNull(((InvalidProtocolBufferException) ex.getCause()).getUnfinishedMessage());
    }
  }

  @Test
  public void extensionRegistry_notNull() {
    thrown.expect(NullPointerException.class);
    thrown.expectMessage("newRegistry");

    ProtoLiteUtils.setExtensionRegistry(null);
  }

  @Test
  public void parseFromKnowLengthInputStream() throws Exception {
    Marshaller<Type> marshaller = ProtoLiteUtils.marshaller(Type.getDefaultInstance());
    Type expect = Type.newBuilder().setName("expected name").build();

    Type result = marshaller.parse(new CustomKnownLengthInputStream(expect.toByteArray()));
    assertEquals(expect, result);
  }

  @Test
  public void defaultMaxMessageSize() {
    assertEquals(GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE, ProtoLiteUtils.DEFAULT_MAX_MESSAGE_SIZE);
  }

  private static class CustomKnownLengthInputStream extends InputStream implements KnownLength {
    private int position = 0;
    private byte[] source;

    private CustomKnownLengthInputStream(byte[] source) {
      this.source = source;
    }

    @Override
    public int available() throws IOException {
      return source.length - position;
    }

    @Override
    public int read() throws IOException {
      if (position == source.length) {
        return -1;
      }

      return source[position++];
    }
  }
}
