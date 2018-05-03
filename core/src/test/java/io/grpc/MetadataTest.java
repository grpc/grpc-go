/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc;

import static com.google.common.base.Charsets.US_ASCII;
import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.collect.Lists;
import io.grpc.Metadata.Key;
import io.grpc.internal.GrpcUtil;
import java.util.Arrays;
import java.util.Iterator;
import java.util.Locale;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link Metadata}.
 */
@RunWith(JUnit4.class)
public class MetadataTest {

  @Rule public final ExpectedException thrown = ExpectedException.none();

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
  public void noPseudoHeaders() {
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("Invalid character");

    Metadata.Key.of(":test-bin", FISH_MARSHALLER);
  }

  @Test
  public void testMutations() {
    Fish lance = new Fish(LANCE);
    Fish cat = new Fish("cat");
    Metadata metadata = new Metadata();

    assertEquals(null, metadata.get(KEY));
    metadata.put(KEY, lance);
    assertEquals(Arrays.asList(lance), Lists.newArrayList(metadata.getAll(KEY)));
    assertEquals(lance, metadata.get(KEY));
    metadata.put(KEY, lance);
    assertEquals(Arrays.asList(lance, lance), Lists.newArrayList(metadata.getAll(KEY)));
    assertTrue(metadata.remove(KEY, lance));
    assertEquals(Arrays.asList(lance), Lists.newArrayList(metadata.getAll(KEY)));

    assertFalse(metadata.remove(KEY, cat));
    metadata.put(KEY, cat);
    assertEquals(cat, metadata.get(KEY));
    metadata.put(KEY, lance);
    assertEquals(Arrays.asList(lance, cat, lance), Lists.newArrayList(metadata.getAll(KEY)));
    assertEquals(lance, metadata.get(KEY));
    assertTrue(metadata.remove(KEY, lance));
    assertEquals(Arrays.asList(cat, lance), Lists.newArrayList(metadata.getAll(KEY)));
    metadata.put(KEY, lance);
    assertTrue(metadata.remove(KEY, cat));
    assertEquals(Arrays.asList(lance, lance), Lists.newArrayList(metadata.getAll(KEY)));

    assertEquals(Arrays.asList(lance, lance), Lists.newArrayList(metadata.removeAll(KEY)));
    assertEquals(null, metadata.getAll(KEY));
    assertEquals(null, metadata.get(KEY));
  }

  @Test
  public void discardAll() {
    Fish lance = new Fish(LANCE);
    Metadata metadata = new Metadata();

    metadata.put(KEY, lance);
    metadata.discardAll(KEY);
    assertEquals(null, metadata.getAll(KEY));
    assertEquals(null, metadata.get(KEY));
  }

  @Test
  public void discardAll_empty() {
    Metadata metadata = new Metadata();
    metadata.discardAll(KEY);
    assertEquals(null, metadata.getAll(KEY));
    assertEquals(null, metadata.get(KEY));
  }

  @Test
  public void testGetAllNoRemove() {
    Fish lance = new Fish(LANCE);
    Metadata metadata = new Metadata();
    metadata.put(KEY, lance);
    Iterator<Fish> i = metadata.getAll(KEY).iterator();
    assertEquals(lance, i.next());

    thrown.expect(UnsupportedOperationException.class);
    i.remove();
  }

  @Test
  public void testWriteParsed() {
    Fish lance = new Fish(LANCE);
    Metadata metadata = new Metadata();
    metadata.put(KEY, lance);
    assertEquals(lance, metadata.get(KEY));
    Iterator<Fish> fishes = metadata.<Fish>getAll(KEY).iterator();
    assertTrue(fishes.hasNext());
    assertEquals(fishes.next(), lance);
    assertFalse(fishes.hasNext());
    byte[][] serialized = metadata.serialize();
    assertEquals(2, serialized.length);
    assertEquals("test-bin", new String(serialized[0], US_ASCII));
    assertArrayEquals(LANCE_BYTES, serialized[1]);
    assertEquals(lance, metadata.get(KEY));
    assertEquals(serialized[0], metadata.serialize()[0]);
    assertEquals(serialized[1], metadata.serialize()[1]);
  }

  @Test
  public void testWriteRaw() {
    Metadata raw = new Metadata(KEY.asciiName(), LANCE_BYTES);
    Fish lance = raw.get(KEY);
    assertEquals(lance, new Fish(LANCE));
    // Reading again should return the same parsed instance
    assertEquals(lance, raw.get(KEY));
  }

  @Test
  public void testSerializeRaw() {
    Metadata raw = new Metadata(KEY.asciiName(), LANCE_BYTES);
    byte[][] serialized = raw.serialize();
    assertArrayEquals(serialized[0], KEY.asciiName());
    assertArrayEquals(serialized[1], LANCE_BYTES);
  }

  @Test
  public void testMergeByteConstructed() {
    Metadata raw = new Metadata(KEY.asciiName(), LANCE_BYTES);
    Metadata serializable = new Metadata();
    serializable.merge(raw);

    byte[][] serialized = serializable.serialize();
    assertArrayEquals(serialized[0], KEY.asciiName());
    assertArrayEquals(serialized[1], LANCE_BYTES);
    assertEquals(new Fish(LANCE), serializable.get(KEY));
  }

