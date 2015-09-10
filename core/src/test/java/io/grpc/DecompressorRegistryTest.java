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

