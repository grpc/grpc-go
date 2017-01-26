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

package io.grpc.protobuf.nano;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.protobuf.nano.InvalidProtocolBufferNanoException;
import com.google.protobuf.nano.MessageNano;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.protobuf.nano.Messages.Message;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link NanoUtils}. */
@RunWith(JUnit4.class)
public class NanoUtilsTest {
  private Marshaller<Message> marshaller = NanoUtils.marshaller(new MessageNanoFactory<Message>() {
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
    assertEquals(2, m2.i);
    assertEquals(true, m2.b);
    assertEquals("string", m2.s);
    assertTrue(MessageNano.messageNanoEquals(m, m2));
  }

  @Test
  public void parseInvalid() throws Exception {
    InputStream is = new ByteArrayInputStream(new byte[] {-127});
    try {
      marshaller.parse(is);
      fail("Expected exception");
    } catch (StatusRuntimeException ex) {
      assertEquals(Status.Code.INTERNAL, ex.getStatus().getCode());
      assertTrue(ex.getCause() instanceof InvalidProtocolBufferNanoException);
    }
  }

  @Test
  public void testLarge() {
    Message m = new Message();
    // The default limit is 64MB. Using a larger proto to verify that the limit is not enforced.
    m.bs = new byte[70 * 1024 * 1024];
    Message m2 = marshaller.parse(marshaller.stream(m));
    assertNotSame(m, m2);
    // TODO(carl-mastrangelo): assertArrayEquals is REALLY slow, and been fixed in junit4.12.
    // Eventually switch back to it once we are using 4.12 everywhere.
    // assertArrayEquals(m.bs, m2.bs);
    assertEquals(m.bs.length, m2.bs.length);
    for (int i = 0; i < m.bs.length; i++) {
      assertEquals(m.bs[i], m2.bs[i]);
    }
  }

  @Test
  public void testAvailable() throws Exception {
    Message m = new Message();
    m.s = "string";
    InputStream is = marshaller.stream(m);
    assertEquals(m.getSerializedSize(), is.available());
    is.read();
    assertEquals(m.getSerializedSize() - 1, is.available());
    while (is.read() != -1) {}
    assertEquals(-1, is.read());
    assertEquals(0, is.available());
  }

  @Test
  public void testEmpty() throws IOException {
    InputStream is = marshaller.stream(new Message());
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
}
