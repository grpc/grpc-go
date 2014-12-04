package com.google.net.stubby;

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.nio.charset.StandardCharsets;
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
  private static final byte[] LANCE_BYTES = LANCE.getBytes(StandardCharsets.US_ASCII);
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
    assertEquals(new String(serialized[0], StandardCharsets.US_ASCII), "test-bin");
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
  }
}
