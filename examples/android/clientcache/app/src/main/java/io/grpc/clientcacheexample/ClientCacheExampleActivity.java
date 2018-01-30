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

import android.app.Activity;
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
import java.lang.ref.WeakReference;
import java.util.concurrent.TimeUnit;

public final class ClientCacheExampleActivity extends AppCompatActivity {
  private static final int CACHE_SIZE_IN_BYTES = 1 * 1024 * 1024; // 1MB
  private static final String TAG = "grpcCacheExample";
  private Button sendButton;
  private EditText hostEdit;
  private EditText portEdit;
  private EditText messageEdit;
  private TextView resultText;
  private CheckBox getCheckBox;
  private CheckBox noCacheCheckBox;
  private CheckBox onlyIfCachedCheckBox;
  private SafeMethodCachingInterceptor.Cache cache;

  @Override
  protected void onCreate(Bundle savedInstanceState) {
    super.onCreate(savedInstanceState);
    setContentView(R.layout.activity_clientcacheexample);
    sendButton = (Button) findViewById(R.id.send_button);
    hostEdit = (EditText) findViewById(R.id.host_edit_text);
    portEdit = (EditText) findViewById(R.id.port_edit_text);
    messageEdit = (EditText) findViewById(R.id.message_edit_text);
    getCheckBox = (CheckBox) findViewById(R.id.get_checkbox);
    noCacheCheckBox = (CheckBox) findViewById(R.id.no_cache_checkbox);
    onlyIfCachedCheckBox = (CheckBox) findViewById(R.id.only_if_cached_checkbox);
    resultText = (TextView) findViewById(R.id.grpc_response_text);
    resultText.setMovementMethod(new ScrollingMovementMethod());
    cache = SafeMethodCachingInterceptor.newLruCache(CACHE_SIZE_IN_BYTES);
  }

  /** Sends RPC. Invoked when app button is pressed. */
  public void sendMessage(View view) {
    ((InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE))
        .hideSoftInputFromWindow(hostEdit.getWindowToken(), 0);
    sendButton.setEnabled(false);
    resultText.setText("");
    new GrpcTask(this, cache)
        .execute(
            hostEdit.getText().toString(),
            messageEdit.getText().toString(),
            portEdit.getText().toString(),
            getCheckBox.isChecked(),
            noCacheCheckBox.isChecked(),
            onlyIfCachedCheckBox.isChecked());
  }

  private static class GrpcTask extends AsyncTask<Object, Void, String> {
    private final WeakReference<Activity> activityReference;
    private final SafeMethodCachingInterceptor.Cache cache;
    private ManagedChannel channel;

    private GrpcTask(Activity activity, SafeMethodCachingInterceptor.Cache cache) {
      this.activityReference = new WeakReference<Activity>(activity);
      this.cache = cache;
    }

    @Override
    protected String doInBackground(Object... params) {
      String host = (String) params[0];
      String message = (String) params[1];
      String portStr = (String) params[2];
      boolean useGet = (boolean) params[3];
      boolean noCache = (boolean) params[4];
      boolean onlyIfCached = (boolean) params[5];
      int port = TextUtils.isEmpty(portStr) ? 0 : Integer.valueOf(portStr);
      try {
        channel = ManagedChannelBuilder.forAddress(host, port).usePlaintext(true).build();
        Channel channelToUse =
            ClientInterceptors.intercept(
                channel, SafeMethodCachingInterceptor.newSafeMethodCachingInterceptor(cache));
        HelloRequest request = HelloRequest.newBuilder().setName(message).build();
        HelloReply reply;
        if (useGet) {
          MethodDescriptor<HelloRequest, HelloReply> safeCacheableUnaryCallMethod =
              GreeterGrpc.getSayHelloMethod().toBuilder().setSafe(true).build();
          CallOptions callOptions = CallOptions.DEFAULT;
          if (noCache) {
            callOptions =
                callOptions.withOption(SafeMethodCachingInterceptor.NO_CACHE_CALL_OPTION, true);
          }
          if (onlyIfCached) {
            callOptions =
                callOptions.withOption(
                    SafeMethodCachingInterceptor.ONLY_IF_CACHED_CALL_OPTION, true);
          }
          reply =
              ClientCalls.blockingUnaryCall(
                  channelToUse, safeCacheableUnaryCallMethod, callOptions, request);
        } else {
          GreeterGrpc.GreeterBlockingStub stub = GreeterGrpc.newBlockingStub(channelToUse);
          reply = stub.sayHello(request);
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
      Activity activity = activityReference.get();
      if (activity == null) {
        return;
      }
      TextView resultText = (TextView) activity.findViewById(R.id.grpc_response_text);
      Button sendButton = (Button) activity.findViewById(R.id.send_button);
      resultText.setText(result);
      sendButton.setEnabled(true);
    }
  }
}
