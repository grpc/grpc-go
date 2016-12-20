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

package io.grpc.testing.integration;

import com.google.common.base.Preconditions;

/**
 * Enum of HTTP/2 interop test cases.
 */
public enum Http2TestCases {
  RST_AFTER_HEADER("server resets stream after sending header"),
  RST_AFTER_DATA("server resets stream after sending data"),
  RST_DURING_DATA("server resets stream in the middle of sending data"),
  GOAWAY("server sends goaway after first request and asserts second request uses new connection"),
  PING("server sends pings during request and verifies client response"),
  MAX_STREAMS("server verifies that the client respects MAX_STREAMS setting");

  private final String description;

  Http2TestCases(String description) {
    this.description = description;
  }

  /**
   * Returns a description of the test case.
   */
  public String description() {
    return description;
  }

  /**
   * Returns the {@link Http2TestCases} matching the string {@code s}. The
   * matching is case insensitive.
   */
  public static Http2TestCases fromString(String s) {
    Preconditions.checkNotNull(s, "s");
    try {
      return Http2TestCases.valueOf(s.toUpperCase());
    } catch (IllegalArgumentException ex) {
      throw new IllegalArgumentException("Invalid test case: " + s);
    }
  }
}
