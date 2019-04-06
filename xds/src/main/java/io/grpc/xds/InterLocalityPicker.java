/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.xds;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.collect.ImmutableList;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import java.util.List;
import java.util.concurrent.ThreadLocalRandom;

final class InterLocalityPicker extends SubchannelPicker {

  private final List<WeightedChildPicker> weightedChildPickers;
  private final ThreadSafeRandom random;
  private final int totalWeight;

  static final class WeightedChildPicker {
    final int weight;
    final SubchannelPicker childPicker;

    WeightedChildPicker(int weight, SubchannelPicker childPicker) {
      checkArgument(weight >= 0, "weight is negative");
      checkNotNull(childPicker, "childPicker is null");

      this.weight = weight;
      this.childPicker = childPicker;
    }

    int getWeight() {
      return weight;
    }

    SubchannelPicker getPicker() {
      return childPicker;
    }
  }

  interface ThreadSafeRandom {
    int nextInt(int bound);
  }

  private static final class ThreadSafeRadomImpl implements ThreadSafeRandom {
    static final ThreadSafeRandom instance = new ThreadSafeRadomImpl();

    @Override
    public int nextInt(int bound) {
      return ThreadLocalRandom.current().nextInt(bound);
    }
  }

  InterLocalityPicker(List<WeightedChildPicker> weightedChildPickers) {
    this(weightedChildPickers, ThreadSafeRadomImpl.instance);
  }

  @VisibleForTesting
  InterLocalityPicker(List<WeightedChildPicker> weightedChildPickers, ThreadSafeRandom random) {
    checkNotNull(weightedChildPickers, "weightedChildPickers in null");
    checkArgument(!weightedChildPickers.isEmpty(), "weightedChildPickers is empty");

    this.weightedChildPickers = ImmutableList.copyOf(weightedChildPickers);

    int totalWeight = 0;
    for (WeightedChildPicker weightedChildPicker : weightedChildPickers) {
      int weight = weightedChildPicker.getWeight();
      totalWeight += weight;
    }
    this.totalWeight = totalWeight;

    this.random = random;
  }

  @Override
  public final PickResult pickSubchannel(PickSubchannelArgs args) {
    SubchannelPicker childPicker = null;

    if (totalWeight == 0) {
      childPicker =
          weightedChildPickers.get(random.nextInt(weightedChildPickers.size())).getPicker();
    } else {
      int rand = random.nextInt(totalWeight);

      // Find the first idx such that rand < accumulatedWeights[idx]
      // Not using Arrays.binarySearch for better readability.
      int accumulatedWeight = 0;
      for (int idx = 0; idx < weightedChildPickers.size(); idx++) {
        accumulatedWeight += weightedChildPickers.get(idx).getWeight();
        if (rand < accumulatedWeight) {
          childPicker = weightedChildPickers.get(idx).getPicker();
          break;
        }
      }
      checkNotNull(childPicker, "childPicker not found");
    }

    return childPicker.pickSubchannel(args);
  }
}
