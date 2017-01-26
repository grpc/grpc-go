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
    Preconditions.checkArgument(state != ConnectivityState.TRANSIENT_FAILURE,
        "state is TRANSIENT_ERROR. Use forError() instead");
    return new ConnectivityStateInfo(state, Status.OK);
  }

  /**
   * Returns an instance for {@code TRANSIENT_FAILURE}, associated with an error status.
   */
  public static ConnectivityStateInfo forTransientFailure(Status error) {
    Preconditions.checkArgument(!error.isOk(), "The error status must not be OK");
    return new ConnectivityStateInfo(ConnectivityState.TRANSIENT_FAILURE, error);
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
