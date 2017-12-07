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

package io.grpc.clientcacheexample;

import android.content.Context;
import android.os.AsyncTask;
import android.os.Bundle;
import android.support.v7.app.AppCompatActivity;
import android.text.TextUtils;
import android.text.method.ScrollingMovementMethod;
import android.util.Log;
import android.view.View;
import android.view.inputmethod.InputMethodManager;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.TextView;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientInterceptors;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.MethodDescriptor;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.stub.ClientCalls;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.util.concurrent.TimeUnit;

public final class ClientCacheExampleActivity extends AppCompatActivity {
  private static final int CACHE_SIZE_IN_BYTES = 1 * 1024 * 1024; // 1MB
  private static final String TAG = "grpcCacheExample";
  private Button mSendButton;
  private EditText mHostEdit;
  private EditText mPortEdit;
  private EditText mMessageEdit;
  private TextView mResultText;
  private CheckBox getCheckBox;
  private CheckBox noCacheCheckBox;
  private CheckBox onlyIfCachedCheckBox;
  private SafeMethodCachingInterceptor.Cache cache;

  @Override
  protected void onCreate(Bundle savedInstanceState) {
    super.onCreate(savedInstanceState);
    setContentView(R.layout.activity_clientcacheexample);
    mSendButton = (Button) findViewById(R.id.send_button);
    mHostEdit = (EditText) findViewById(R.id.host_edit_text);
    mPortEdit = (EditText) findViewById(R.id.port_edit_text);
    mMessageEdit = (EditText) findViewById(R.id.message_edit_text);
    getCheckBox = (CheckBox) findViewById(R.id.get_checkbox);
    noCacheCheckBox = (CheckBox) findViewById(R.id.no_cache_checkbox);
    onlyIfCachedCheckBox = (CheckBox) findViewById(R.id.only_if_cached_checkbox);
    mResultText = (TextView) findViewById(R.id.grpc_response_text);
    mResultText.setMovementMethod(new ScrollingMovementMethod());

    cache = SafeMethodCachingInterceptor.newLruCache(CACHE_SIZE_IN_BYTES);
  }

  /** Sends RPC. Invoked when app button is pressed. */
  public void sendMessage(View view) {
    ((InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE))
        .hideSoftInputFromWindow(mHostEdit.getWindowToken(), 0);
    mSendButton.setEnabled(false);
    new GrpcTask().execute();
  }

  private class GrpcTask extends AsyncTask<Void, Void, String> {
    private String host;
    private String message;
    private int port;
    private ManagedChannel channel;

    @Override
    protected void onPreExecute() {
      host = mHostEdit.getText().toString();
      message = mMessageEdit.getText().toString();
      String portStr = mPortEdit.getText().toString();
      port = TextUtils.isEmpty(portStr) ? 0 : Integer.valueOf(portStr);
      mResultText.setText("");
    }

    @Override
    protected String doInBackground(Void... nothing) {
      try {
        channel = ManagedChannelBuilder.forAddress(host, port).usePlaintext(true).build();
        Channel channelToUse =
            ClientInterceptors.intercept(
                channel, SafeMethodCachingInterceptor.newSafeMethodCachingInterceptor(cache));
        HelloRequest message = HelloRequest.newBuilder().setName(this.message).build();
        HelloReply reply;
        if (getCheckBox.isChecked()) {
          MethodDescriptor<HelloRequest, HelloReply> safeCacheableUnaryCallMethod =
              GreeterGrpc.getSayHelloMethod().toBuilder().setSafe(true).build();
          CallOptions callOptions = CallOptions.DEFAULT;
          if (noCacheCheckBox.isChecked()) {
            callOptions =
                callOptions.withOption(SafeMethodCachingInterceptor.NO_CACHE_CALL_OPTION, true);
          }
          if (onlyIfCachedCheckBox.isChecked()) {
            callOptions =
                callOptions.withOption(
                    SafeMethodCachingInterceptor.ONLY_IF_CACHED_CALL_OPTION, true);
          }
          reply =
              ClientCalls.blockingUnaryCall(
                  channelToUse, safeCacheableUnaryCallMethod, callOptions, message);
        } else {
          GreeterGrpc.GreeterBlockingStub stub = GreeterGrpc.newBlockingStub(channelToUse);
          reply = stub.sayHello(message);
        }
        return reply.getMessage();
      } catch (Exception e) {
        Log.e(TAG, "RPC failed", e);
        StringWriter sw = new StringWriter();
        PrintWriter pw = new PrintWriter(sw);
        e.printStackTrace(pw);
        pw.flush();
        return String.format("Failed... : %n%s", sw);
      }
    }

    @Override
    protected void onPostExecute(String result) {
      if (channel != null) {
        try {
          channel.shutdown().awaitTermination(1, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
          Thread.currentThread().interrupt();
        }
      }
      mResultText.setText(result);
      mSendButton.setEnabled(true);
    }
  }
}
