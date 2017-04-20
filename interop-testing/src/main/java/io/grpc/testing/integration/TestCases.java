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
 * Enum of interop test cases.
 */
public enum TestCases {
  EMPTY_UNARY("empty (zero bytes) request and response"),
  CACHEABLE_UNARY("cacheable unary rpc sent using GET"),
  LARGE_UNARY("single request and (large) response"),
  CLIENT_STREAMING("request streaming with single response"),
  SERVER_STREAMING("single request with response streaming"),
  PING_PONG("full-duplex ping-pong streaming"),
  EMPTY_STREAM("A stream that has zero-messages in both directions"),
  COMPUTE_ENGINE_CREDS("large_unary with service_account auth"),
  SERVICE_ACCOUNT_CREDS("large_unary with compute engine auth"),
  JWT_TOKEN_CREDS("JWT-based auth"),
  OAUTH2_AUTH_TOKEN("raw oauth2 access token auth"),
  PER_RPC_CREDS("per rpc raw oauth2 access token auth"),
  CUSTOM_METADATA("unary and full duplex calls with metadata"),
  STATUS_CODE_AND_MESSAGE("request error code and message"),
  UNIMPLEMENTED_METHOD("call an unimplemented RPC method"),
  UNIMPLEMENTED_SERVICE("call an unimplemented RPC service"),
  CANCEL_AFTER_BEGIN("cancel stream after starting it"),
  CANCEL_AFTER_FIRST_RESPONSE("cancel on first response"),
  TIMEOUT_ON_SLEEPING_SERVER("timeout before receiving a response");

  private final String description;

  TestCases(String description) {
    this.description = description;
  }

  /**
   * Returns a description of the test case.
   */
  public String description() {
    return description;
  }

  /**
   * Returns the {@link TestCases} matching the string {@code s}. The
   * matching is done case insensitive.
   */
  public static TestCases fromString(String s) {
    Preconditions.checkNotNull(s, "s");
    return TestCases.valueOf(s.toUpperCase());
  }
}
