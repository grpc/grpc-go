/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.grpclb;

import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancer.Subchannel;
import io.grpc.LoadBalancer.SubchannelStateListener;
import javax.annotation.concurrent.NotThreadSafe;

/**
 * Manages life-cycle of Subchannels for {@link GrpclbState}.
 *
 * <p>All methods are run from the ChannelExecutor that the helper uses.
 */
@NotThreadSafe
interface SubchannelPool {
  /**
   * Pass essential utilities and the balancer that's using this pool.
   */
  void init(Helper helper);

  /**
   * Takes a {@link Subchannel} from the pool for the given {@code eag} if there is one available.
   * Otherwise, creates and returns a new {@code Subchannel} with the given {@code eag} and {@code
   * defaultAttributes}.
   *
   * <p>There can be at most one Subchannel for each EAG.  After a Subchannel is taken out of the
   * pool, it must be returned before the same EAG can be used to call this method.
   *
   * @param defaultAttributes the attributes used to create the Subchannel.  Not used if a pooled
   *        subchannel is returned.
   * @param stateListener receives state updates from now on
   */
  Subchannel takeOrCreateSubchannel(
      EquivalentAddressGroup eag, Attributes defaultAttributes,
      SubchannelStateListener stateListener);

  /**
   * Puts a {@link Subchannel} back to the pool.  From this point the Subchannel is owned by the
   * pool, and the caller should stop referencing to this Subchannel.  The {@link
   * SubchannelStateListener} will not receive any more updates.
   *
   * <p>Can only be called with a Subchannel created by this pool.  Must not be called if the
   * Subchannel is already in the pool.
   */
  void returnSubchannel(Subchannel subchannel);

  /**
   * Shuts down all subchannels in the pool immediately.
   */
  void clear();
}
