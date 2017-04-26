/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
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