  @Test
  public void headerMergeShouldCopyValues() {
    Fish lance = new Fish(LANCE);
    Metadata h1 = new Metadata();

    Metadata h2 = new Metadata();
    h2.put(KEY, lance);

    h1.merge(h2);

    Iterator<Fish> fishes = h1.<Fish>getAll(KEY).iterator();
    assertTrue(fishes.hasNext());
    assertEquals(fishes.next(), lance);
    assertFalse(fishes.hasNext());
  }

  @Test
  public void mergeExpands() {
    Fish lance = new Fish(LANCE);
    Metadata h1 = new Metadata();
    h1.put(KEY, lance);

    Metadata h2 = new Metadata();
    h2.put(KEY, lance);
    h2.put(KEY, lance);
    h2.put(KEY, lance);
    h2.put(KEY, lance);

    h1.merge(h2);
  }

  @Test
  public void shortBinaryKeyName() {
    thrown.expect(IllegalArgumentException.class);

    Metadata.Key.of("-bin", FISH_MARSHALLER);
  }

  @Test
  public void invalidSuffixBinaryKeyName() {
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("Binary header is named");

    Metadata.Key.of("nonbinary", FISH_MARSHALLER);
  }

  @Test
  public void verifyToString() {
    Metadata h = new Metadata();
    h.put(KEY, new Fish("binary"));
    h.put(Metadata.Key.of("test", Metadata.ASCII_STRING_MARSHALLER), "ascii");
    assertEquals("Metadata(test-bin=YmluYXJ5,test=ascii)", h.toString());

    Metadata t = new Metadata();
    t.put(Metadata.Key.of("test", Metadata.ASCII_STRING_MARSHALLER), "ascii");
    assertEquals("Metadata(test=ascii)", t.toString());

    t = new Metadata("test".getBytes(US_ASCII), "ascii".getBytes(US_ASCII),
        "test-bin".getBytes(US_ASCII), "binary".getBytes(US_ASCII));
    assertEquals("Metadata(test=ascii,test-bin=YmluYXJ5)", t.toString());
  }

  @Test
  public void verifyToString_usingBinary() {
    Metadata h = new Metadata();
    h.put(KEY, new Fish("binary"));
    h.put(Metadata.Key.of("test", Metadata.ASCII_STRING_MARSHALLER), "ascii");
    assertEquals("Metadata(test-bin=YmluYXJ5,test=ascii)", h.toString());

    Metadata t = new Metadata();
    t.put(Metadata.Key.of("test", Metadata.ASCII_STRING_MARSHALLER), "ascii");
    assertEquals("Metadata(test=ascii)", t.toString());
  }

  @Test
  public void testKeyCaseHandling() {
    Locale originalLocale = Locale.getDefault();
    Locale.setDefault(new Locale("tr", "TR"));
    try {
      // In Turkish, both I and i (which are in ASCII) change into non-ASCII characters when their
      // case is changed as ı and İ, respectively.
      assertEquals("İ", "i".toUpperCase());
      assertEquals("ı", "I".toLowerCase());

      Metadata.Key<String> keyTitleCase
          = Metadata.Key.of("If-Modified-Since", Metadata.ASCII_STRING_MARSHALLER);
      Metadata.Key<String> keyLowerCase
          = Metadata.Key.of("if-modified-since", Metadata.ASCII_STRING_MARSHALLER);
      Metadata.Key<String> keyUpperCase
          = Metadata.Key.of("IF-MODIFIED-SINCE", Metadata.ASCII_STRING_MARSHALLER);

      Metadata metadata = new Metadata();
      metadata.put(keyTitleCase, "plain string");
      assertEquals("plain string", metadata.get(keyTitleCase));
      assertEquals("plain string", metadata.get(keyLowerCase));
      assertEquals("plain string", metadata.get(keyUpperCase));

      byte[][] bytes = metadata.serialize();
      assertEquals(2, bytes.length);
      assertArrayEquals("if-modified-since".getBytes(US_ASCII), bytes[0]);
      assertArrayEquals("plain string".getBytes(US_ASCII), bytes[1]);
    } finally {
      Locale.setDefault(originalLocale);
    }
  }

  @Test
  public void removeIgnoresMissingValue() {
    Metadata m = new Metadata();
    // Any key will work.
    Key<String> key = GrpcUtil.USER_AGENT_KEY;

    boolean success = m.remove(key, "agent");
    assertFalse(success);
  }

  @Test
  public void removeAllIgnoresMissingValue() {
    Metadata m = new Metadata();
    // Any key will work.
    Key<String> key = GrpcUtil.USER_AGENT_KEY;

    Iterable<String> removed = m.removeAll(key);
    assertNull(removed);
  }

  @Test
  public void keyEqualsHashNameWorks() {
    Key<?> k1 = Key.of("case", Metadata.ASCII_STRING_MARSHALLER);

    Key<?> k2 = Key.of("CASE", Metadata.ASCII_STRING_MARSHALLER);
    assertEquals(k1, k1);
    assertNotEquals(k1, null);
    assertNotEquals(k1, new Object(){});
    assertEquals(k1, k2);

    assertEquals(k1.hashCode(), k2.hashCode());
    // Check that the casing is preserved.
    assertEquals("CASE", k2.originalName());
    assertEquals("case", k2.name());
  }

  @Test
  public void invalidKeyName() {
    try {
      Key.of("io.grpc/key1", Metadata.ASCII_STRING_MARSHALLER);
      fail("Should have thrown");
    } catch (IllegalArgumentException e) {
      assertEquals("Invalid character '/' in key name 'io.grpc/key1'", e.getMessage());
    }
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
