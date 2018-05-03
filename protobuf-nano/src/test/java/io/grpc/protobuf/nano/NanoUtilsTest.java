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
