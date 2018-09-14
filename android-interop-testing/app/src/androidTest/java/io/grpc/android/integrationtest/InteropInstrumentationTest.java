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

package io.grpc.android.integrationtest;

import static junit.framework.Assert.assertEquals;

import android.util.Log;
import androidx.test.InstrumentationRegistry;
import androidx.test.rule.ActivityTestRule;
import androidx.test.runner.AndroidJUnit4;
import com.google.android.gms.common.GooglePlayServicesNotAvailableException;
import com.google.android.gms.common.GooglePlayServicesRepairableException;
import com.google.android.gms.security.ProviderInstaller;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.ClientInterceptor;
import io.grpc.android.integrationtest.InteropTask.Listener;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.concurrent.TimeUnit;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class InteropInstrumentationTest {
  private static final int TIMEOUT_SECONDS = 60;
  private static final String LOG_TAG = "GrpcInteropInstrumentationTest";

  private String host;
  private int port;
  private boolean useTls;
  private String serverHostOverride;
  private boolean useTestCa;
  private String testCase;

  // Ensures Looper is initialized for tests running on API level 15. Otherwise instantiating an
  // AsyncTask throws an exception.
  @Rule
  public ActivityTestRule<TesterActivity> activityRule =
      new ActivityTestRule<TesterActivity>(TesterActivity.class);

  @Before
  public void setUp() throws Exception {
    host = InstrumentationRegistry.getArguments().getString("server_host", "10.0.2.2");
    port =
        Integer.parseInt(InstrumentationRegistry.getArguments().getString("server_port", "8080"));
    useTls =
        Boolean.parseBoolean(InstrumentationRegistry.getArguments().getString("use_tls", "true"));
    serverHostOverride = InstrumentationRegistry.getArguments().getString("server_host_override");
    useTestCa =
        Boolean.parseBoolean(
            InstrumentationRegistry.getArguments().getString("use_test_ca", "false"));
    testCase = InstrumentationRegistry.getArguments().getString("test_case", "all");

    if (useTls) {
      try {
        ProviderInstaller.installIfNeeded(InstrumentationRegistry.getTargetContext());
      } catch (GooglePlayServicesRepairableException e) {
        // The provider is helpful, but it is possible to succeed without it.
        // Hope that the system-provided libraries are new enough.
        Log.i(LOG_TAG, "Failed installing security provider", e);
      } catch (GooglePlayServicesNotAvailableException e) {
        // The provider is helpful, but it is possible to succeed without it.
        // Hope that the system-provided libraries are new enough.
        Log.i(LOG_TAG, "Failed installing security provider", e);
      }
    }
  }

  @Test
  public void interopTests() throws Exception {
    if (testCase.equals("all")) {
      runTest("empty_unary");
      runTest("large_unary");
      runTest("client_streaming");
      runTest("server_streaming");
      runTest("ping_pong");
      runTest("empty_stream");
      runTest("cancel_after_begin");
      runTest("cancel_after_first_response");
      runTest("full_duplex_call_should_succeed");
      runTest("half_duplex_call_should_succeed");
      runTest("server_streaming_should_be_flow_controlled");
      runTest("very_large_request");
      runTest("very_large_response");
      runTest("deadline_not_exceeded");
      runTest("deadline_exceeded");
      runTest("deadline_exceeded_server_streaming");
      runTest("unimplemented_method");
      runTest("timeout_on_sleeping_server");
      runTest("graceful_shutdown");
    } else {
      runTest(testCase);
    }
  }

  private void runTest(String testCase) throws Exception {
    final SettableFuture<String> resultFuture = SettableFuture.create();
    InteropTask.Listener listener =
        new Listener() {
          @Override
          public void onComplete(String result) {
            resultFuture.set(result);
          }
        };
    InputStream testCa;
    if (useTestCa) {
      testCa = InstrumentationRegistry.getTargetContext().getResources().openRawResource(R.raw.ca);
    } else {
      testCa = null;
    }
    new InteropTask(
            listener,
            TesterOkHttpChannelBuilder.build(host, port, serverHostOverride, useTls, testCa),
            new ArrayList<>(),
            testCase)
        .execute();
    String result = resultFuture.get(TIMEOUT_SECONDS, TimeUnit.SECONDS);
    assertEquals(testCase + " failed", InteropTask.SUCCESS_MESSAGE, result);
  }
}
