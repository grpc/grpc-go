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

package io.grpc.internal;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.InternalChannelz.ChannelStats;
import io.grpc.InternalInstrumented;
import io.grpc.LoadBalancer;
import javax.annotation.Nullable;

/**
 * The base interface of the Subchannels returned by {@link
 * io.grpc.LoadBalancer.Helper#createSubchannel}.
 */
abstract class AbstractSubchannel extends LoadBalancer.Subchannel {

  /**
   * Same as {@link InternalSubchannel#obtainActiveTransport}.
   */
  @Nullable
  abstract ClientTransport obtainActiveTransport();

  /**
   * Returns the InternalSubchannel as an {@code Instrumented<T>} for the sole purpose of channelz
   * unit tests.
   */
  @VisibleForTesting
  abstract InternalInstrumented<ChannelStats> getInternalSubchannel();
}
