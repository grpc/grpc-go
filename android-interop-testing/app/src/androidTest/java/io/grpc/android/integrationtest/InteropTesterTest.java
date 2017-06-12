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

package io.grpc.android.integrationtest;

import static junit.framework.Assert.assertEquals;

import android.support.test.runner.AndroidJUnit4;
import com.google.common.util.concurrent.SettableFuture;
import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class InteropTesterTest {
  private final int TIMEOUT_SECONDS = 120;

  @Test
  public void interopTests() throws Exception {
    final SettableFuture<String> resultFuture = SettableFuture.create();
    new InteropTester(
            "all",
            TesterOkHttpChannelBuilder.build(
                "grpc-test.sandbox.googleapis.com", 443, null, true, null, null),
            new InteropTester.TestListener() {
              @Override
              public void onPreTest() {}

              @Override
              public void onPostTest(String result) {
                resultFuture.set(result);
              }
            },
            false)
        .execute();
    String result = resultFuture.get(TIMEOUT_SECONDS, TimeUnit.SECONDS);
    assertEquals(result, InteropTester.SUCCESS_MESSAGE);
  }
}
