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

package io.grpc.strictmodehelloworldexample;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Context;
import android.net.TrafficStats;
import android.os.AsyncTask;
import android.os.Bundle;
import android.os.StrictMode;
import android.os.StrictMode.OnVmViolationListener;
import android.os.StrictMode.VmPolicy;
import android.os.strictmode.Violation;
import android.support.v7.app.AppCompatActivity;
import android.text.TextUtils;
import android.text.method.ScrollingMovementMethod;
import android.view.View;
import android.view.inputmethod.InputMethodManager;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.ThreadFactoryBuilder;
import io.grpc.ManagedChannel;
import io.grpc.examples.helloworld.GreeterGrpc;
import io.grpc.examples.helloworld.HelloReply;
import io.grpc.examples.helloworld.HelloRequest;
import io.grpc.okhttp.OkHttpChannelBuilder;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.lang.ref.WeakReference;
import java.util.concurrent.SynchronousQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;

public class StrictModeHelloworldActivity extends AppCompatActivity {
  private Button sendButton;
  private EditText hostEdit;
  private EditText portEdit;
  private EditText messageEdit;
  private TextView resultText;

  @Override
  protected void onCreate(Bundle savedInstanceState) {
    super.onCreate(savedInstanceState);
    VmPolicy policy =
        new StrictMode.VmPolicy.Builder()
            .detectCleartextNetwork()
            .penaltyListener(
                MoreExecutors.directExecutor(),
                new OnVmViolationListener() {
                  @Override
                  public void onVmViolation(final Violation v) {
                    runOnUiThread(
                        new Runnable() {
                          @Override
                          public void run() {
                            new AlertDialog.Builder(StrictModeHelloworldActivity.this)
                                .setTitle("StrictMode VM Violation")
                                .setMessage(v.getLocalizedMessage())
                                .show();
                          }
                        });
                  }
                })
            .penaltyLog()
            .build();
    StrictMode.setVmPolicy(policy);
    setContentView(R.layout.activity_strictmodehelloworld);
    sendButton = (Button) findViewById(R.id.send_button);
    hostEdit = (EditText) findViewById(R.id.host_edit_text);
    portEdit = (EditText) findViewById(R.id.port_edit_text);
    messageEdit = (EditText) findViewById(R.id.message_edit_text);
    resultText = (TextView) findViewById(R.id.grpc_response_text);
    resultText.setMovementMethod(new ScrollingMovementMethod());
  }

  public void sendMessage(View view) {
    ((InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE))
        .hideSoftInputFromWindow(hostEdit.getWindowToken(), 0);
    sendButton.setEnabled(false);
    resultText.setText("");
    new GrpcTask(this)
        .execute(
            hostEdit.getText().toString(),
            messageEdit.getText().toString(),
            portEdit.getText().toString());
  }

  private static class GrpcTask extends AsyncTask<String, Void, String> {
    private final WeakReference<Activity> activityReference;
    private ManagedChannel channel;

    private GrpcTask(Activity activity) {
      this.activityReference = new WeakReference<Activity>(activity);
    }

    @Override
    protected String doInBackground(String... params) {
      String host = params[0];
      String message = params[1];
      String portStr = params[2];
      int port = TextUtils.isEmpty(portStr) ? 0 : Integer.valueOf(portStr);
      try {
        channel =
            OkHttpChannelBuilder.forAddress(host, port)
                .transportExecutor(new NetworkTaggingExecutor(0xFDD))
                .usePlaintext()
                .build();
        GreeterGrpc.GreeterBlockingStub stub = GreeterGrpc.newBlockingStub(channel);
        HelloRequest request = HelloRequest.newBuilder().setName(message).build();
        HelloReply reply = stub.sayHello(request);
        return reply.getMessage();
      } catch (Exception e) {
        StringWriter sw = new StringWriter();
        PrintWriter pw = new PrintWriter(sw);
        e.printStackTrace(pw);
        pw.flush();
        return String.format("Failed... : %n%s", sw);
      }
    }

    @Override
    protected void onPostExecute(String result) {
      try {
        channel.shutdown().awaitTermination(1, TimeUnit.SECONDS);
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
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

  private static class NetworkTaggingExecutor extends ThreadPoolExecutor {

    private int trafficStatsTag;

    NetworkTaggingExecutor(int tag) {
      super(
          0,
          Integer.MAX_VALUE,
          60L,
          TimeUnit.SECONDS,
          new SynchronousQueue<Runnable>(),
          new ThreadFactoryBuilder().setDaemon(true).setNameFormat("grpc-android-%d").build());
      trafficStatsTag = tag;
    }

    @Override
    protected void beforeExecute(Thread t, Runnable r) {
      TrafficStats.setThreadStatsTag(trafficStatsTag);
      super.beforeExecute(t, r);
    }

    @Override
    protected void afterExecute(Runnable r, Throwable t) {
      TrafficStats.clearThreadStatsTag();
      super.afterExecute(r, t);
    }
  }
}
