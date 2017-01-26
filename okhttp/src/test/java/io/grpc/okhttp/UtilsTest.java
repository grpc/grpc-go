/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.okhttp;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import io.grpc.okhttp.internal.CipherSuite;
import io.grpc.okhttp.internal.ConnectionSpec;
import io.grpc.okhttp.internal.TlsVersion;
import java.util.List;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link Utils}.
 */
@RunWith(JUnit4.class)
public class UtilsTest {

  @Rule
  public final ExpectedException thrown = ExpectedException.none();

  @Test
  public void convertSpecRejectsPlaintext() {
    com.squareup.okhttp.ConnectionSpec plaintext = com.squareup.okhttp.ConnectionSpec.CLEARTEXT;
    thrown.expect(IllegalArgumentException.class);
    thrown.expectMessage("plaintext ConnectionSpec is not accepted");
    Utils.convertSpec(plaintext);
  }

  @Test
  public void convertSpecKeepsAllData() {
    com.squareup.okhttp.ConnectionSpec squareSpec = com.squareup.okhttp.ConnectionSpec.MODERN_TLS;
    ConnectionSpec spec = Utils.convertSpec(squareSpec);

    List<com.squareup.okhttp.TlsVersion> squareTlsVersions = squareSpec.tlsVersions();
    List<TlsVersion> tlsVersions = spec.tlsVersions();
    int versionsSize = squareTlsVersions.size();
    List<com.squareup.okhttp.CipherSuite> squareCipherSuites = squareSpec.cipherSuites();
    List<CipherSuite> cipherSuites = spec.cipherSuites();
    int cipherSuitesSize = squareCipherSuites.size();

    assertTrue(spec.isTls());
    assertTrue(spec.supportsTlsExtensions());
    assertEquals(versionsSize, tlsVersions.size());
    for (int i = 0; i < versionsSize; i++) {
      assertEquals(TlsVersion.forJavaName(squareTlsVersions.get(i).javaName()), tlsVersions.get(i));
    }
    assertEquals(cipherSuitesSize, cipherSuites.size());
    for (int i = 0; i < cipherSuitesSize; i++) {
      assertEquals(CipherSuite.forJavaName(squareCipherSuites.get(i).name()), cipherSuites.get(i));
    }
  }
}
