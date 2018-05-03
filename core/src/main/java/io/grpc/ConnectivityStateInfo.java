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

import static io.grpc.ConnectivityState.TRANSIENT_FAILURE;

import com.google.common.base.Preconditions;

/**
 * A tuple of a {@link ConnectivityState} and its associated {@link Status}.
 *
 * <p>If the state is {@code TRANSIENT_FAILURE}, the status is never {@code OK}.  For other states,
 * the status is always {@code OK}.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
public final class ConnectivityStateInfo {
  private final ConnectivityState state;
  private final Status status;

  /**
   * Returns an instance for a state that is not {@code TRANSIENT_FAILURE}.
   *
   * @throws IllegalArgumentException if {@code state} is {@code TRANSIENT_FAILURE}.
   */
  public static ConnectivityStateInfo forNonError(ConnectivityState state) {
    Preconditions.checkArgument(
        state != TRANSIENT_FAILURE,
        "state is TRANSIENT_ERROR. Use forError() instead");
    return new ConnectivityStateInfo(state, Status.OK);
  }

  /**
   * Returns an instance for {@code TRANSIENT_FAILURE}, associated with an error status.
   */
  public static ConnectivityStateInfo forTransientFailure(Status error) {
    Preconditions.checkArgument(!error.isOk(), "The error status must not be OK");
    return new ConnectivityStateInfo(TRANSIENT_FAILURE, error);
  }

  /**
   * Returns the state.
   */
  public ConnectivityState getState() {
    return state;
  }

  /**
   * Returns the status associated with the state.
   *
   * <p>If the state is {@code TRANSIENT_FAILURE}, the status is never {@code OK}.  For other
   * states, the status is always {@code OK}.
   */
  public Status getStatus() {
    return status;
  }

  @Override
  public boolean equals(Object other) {
    if (!(other instanceof ConnectivityStateInfo)) {
      return false;
    }
    ConnectivityStateInfo o = (ConnectivityStateInfo) other;
    return state.equals(o.state) && status.equals(o.status);
  }

  @Override
  public int hashCode() {
    return state.hashCode() ^ status.hashCode();
  }

  @Override
  public String toString() {
    if (status.isOk()) {
      return state.toString();
    }
    return state + "(" + status + ")";
  }

  private ConnectivityStateInfo(ConnectivityState state, Status status) {
    this.state = Preconditions.checkNotNull(state, "state is null");
    this.status = Preconditions.checkNotNull(status, "status is null");
  }
}
