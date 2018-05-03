/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.internal.testing;

import static com.google.common.truth.Truth.assertThat;

import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link io.grpc.internal.testing.TestUtils}.
 */
@RunWith(JUnit4.class)
public class TestUtilsTest {
  @Test
  public void sleepAtLeast() throws Exception {
    long sleepMilis = 10L;

    long start = System.nanoTime();
    TestUtils.sleepAtLeast(sleepMilis);
    long end = System.nanoTime();

    assertThat(end - start).isAtLeast(TimeUnit.MILLISECONDS.toNanos(sleepMilis));
  }
}
