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

import android.content.Context;
import android.content.Intent;
import android.os.Bundle;
import android.support.v7.app.AppCompatActivity;
import android.text.TextUtils;
import android.util.Log;
import android.view.View;
import android.view.inputmethod.InputMethodManager;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.TextView;
import com.google.android.gms.security.ProviderInstaller;
import java.util.LinkedList;
import java.util.List;

public class TesterActivity extends AppCompatActivity
    implements ProviderInstaller.ProviderInstallListener {
  private List<Button> buttons;
  private EditText hostEdit;
  private EditText portEdit;
  private TextView resultText;
  private CheckBox getCheckBox;

  @Override
  protected void onCreate(Bundle savedInstanceState) {
    super.onCreate(savedInstanceState);
    setContentView(R.layout.activity_tester);
    buttons = new LinkedList<>();
    buttons.add((Button) findViewById(R.id.empty_unary_button));
    buttons.add((Button) findViewById(R.id.large_unary_button));
    buttons.add((Button) findViewById(R.id.client_streaming_button));
    buttons.add((Button) findViewById(R.id.server_streaming_button));
    buttons.add((Button) findViewById(R.id.ping_pong_button));

    hostEdit = (EditText) findViewById(R.id.host_edit_text);
    portEdit = (EditText) findViewById(R.id.port_edit_text);
    resultText = (TextView) findViewById(R.id.grpc_response_text);
    getCheckBox = (CheckBox) findViewById(R.id.get_checkbox);

    ProviderInstaller.installIfNeededAsync(this, this);
    // Disable buttons until the security provider installing finishes.
    enableButtons(false);
  }

  public void startEmptyUnary(View view) {
    startTest("empty_unary");
  }

  public void startLargeUnary(View view) {
    startTest("large_unary");
  }

  public void startClientStreaming(View view) {
    startTest("client_streaming");
  }

  public void startServerStreaming(View view) {
    startTest("server_streaming");
  }

  public void startPingPong(View view) {
    startTest("ping_pong");
  }

  private void enableButtons(boolean enable) {
    for (Button button : buttons) {
      button.setEnabled(enable);
    }
  }

  private void startTest(String testCase) {
    ((InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE)).hideSoftInputFromWindow(
        hostEdit.getWindowToken(), 0);
    enableButtons(false);
    String host = hostEdit.getText().toString();
    String portStr = portEdit.getText().toString();
    int port = TextUtils.isEmpty(portStr) ? 8080 : Integer.valueOf(portStr);

    // TODO (madongfly) support server_host_override, useTls and useTestCa in the App UI.
    new InteropTester(testCase,
        TesterOkHttpChannelBuilder.build(host, port, "foo.test.google.fr", true,
            getResources().openRawResource(R.raw.ca), null),
        new InteropTester.TestListener() {
          @Override public void onPreTest() {
            resultText.setText("Testing...");
          }

          @Override public void onPostTest(String result) {
            resultText.setText(result);
            enableButtons(true);
          }
        }, getCheckBox.isChecked()).execute();
  }

  @Override
  public void onProviderInstalled() {
    // Provider is up-to-date, app can make secure network calls.
    enableButtons(true);
  }

  @Override
  public void onProviderInstallFailed(int errorCode, Intent recoveryIntent) {
    // The provider is helpful, but it is possible to succeed without it.
    // Hope that the system-provided libraries are new enough.
    Log.w(InteropTester.LOG_TAG, "Failed installing security provider, error code: " + errorCode);
    enableButtons(true);
  }
}
