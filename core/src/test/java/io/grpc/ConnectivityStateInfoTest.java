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

package io.grpc;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotEquals;
import static org.junit.Assert.assertNotSame;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ConnectivityStateInfo}. */
@RunWith(JUnit4.class)
public class ConnectivityStateInfoTest {
  @Test
  public void forNonError() {
    ConnectivityStateInfo info = ConnectivityStateInfo.forNonError(ConnectivityState.IDLE);
    assertEquals(ConnectivityState.IDLE, info.getState());
    assertEquals(Status.OK, info.getStatus());
  }

  @Test(expected = IllegalArgumentException.class)
  public void forNonErrorInvalid() {
    ConnectivityStateInfo.forNonError(ConnectivityState.TRANSIENT_FAILURE);
  }

  @Test
  public void forTransientFailure() {
    ConnectivityStateInfo info = ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE);
    assertEquals(ConnectivityState.TRANSIENT_FAILURE, info.getState());
    assertEquals(Status.UNAVAILABLE, info.getStatus());
  }

  @Test(expected = IllegalArgumentException.class)
  public void forTransientFailureInvalid() {
    ConnectivityStateInfo.forTransientFailure(Status.OK);
  }

  @Test
  public void equality() {
    ConnectivityStateInfo info1 = ConnectivityStateInfo.forNonError(ConnectivityState.IDLE);
    ConnectivityStateInfo info2 = ConnectivityStateInfo.forNonError(ConnectivityState.CONNECTING);
    ConnectivityStateInfo info3 = ConnectivityStateInfo.forNonError(ConnectivityState.IDLE);
    ConnectivityStateInfo info4 = ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE);
    ConnectivityStateInfo info5 = ConnectivityStateInfo.forTransientFailure(Status.INTERNAL);
    ConnectivityStateInfo info6 = ConnectivityStateInfo.forTransientFailure(Status.INTERNAL);

    assertEquals(info1, info3);
    assertNotSame(info1, info3);
    assertEquals(info1.hashCode(), info3.hashCode());
    assertEquals(info5, info6);
    assertEquals(info5.hashCode(), info6.hashCode());
    assertNotSame(info5, info6);

    assertNotEquals(info1, info2);
    assertNotEquals(info1, info4);
    assertNotEquals(info4, info6);

    assertFalse(info1.equals(null));
    // Extra cast to avoid ErrorProne EqualsIncompatibleType failure
    assertFalse(((Object) info1).equals(this));
  }
}
