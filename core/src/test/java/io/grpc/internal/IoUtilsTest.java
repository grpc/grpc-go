/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotSame;

import java.io.ByteArrayInputStream;
import java.io.InputStream;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit test for IoUtils. */
@RunWith(JUnit4.class)
public class IoUtilsTest {

  @Test
  public void testRoundTrip() throws Exception {
    byte[] bytes = { 1, 2, 3, -127, 100, 127};
    InputStream is = new ByteArrayInputStream(bytes);
    byte[] bytes2 = IoUtils.toByteArray(is);
    
    assertNotSame(bytes2, bytes);
    assertEquals(bytes.length, bytes2.length);
    for (int i = 0; i < bytes.length; ++i) {
      assertEquals(bytes[i], bytes2[i]);
    }
  }

  @Test
  public void testEmpty() throws Exception {
    InputStream is = new ByteArrayInputStream(new byte[0]);
    byte[] bytes = IoUtils.toByteArray(is);

    assertEquals(0, bytes.length);
  }
}