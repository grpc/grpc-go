/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

package io.grpc.thrift;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.grpc.Drainable;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.internal.IoUtils;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collections;
import org.apache.thrift.TException;
import org.apache.thrift.TSerializer;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ThriftUtils}. */
@RunWith(JUnit4.class)
public class ThriftUtilsTest {
  private Marshaller<Message> marshaller = ThriftUtils.marshaller(new MessageFactory<Message>() {
    @Override
    public Message newInstance() {
      return new Message();
    }
  });

  private Metadata.BinaryMarshaller<Message> metadataMarshaller = ThriftUtils.metadataMarshaller(
      new MessageFactory<Message>() {
        @Override
        public Message newInstance() {
          return new Message();
        }
      });

  @Test
  public void testRoundTrip() {
    Message m = new Message();
    m.i = 2;
    m.b = true;
    m.s = "string";
    Message m2 = marshaller.parse(marshaller.stream(m));
    assertNotSame(m, m2);
    assertTrue(m.equals( m2 ));
  }

  @Test
  public void parseInvalid() throws Exception {
    InputStream is = new ByteArrayInputStream(new byte[] {-127});
    try {
      marshaller.parse(is);
      fail("Expected exception");
    } catch (StatusRuntimeException ex) {
      assertEquals(Status.Code.INTERNAL, ex.getStatus().getCode());
      assertTrue(ex.getCause() instanceof TException);
    }
  }

  @Test
  public void testLarge() throws Exception {
    Message m = new Message();
    // list size 80 MB
    m.l = new ArrayList<Integer>(Collections.nCopies(20 * 1024 * 1024, 1000000007));
    Message m2 = marshaller.parse(marshaller.stream(m));
    assertNotSame(m, m2);
    assertEquals(m.l.size(), m2.l.size());
  }

  @Test
  public void metadataMarshaller_roundtrip() {
    Message m = new Message();
    m.i = 2;
    assertEquals(m, metadataMarshaller.parseBytes(metadataMarshaller.toBytes(m)));
  }

  @Test
  public void metadataMarshaller_invalid() {
    try {
      metadataMarshaller.parseBytes(new byte[] {-127});
      fail("Expected exception");
    } catch (Exception ex) {
      assertTrue(ex.getCause() instanceof TException);
    }
  }

  @Test
  public void testAvailable() throws Exception {
    Message m = new Message();
    m.i = 10;
    InputStream is = marshaller.stream(m);
    assertEquals(is.available(), new TSerializer().serialize(m).length);
    is.read();
    assertEquals(is.available(), new TSerializer().serialize(m).length - 1);
    while (is.read() != -1) {}
    assertEquals(-1, is.read());
    assertEquals(0, is.available());
  }

  @Test
  public void testDrainTo_all() throws Exception {
    Message m = new Message();
    byte[] bytes = IoUtils.toByteArray(marshaller.stream(m));
    InputStream is = marshaller.stream(m);
    Drainable d = (Drainable) is;
    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    int drained = d.drainTo(baos);
    assertEquals(baos.size(), drained);
    assertArrayEquals(bytes, baos.toByteArray());
    assertEquals(0, is.available());
  }

  @Test
  public void testDrainTo_partial() throws Exception {
    Message m = new Message();
    final byte[] bytes;
    {
      InputStream is = marshaller.stream(m);
      is.read();
      bytes = IoUtils.toByteArray(is);
    }
    InputStream is = marshaller.stream(m);
    is.read();
    Drainable d = (Drainable) is;
    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    int drained = d.drainTo(baos);
    assertEquals(baos.size(), drained);
    assertArrayEquals(bytes, baos.toByteArray());
    assertEquals(0, is.available());
  }

  @Test
  public void testDrainTo_none() throws Exception {
    Message m = new Message();
    byte[] bytes = IoUtils.toByteArray(marshaller.stream(m));
    InputStream is = marshaller.stream(m);
    byte[] unused = IoUtils.toByteArray(is);
    Drainable d = (Drainable) is;
    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    assertEquals(0, d.drainTo(baos));
    assertArrayEquals(new byte[0], baos.toByteArray());
    assertEquals(0, is.available());
  }
}