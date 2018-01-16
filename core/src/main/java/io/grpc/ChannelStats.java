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

import javax.annotation.concurrent.Immutable;

/**
 * A data class to represent a channel's stats.
 */
// Not final so that InternalChannelStats can make this class visible outside of io.grpc
@Immutable
class ChannelStats {
  public final String target;
  public final ConnectivityState state;
  public final long callsStarted;
  public final long callsSucceeded;
  public final long callsFailed;
  public final long lastCallStartedMillis;

  ChannelStats(
      String target,
      ConnectivityState state,
      long callsStarted,
      long callsSucceeded,
      long callsFailed,
      long lastCallStartedMillis) {
    this.target = target;
    this.state = state;
    this.callsStarted = callsStarted;
    this.callsSucceeded = callsSucceeded;
    this.callsFailed = callsFailed;
    this.lastCallStartedMillis = lastCallStartedMillis;
  }
}
