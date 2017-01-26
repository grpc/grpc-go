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

package io.grpc.protobuf;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.fail;

import com.google.common.io.ByteStreams;
import com.google.protobuf.Type;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import org.junit.Ignore;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ProtoUtils}. */
@RunWith(JUnit4.class)
public class ProtoUtilsTest {
  private Type proto = Type.newBuilder().setName("value").build();

  @Test
  public void testRoundtrip() throws Exception {
    Marshaller<Type> marshaller = ProtoUtils.marshaller(Type.getDefaultInstance());
    InputStream is = marshaller.stream(proto);
    is = new ByteArrayInputStream(ByteStreams.toByteArray(is));
    assertEquals(proto, marshaller.parse(is));
  }

  @Test
  public void keyForProto() {
    assertEquals("google.protobuf.Type-bin",
        ProtoUtils.keyForProto(Type.getDefaultInstance()).originalName());
  }

  @Test
  public void testJsonRoundtrip() throws Exception {
    Marshaller<Type> marshaller = ProtoUtils.jsonMarshaller(Type.getDefaultInstance());
    InputStream is = marshaller.stream(proto);
    is = new ByteArrayInputStream(ByteStreams.toByteArray(is));
    assertEquals(proto, marshaller.parse(is));
  }

  @Test
  public void testJsonRepresentation() throws Exception {
    Marshaller<Type> marshaller = ProtoUtils.jsonMarshaller(Type.getDefaultInstance());
    InputStream is = marshaller.stream(proto);
    String s = new String(ByteStreams.toByteArray(is), "UTF-8");
    assertEquals("{\"name\":\"value\"}", s.replaceAll("\\s", ""));
  }

  @Ignore("https://github.com/google/protobuf/issues/1470")
  @Test
  public void testJsonInvalid() throws Exception {
    Marshaller<Type> marshaller = ProtoUtils.jsonMarshaller(Type.getDefaultInstance());
    try {
      marshaller.parse(new ByteArrayInputStream("{]".getBytes("UTF-8")));
      fail("Expected exception");
    } catch (StatusRuntimeException ex) {
      assertEquals(Status.Code.INTERNAL, ex.getStatus().getCode());
      assertNotNull(ex.getCause());
    }
  }

  @Test
  public void testJsonInvalidProto() throws Exception {
    Marshaller<Type> marshaller = ProtoUtils.jsonMarshaller(Type.getDefaultInstance());
    try {
      marshaller.parse(new ByteArrayInputStream("{\"\":3}".getBytes("UTF-8")));
      fail("Expected exception");
    } catch (StatusRuntimeException ex) {
      assertEquals(Status.Code.INTERNAL, ex.getStatus().getCode());
      assertNotNull(ex.getCause());
    }
  }

  @Test
  public void testJsonIoException() throws Exception {
    Marshaller<Type> marshaller = ProtoUtils.jsonMarshaller(Type.getDefaultInstance());
    final IOException ioe = new IOException();
    try {
      marshaller.parse(new ByteArrayInputStream("{}".getBytes("UTF-8")) {
        @Override
        public void close() throws IOException {
          throw ioe;
        }
      });
      fail("Exception expected");
    } catch (StatusRuntimeException ex) {
      assertEquals(Status.Code.INTERNAL, ex.getStatus().getCode());
      assertEquals(ioe, ex.getCause());
    }
  }
}
