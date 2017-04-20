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

import static io.grpc.testing.integration.TestCases.fromString;
import static org.junit.Assert.assertEquals;

import java.util.HashSet;
import java.util.Set;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link TestCases}.
 */
@RunWith(JUnit4.class)
public class TestCasesTest {

  @Test(expected = IllegalArgumentException.class)
  public void unknownStringThrowsException() {
    fromString("does_not_exist_1234");
  }

  @Test
  public void testCaseNamesShouldMapToEnums() {
    // names of testcases as defined in the interop spec
    String[] testCases = {
      "empty_unary",
      "cacheable_unary",
      "large_unary",
      "client_streaming",
      "server_streaming",
      "ping_pong",
      "empty_stream",
      "compute_engine_creds",
      "service_account_creds",
      "jwt_token_creds",
      "oauth2_auth_token",
      "per_rpc_creds",
      "custom_metadata",
      "status_code_and_message",
      "unimplemented_method",
      "unimplemented_service",
      "cancel_after_begin",
      "cancel_after_first_response",
      "timeout_on_sleeping_server"
    };

    assertEquals(testCases.length, TestCases.values().length);

    Set<TestCases> testCaseSet = new HashSet<TestCases>(testCases.length);
    for (String testCase : testCases) {
      testCaseSet.add(TestCases.fromString(testCase));
    }

    assertEquals(TestCases.values().length, testCaseSet.size());
  }
}
