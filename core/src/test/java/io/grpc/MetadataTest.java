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

import static com.google.common.base.Charsets.US_ASCII;
import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.Iterator;

/**
 * Tests for {@link Metadata}
 */
@RunWith(JUnit4.class)
public class MetadataTest {

  private static final Metadata.BinaryMarshaller<Fish> FISH_MARSHALLER =
      new Metadata.BinaryMarshaller<Fish>() {
    @Override
    public byte[] toBytes(Fish fish) {
      return fish.name.getBytes(UTF_8);
    }

    @Override
    public Fish parseBytes(byte[] serialized) {
      return new Fish(new String(serialized, UTF_8));
    }
  };

  private static final String LANCE = "lance";
  private static final byte[] LANCE_BYTES = LANCE.getBytes(US_ASCII);
  private static final Metadata.Key<Fish> KEY = Metadata.Key.of("test-bin", FISH_MARSHALLER);

  @Test
  public void testWriteParsed() {
    Fish lance = new Fish(LANCE);
    Metadata.Headers metadata = new Metadata.Headers();
    metadata.put(KEY, lance);
    // Should be able to read same instance out
    assertSame(lance, metadata.get(KEY));
    Iterator<Fish> fishes = metadata.<Fish>getAll(KEY).iterator();
    assertTrue(fishes.hasNext());
    assertSame(fishes.next(), lance);
    assertFalse(fishes.hasNext());
    byte[][] serialized = metadata.serialize();
    assertEquals(2, serialized.length);
    assertEquals(new String(serialized[0], US_ASCII), "test-bin");
    assertArrayEquals(LANCE_BYTES, serialized[1]);
    assertSame(lance, metadata.get(KEY));
    // Serialized instance should be cached too
    assertSame(serialized[0], metadata.serialize()[0]);
    assertSame(serialized[1], metadata.serialize()[1]);
  }

  @Test
  public void testWriteRaw() {
    Metadata.Headers raw = new Metadata.Headers(KEY.asciiName(), LANCE_BYTES);
    Fish lance = raw.get(KEY);
    assertEquals(lance, new Fish(LANCE));
    // Reading again should return the same parsed instance
    assertSame(lance, raw.get(KEY));
  }

  @Test(expected = IllegalStateException.class)
  public void testFailSerializeRaw() {
    Metadata.Headers raw = new Metadata.Headers(KEY.asciiName(), LANCE_BYTES);
    raw.serialize();
  }

  @Test(expected = IllegalArgumentException.class)
  public void testFailMergeRawIntoSerializable() {
    Metadata.Headers raw = new Metadata.Headers(KEY.asciiName(), LANCE_BYTES);
    Metadata.Headers serializable = new Metadata.Headers();
    serializable.merge(raw);
  }

  @Test
  public void headerMergeShouldCopyValues() {
    Fish lance = new Fish(LANCE);
    Metadata.Headers h1 = new Metadata.Headers();

    Metadata.Headers h2 = new Metadata.Headers();
    h2.setPath("/some/path");
    h2.setAuthority("authority");
    h2.put(KEY, lance);

    h1.merge(h2);

    Iterator<Fish> fishes = h1.<Fish>getAll(KEY).iterator();
    assertTrue(fishes.hasNext());
    assertSame(fishes.next(), lance);
    assertFalse(fishes.hasNext());
    assertEquals("/some/path", h1.getPath());
    assertEquals("authority", h1.getAuthority());
  }

  @Test
  public void integerMarshallerIsDecimal() {
    assertEquals("12345678", Metadata.INTEGER_MARSHALLER.toAsciiString(12345678));
  }

  @Test
  public void roundTripIntegerMarshaller() {
    roundTripInteger(0);
    roundTripInteger(1);
    roundTripInteger(-1);
    roundTripInteger(0x12345678);
    roundTripInteger(0x87654321);
  }

  private void roundTripInteger(Integer i) {
    assertEquals(i, Metadata.INTEGER_MARSHALLER.parseAsciiString(
        Metadata.INTEGER_MARSHALLER.toAsciiString(i)));
  }

  @Test
  public void verifyToString() {
    Metadata.Headers h = new Metadata.Headers();
    h.setPath("/path");
    h.setAuthority("myauthority");
    h.put(KEY, new Fish("binary"));
    h.put(Metadata.Key.of("test", Metadata.ASCII_STRING_MARSHALLER), "ascii");
    assertEquals("Headers(path=/path,authority=myauthority,"
        + "metadata={test-bin=[Fish(binary)], test=[ascii]})", h.toString());

    Metadata.Trailers t = new Metadata.Trailers();
    t.put(Metadata.Key.of("test", Metadata.ASCII_STRING_MARSHALLER), "ascii");
    assertEquals("Trailers({test=[ascii]})", t.toString());

    t = new Metadata.Trailers("test".getBytes(US_ASCII), "ascii".getBytes(US_ASCII),
        "test-bin".getBytes(US_ASCII), "binary".getBytes(US_ASCII));
    assertEquals("Trailers({test=[ascii], test-bin=[[98, 105, 110, 97, 114, 121]]})", t.toString());
  }

  private static class Fish {
    private String name;

    private Fish(String name) {
      this.name = name;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (o == null || getClass() != o.getClass()) {
        return false;
      }
      Fish fish = (Fish) o;
      if (name != null ? !name.equals(fish.name) : fish.name != null) {
        return false;
      }
      return true;
    }

    @Override
    public int hashCode() {
      return name.hashCode();
    }

    @Override
    public String toString() {
      return "Fish(" + name + ")";
    }
  }
}
