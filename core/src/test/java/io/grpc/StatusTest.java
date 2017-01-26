/*
 * Copyright 2014, Google Inc. All rights reserved.
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
import static org.junit.Assert.assertSame;

import io.grpc.Status.Code;
import java.nio.charset.Charset;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link Status}. */
@RunWith(JUnit4.class)
public class StatusTest {
  private final Charset ascii = Charset.forName("US-ASCII");

  @Test
  public void verifyExceptionMessage() {
    assertEquals("UNKNOWN", Status.UNKNOWN.asRuntimeException().getMessage());
    assertEquals("CANCELLED: This is a test",
        Status.CANCELLED.withDescription("This is a test").asRuntimeException().getMessage());
    assertEquals("UNKNOWN", Status.UNKNOWN.asException().getMessage());
    assertEquals("CANCELLED: This is a test",
        Status.CANCELLED.withDescription("This is a test").asException().getMessage());
  }

  @Test
  public void impossibleCodeValue() {
    assertEquals(Code.UNKNOWN, Status.fromCodeValue(-1).getCode());
  }

  @Test
  public void sameCauseReturnsSelf() {
    assertSame(Status.CANCELLED, Status.CANCELLED.withCause(null));
  }

  @Test
  public void sameDescriptionReturnsSelf() {
    assertSame(Status.CANCELLED, Status.CANCELLED.withDescription(null));
    assertSame(Status.CANCELLED, Status.CANCELLED.augmentDescription(null));
  }

  @Test
  public void useObjectHashCode() {
    assertEquals(Status.CANCELLED.hashCode(), System.identityHashCode(Status.CANCELLED));
  }

  @Test
  public void metadataEncode_lowAscii() {
    byte[] b = Status.MESSAGE_KEY.toBytes("my favorite character is \u0000");
    assertEquals("my favorite character is %00", new String(b, ascii));
  }

  @Test
  public void metadataEncode_percent() {
    byte[] b = Status.MESSAGE_KEY.toBytes("my favorite character is %");
    assertEquals("my favorite character is %25", new String(b, ascii));
  }

  @Test
  public void metadataEncode_surrogatePair() {
    byte[] b = Status.MESSAGE_KEY.toBytes("my favorite character is 𐀁");
    assertEquals("my favorite character is %F0%90%80%81", new String(b, ascii));
  }

  @Test
  public void metadataEncode_unmatchedHighSurrogate() {
    byte[] b = Status.MESSAGE_KEY.toBytes("my favorite character is " + ((char) 0xD801));
    assertEquals("my favorite character is ?", new String(b, ascii));
  }

  @Test
  public void metadataEncode_unmatchedLowSurrogate() {
    byte[] b = Status.MESSAGE_KEY.toBytes("my favorite character is " + ((char)0xDC37));
    assertEquals("my favorite character is ?", new String(b, ascii));
  }

  @Test
  public void metadataEncode_maxSurrogatePair() {
    byte[] b = Status.MESSAGE_KEY.toBytes(
        "my favorite character is " + ((char)0xDBFF) + ((char)0xDFFF));
    assertEquals("my favorite character is %F4%8F%BF%BF", new String(b, ascii));
  }

  @Test
  public void metadataDecode_ascii() {
    String s = Status.MESSAGE_KEY.parseBytes(new byte[]{'H', 'e', 'l', 'l', 'o'});
    assertEquals("Hello", s);
  }

  @Test
  public void metadataDecode_percent() {
    String s = Status.MESSAGE_KEY.parseBytes(new byte[]{'H', '%', '6', '1', 'o'});
    assertEquals("Hao", s);
  }

  @Test
  public void metadataDecode_percentUnderflow() {
    String s = Status.MESSAGE_KEY.parseBytes(new byte[]{'H', '%', '6'});
    assertEquals("H%6", s);
  }

  @Test
  public void metadataDecode_surrogate() {
    String s = Status.MESSAGE_KEY.parseBytes(
        new byte[]{'%', 'F', '0', '%', '9', '0', '%', '8', '0', '%', '8', '1'});
    assertEquals("𐀁", s);
  }

  @Test
  public void metadataDecode_badEncoding() {
    String s = Status.MESSAGE_KEY.parseBytes(new byte[]{'%', 'G', '0'});
    assertEquals("%G0", s);
  }
}
