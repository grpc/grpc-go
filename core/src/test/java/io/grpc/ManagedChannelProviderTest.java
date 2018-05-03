/*
 * Copyright 2015 The gRPC Authors
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

import com.google.common.collect.ImmutableSet;
import java.util.Iterator;
import java.util.concurrent.Callable;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link ManagedChannelProvider}. */
@RunWith(JUnit4.class)
public class ManagedChannelProviderTest {

  @Test
  public void getCandidatesViaHardCoded_triesToLoadClasses() throws Exception {
    ServiceProvidersTestUtil.testHardcodedClasses(
        HardcodedClassesCallable.class.getName(),
        getClass().getClassLoader(),
        ImmutableSet.of(
            "io.grpc.okhttp.OkHttpChannelProvider",
            "io.grpc.netty.NettyChannelProvider"));
  }

  public static final class HardcodedClassesCallable implements Callable<Iterator<Class<?>>> {
    @Override
    public Iterator<Class<?>> call() {
      return ManagedChannelProvider.HARDCODED_CLASSES.iterator();
    }
  }
}
