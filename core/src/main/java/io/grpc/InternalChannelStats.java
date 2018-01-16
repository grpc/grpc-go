/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

/**
 * An internal gRPC class. Do not use.
 */
@Internal
public final class InternalChannelStats extends ChannelStats {
  public InternalChannelStats(
      String name,
      ConnectivityState state,
      long callsStarted,
      long callsSucceeded,
      long callsFailed,
      long lastCallStartedMillis) {
    super(name, state, callsStarted, callsSucceeded, callsFailed, lastCallStartedMillis);
  }

  public static final class Builder {
    private String target;
    private ConnectivityState state;
    private long callsStarted;
    private long callsSucceeded;
    private long callsFailed;
    private long lastCallStartedMillis;

    public Builder setTarget(String target) {
      this.target = target;
      return this;
    }

    public Builder setState(ConnectivityState state) {
      this.state = state;
      return this;
    }

    public Builder setCallsStarted(long callsStarted) {
      this.callsStarted = callsStarted;
      return this;
    }

    public Builder setCallsSucceeded(long callsSucceeded) {
      this.callsSucceeded = callsSucceeded;
      return this;
    }

    public Builder setCallsFailed(long callsFailed) {
      this.callsFailed = callsFailed;
      return this;
    }

    public Builder setLastCallStartedMillis(long lastCallStartedMillis) {
      this.lastCallStartedMillis = lastCallStartedMillis;
      return this;
    }

    public InternalChannelStats build() {
      return new InternalChannelStats(
          target, state, callsStarted,  callsSucceeded,  callsFailed,  lastCallStartedMillis);
    }
  }
}
