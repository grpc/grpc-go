/*
 * Copyright 2016 The gRPC Authors
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

import static io.grpc.ConnectivityState.CONNECTING;
import static io.grpc.ConnectivityState.IDLE;
import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;
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
    ConnectivityStateInfo info = ConnectivityStateInfo.forNonError(IDLE);
    assertEquals(IDLE, info.getState());
    assertEquals(Status.OK, info.getStatus());
  }

  @Test(expected = IllegalArgumentException.class)
  public void forNonErrorInvalid() {
    ConnectivityStateInfo.forNonError(TRANSIENT_FAILURE);
  }

  @Test
  public void forTransientFailure() {
    ConnectivityStateInfo info = ConnectivityStateInfo.forTransientFailure(Status.UNAVAILABLE);
    assertEquals(TRANSIENT_FAILURE, info.getState());
    assertEquals(Status.UNAVAILABLE, info.getStatus());
  }

  @Test(expected = IllegalArgumentException.class)
  public void forTransientFailureInvalid() {
    ConnectivityStateInfo.forTransientFailure(Status.OK);
  }

  @Test
  public void equality() {
    ConnectivityStateInfo info1 = ConnectivityStateInfo.forNonError(IDLE);
    ConnectivityStateInfo info2 = ConnectivityStateInfo.forNonError(CONNECTING);
    ConnectivityStateInfo info3 = ConnectivityStateInfo.forNonError(IDLE);
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
