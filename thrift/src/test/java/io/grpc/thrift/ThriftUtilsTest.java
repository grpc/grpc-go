/*
 * Copyright 2016, Google Inc. All rights reserved.
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