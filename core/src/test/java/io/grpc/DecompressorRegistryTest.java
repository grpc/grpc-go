package io.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.IOException;
import java.io.InputStream;
import java.util.HashSet;
import java.util.Set;

/**
 * Tests for {@link DecompressorRegistry}.
 */
@RunWith(JUnit4.class)
public class DecompressorRegistryTest {

  private final Dummy dummyDecompressor = new Dummy();
  private final DecompressorRegistry registry = new DecompressorRegistry();

  @Test
  public void lookupDecompressor_checkDefaultMessageEncodingsExist() {
    // Explicitly put the names in, rather than link against MessageEncoding
    assertNotNull("Expected identity to be registered",
        registry.internalLookupDecompressor("identity"));
    assertNotNull("Expected gzip to be registered",
        registry.internalLookupDecompressor("gzip"));
  }

  @Test
  public void getKnownMessageEncodings_checkDefaultMessageEncodingsExist() {
    Set<String> knownEncodings = new HashSet<String>();
    knownEncodings.add("identity");
    knownEncodings.add("gzip");

    assertEquals(knownEncodings, registry.internalGetKnownMessageEncodings());
  }

  /*
   * This test will likely change once encoders are advertised
   */
  @Test
  public void getAdvertisedMessageEncodings_noEncodingsAdvertised() {
    assertTrue(registry.internalGetAdvertisedMessageEncodings().isEmpty());
  }

  @Test
  public void registerDecompressor_advertisedDecompressor() {
    registry.internalRegister(dummyDecompressor, true);

    assertTrue(registry.internalGetAdvertisedMessageEncodings()
        .contains(dummyDecompressor.getMessageEncoding()));
  }

  @Test
  public void registerDecompressor_nonadvertisedDecompressor() {
    DecompressorRegistry.register(dummyDecompressor, false);

    assertFalse(registry.internalGetAdvertisedMessageEncodings()
        .contains(dummyDecompressor.getMessageEncoding()));
  }

  private static final class Dummy implements Decompressor {
    @Override
    public String getMessageEncoding() {
      return "dummy";
    }

    @Override
    public InputStream decompress(InputStream is) throws IOException {
      return is;
    }
  }
}

