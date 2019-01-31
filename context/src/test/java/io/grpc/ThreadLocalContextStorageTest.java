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

package io.grpc;

import static com.google.common.truth.Truth.assertThat;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class ThreadLocalContextStorageTest {
  private static final Context.Key<Object> KEY = Context.key("test-key");
  private final ThreadLocalContextStorage storage = new ThreadLocalContextStorage();

  @Test
  public void detach_threadLocalClearedOnRoot() {
    Context context = Context.ROOT.withValue(KEY, new Object());
    Context old = storage.doAttach(context);
    assertThat(old).isNull();
    assertThat(storage.current()).isSameAs(context);
    // Users see nulls converted to ROOT, so they will pass non-null as the "old" value
    storage.detach(context, Context.ROOT);
    assertThat(storage.current()).isNull();
  }
}
