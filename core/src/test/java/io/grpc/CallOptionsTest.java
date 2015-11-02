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

package io.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import com.google.common.base.Objects;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.OutputStream;
import java.util.concurrent.TimeUnit;

/** Unit tests for {@link CallOptions}. */
@RunWith(JUnit4.class)
public class CallOptionsTest {
  private String sampleAuthority = "authority";
  private Long sampleDeadlineNanoTime = 1L;
  private Compressor sampleCompressor = new Codec.Gzip();
  private RequestKey sampleRequestKey = new RequestKey();
  private CallOptions allSet = CallOptions.DEFAULT
      .withAuthority(sampleAuthority)
      .withDeadlineNanoTime(sampleDeadlineNanoTime)
      .withCompressor(sampleCompressor)
      .withRequestKey(sampleRequestKey);

  @Test
  public void defaultsAreAllNull() {
    assertNull(CallOptions.DEFAULT.getDeadlineNanoTime());
    assertNull(CallOptions.DEFAULT.getCompressor());
    assertNull(CallOptions.DEFAULT.getAuthority());
    assertNull(CallOptions.DEFAULT.getRequestKey());
  }

  @Test
  public void allWiths() {
    assertSame(sampleAuthority, allSet.getAuthority());
    assertSame(sampleDeadlineNanoTime, allSet.getDeadlineNanoTime());
    assertSame(sampleCompressor, allSet.getCompressor());
    assertSame(sampleRequestKey, allSet.getRequestKey());
  }

  @Test
  public void noStrayModifications() {
    assertTrue(equal(allSet,
          allSet.withAuthority("blah").withAuthority(sampleAuthority)));
    assertTrue(equal(allSet,
          allSet.withDeadlineNanoTime(314L).withDeadlineNanoTime(sampleDeadlineNanoTime)));
    assertTrue(equal(allSet,
          allSet.withCompressor(Codec.Identity.NONE).withCompressor(sampleCompressor)));
    assertTrue(equal(allSet,
          allSet.withRequestKey(new RequestKey()).withRequestKey(sampleRequestKey)));
  }

  @Test
  public void mutation() {
    CallOptions options1 = CallOptions.DEFAULT.withDeadlineNanoTime(10L);
    assertNull(CallOptions.DEFAULT.getDeadlineNanoTime());
    assertEquals(10L, (long) options1.getDeadlineNanoTime());
    CallOptions options2 = options1.withDeadlineNanoTime(null);
    assertEquals(10L, (long) options1.getDeadlineNanoTime());
    assertNull(options2.getDeadlineNanoTime());
  }

  @Test
  public void testWithDeadlineAfter() {
    long deadline = CallOptions.DEFAULT
        .withDeadlineAfter(1, TimeUnit.MINUTES).getDeadlineNanoTime();
    long expected = System.nanoTime() + 1L * 60 * 1000 * 1000 * 1000;
    // 10 milliseconds of leeway
    long epsilon = 1000 * 1000 * 10;

    assertEquals(expected, deadline, epsilon);
  }

  @Test
  public void testToString() {
    Compressor gzip = new Compressor() {
      @Override
      public String toString() {
        return "GziP";
      }

      @Override
      public String getMessageEncoding() {
        throw new UnsupportedOperationException();
      }

      @Override
      public OutputStream compress(OutputStream os) {
        throw new UnsupportedOperationException();
      }
    };
    assertEquals("CallOptions{deadlineNanoTime=null, compressor=null, authority=null}",
        CallOptions.DEFAULT.toString());
    // Deadline makes it hard to check string for equality.
    assertEquals("CallOptions{deadlineNanoTime=null, compressor=GziP, authority=authority}",
        allSet.withCompressor(gzip).withDeadlineNanoTime(null).toString());
    assertTrue(allSet.toString().contains("deadlineNanoTime=" + sampleDeadlineNanoTime + ","));
  }

  private static boolean equal(CallOptions o1, CallOptions o2) {
    return Objects.equal(o1.getDeadlineNanoTime(), o2.getDeadlineNanoTime())
        && Objects.equal(o1.getCompressor(), o2.getCompressor())
        && Objects.equal(o1.getAuthority(), o2.getAuthority())
        && Objects.equal(o1.getRequestKey(), o2.getRequestKey());
  }
}
