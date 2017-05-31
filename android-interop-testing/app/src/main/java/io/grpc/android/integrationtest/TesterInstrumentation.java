/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

import android.app.Instrumentation;
import android.os.Bundle;
import android.util.Log;
import com.google.android.gms.common.GooglePlayServicesNotAvailableException;
import com.google.android.gms.common.GooglePlayServicesRepairableException;
import com.google.android.gms.security.ProviderInstaller;
import java.io.InputStream;
import java.lang.Throwable;


/**
 * The instrumentation used to run the interop test from command line.
 */
public class TesterInstrumentation extends Instrumentation {
  private String testCase;
  private String host;
  private int port;
  private String serverHostOverride;
  private boolean useTls;
  private boolean useTestCa;
  private String androidSocketFactoryTls;
  private boolean useGet;

  @Override
  public void onCreate(Bundle args) {
    super.onCreate(args);

    testCase = args.getString("test_case") != null ? args.getString("test_case") : "empty_unary";
    host = args.getString("server_host");
    port = Integer.parseInt(args.getString("server_port"));
    serverHostOverride = args.getString("server_host_override");
    useTls = args.getString("use_tls") != null
        ? Boolean.parseBoolean(args.getString("use_tls")) : true;
    useTestCa = args.getString("use_test_ca") != null
        ? Boolean.parseBoolean(args.getString("use_test_ca")) : false;
    androidSocketFactoryTls = args.getString("android_socket_factory_tls");
    useGet = args.getString("use_get") != null
        ? Boolean.parseBoolean(args.getString("use_get")) : false;

    InputStream testCa = null;
    if (useTestCa) {
      testCa = getContext().getResources().openRawResource(R.raw.ca);
    }

    if (useTls) {
      try {
        ProviderInstaller.installIfNeeded(getContext());
      } catch (GooglePlayServicesRepairableException e) {
        // The provider is helpful, but it is possible to succeed without it.
        // Hope that the system-provided libraries are new enough.
        Log.w(InteropTester.LOG_TAG, "Failed installing security provider", e);
      } catch (GooglePlayServicesNotAvailableException e) {
        // The provider is helpful, but it is possible to succeed without it.
        // Hope that the system-provided libraries are new enough.
        Log.w(InteropTester.LOG_TAG, "Failed installing security provider", e);
      }
    }

    try {
      new InteropTester(testCase,
          TesterOkHttpChannelBuilder.build(host, port, serverHostOverride, useTls, testCa,
              androidSocketFactoryTls),
          new InteropTester.TestListener() {
            @Override
            public void onPreTest() {
            }

            @Override
            public void onPostTest(String result) {
              Bundle bundle = new Bundle();
              bundle.putString("grpc test result", result);
              if (InteropTester.SUCCESS_MESSAGE.equals(result)) {
                finish(0, bundle);
              } else {
                finish(1, bundle);
              }
            }
          },
          useGet).execute();
    } catch (Throwable t) {
      Bundle bundle = new Bundle();
      bundle.putString("Exception encountered", t.toString());
      finish(1, bundle);
    }
  }
}
