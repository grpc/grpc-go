/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

package io.grpc.protobuf;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.fail;

import com.google.common.io.ByteStreams;
import com.google.protobuf.Any;
import com.google.protobuf.ByteString;
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
  public void testJsonMarshallerFailToPrint() {
    Marshaller<Any> marshaller = ProtoUtils.jsonMarshaller(Any.getDefaultInstance());
    try {
      marshaller.stream(
          Any.newBuilder().setValue(ByteString.copyFromUtf8("invalid (no type url)")).build());
      fail("Expected exception");
    } catch (StatusRuntimeException e) {
      assertNotNull(e.getCause());
      assertNotNull(e.getMessage());
      assertEquals(Status.Code.INTERNAL, e.getStatus().getCode());
    }
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
