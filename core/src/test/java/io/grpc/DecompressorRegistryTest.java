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

package io.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

import java.io.IOException;
import java.io.InputStream;
import java.util.HashSet;
import java.util.Set;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link DecompressorRegistry}.
 */
@RunWith(JUnit4.class)
public class DecompressorRegistryTest {

  private final Dummy dummyDecompressor = new Dummy();
  private DecompressorRegistry registry = DecompressorRegistry.emptyInstance();

  @Test
  public void lookupDecompressor_checkDefaultMessageEncodingsExist() {
    // Explicitly put the names in, rather than link against MessageEncoding
    assertNotNull("Expected identity to be registered",
        DecompressorRegistry.getDefaultInstance().lookupDecompressor("identity"));
    assertNotNull("Expected gzip to be registered",
        DecompressorRegistry.getDefaultInstance().lookupDecompressor("gzip"));
  }

  @Test
  public void getKnownMessageEncodings_checkDefaultMessageEncodingsExist() {
    Set<String> knownEncodings = new HashSet<>();
    knownEncodings.add("identity");
    knownEncodings.add("gzip");

    assertEquals(knownEncodings,
        DecompressorRegistry.getDefaultInstance().getKnownMessageEncodings());
  }

  /*
   * This test will likely change once encoders are advertised
   */
  @Test
  public void getAdvertisedMessageEncodings_noEncodingsAdvertised() {
    assertTrue(registry.getAdvertisedMessageEncodings().isEmpty());
  }

  @Test
  public void registerDecompressor_advertisedDecompressor() {
    registry = registry.with(dummyDecompressor, true);

    assertTrue(registry.getAdvertisedMessageEncodings()
        .contains(dummyDecompressor.getMessageEncoding()));
  }

  @Test
  public void registerDecompressor_nonadvertisedDecompressor() {
    registry = registry.with(dummyDecompressor, false);

    assertFalse(registry.getAdvertisedMessageEncodings()
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

