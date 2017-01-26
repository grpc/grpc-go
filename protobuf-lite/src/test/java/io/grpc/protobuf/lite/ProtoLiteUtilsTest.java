/*
 * Copyright 2015, Google Inc. All rights reserved.
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
import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.PrototypeMarshaller;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
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
    byte[] golden = ByteStreams.toByteArray(marshaller.stream(proto));
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
}
