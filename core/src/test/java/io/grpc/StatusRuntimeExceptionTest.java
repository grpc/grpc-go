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

package io.grpc;

import static com.google.common.truth.Truth.assertThat;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link StatusRuntimeException}.
 */
@RunWith(JUnit4.class)
public class StatusRuntimeExceptionTest {

  @Test
  public void internalCtorRemovesStack() {
    StackTraceElement[] trace =
        new StatusRuntimeException(Status.CANCELLED, null, false) {}.getStackTrace();

    assertThat(trace).isEmpty();
  }

  @Test
  public void normalCtorKeepsStack() {
    StackTraceElement[] trace =
        new StatusRuntimeException(Status.CANCELLED, null) {}.getStackTrace();

    assertThat(trace).isNotEmpty();
  }

  @Test
  public void extendPreservesStack() {
    StackTraceElement[] trace = new StatusRuntimeException(Status.CANCELLED) {}.getStackTrace();

    assertThat(trace).isNotEmpty();
  }

  @Test
  public void extendAndOverridePreservesStack() {
    final StackTraceElement element = new StackTraceElement("a", "b", "c", 4);
    StatusRuntimeException exception =
        new StatusRuntimeException(Status.CANCELLED, new Metadata()) {

      @Override
      public synchronized Throwable fillInStackTrace() {
        setStackTrace(new StackTraceElement[]{element});
        return this;
      }
    };
    assertThat(exception.getStackTrace()).asList().containsExactly(element);
  }
}
