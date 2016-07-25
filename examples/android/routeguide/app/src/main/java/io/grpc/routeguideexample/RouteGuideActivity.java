/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.routeguideexample;

import android.content.Context;
import android.os.AsyncTask;
import android.os.Bundle;
import android.support.v7.app.AppCompatActivity;
import android.text.TextUtils;
import android.text.method.ScrollingMovementMethod;
import android.view.View;
import android.view.inputmethod.InputMethodManager;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.routeguideexample.RouteGuideGrpc.RouteGuideBlockingStub;
import io.grpc.routeguideexample.RouteGuideGrpc.RouteGuideStub;
import io.grpc.stub.StreamObserver;

import java.io.IOException;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.text.MessageFormat;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.List;
import java.util.Random;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

public class RouteGuideActivity extends AppCompatActivity {
    private static final Logger logger = Logger.getLogger(RouteGuideActivity.class.getName());
    private EditText mHostEdit;
    private EditText mPortEdit;
    private Button mStartRouteGuideButton;
    private Button mExitRouteGuideButton;    
    private Button mGetFeatureButton;
    private Button mListFeaturesButton;
    private Button mRecordRouteButton;
    private Button mRouteChatButton;
    private TextView mResultText;

    private ManagedChannel mChannel;
        
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_routeguide);
        mHostEdit = (EditText) findViewById(R.id.host_edit_text);
        mPortEdit = (EditText) findViewById(R.id.port_edit_text);
        mStartRouteGuideButton = (Button) findViewById(R.id.start_route_guide_button);
        mExitRouteGuideButton = (Button) findViewById(R.id.exit_route_guide_button);
        mGetFeatureButton = (Button) findViewById(R.id.get_feature_button);
        mListFeaturesButton = (Button) findViewById(R.id.list_features_button);
        mRecordRouteButton = (Button) findViewById(R.id.record_route_button);
        mRouteChatButton = (Button) findViewById(R.id.route_chat_button);
        mResultText = (TextView) findViewById(R.id.result_text);
        mResultText.setMovementMethod(new ScrollingMovementMethod());
        disableButtons();
    }
    
    public void startRouteGuide(View view) {
        String host = mHostEdit.getText().toString();
        String portStr = mPortEdit.getText().toString();
        int port = TextUtils.isEmpty(portStr) ? 0 : Integer.valueOf(portStr);
        ((InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE))
                .hideSoftInputFromWindow(mHostEdit.getWindowToken(), 0);
        mChannel = ManagedChannelBuilder.forAddress(host, port).usePlaintext(true).build();
        mHostEdit.setEnabled(false);
        mPortEdit.setEnabled(false);
        mStartRouteGuideButton.setEnabled(false);
        enableButtons();
    }
    
    public void exitRouteGuide(View view) {
        mChannel.shutdown();
        disableButtons();
        mHostEdit.setEnabled(true);
        mPortEdit.setEnabled(true);
        mStartRouteGuideButton.setEnabled(true);
    }

    public void getFeature(View view) {
        new GrpcTask(new GetFeatureRunnable()).execute();
    }

    public void listFeatures(View view) {
        new GrpcTask(new ListFeaturesRunnable()).execute();
    }

    public void recordRoute(View view) {
        new GrpcTask(new RecordRouteRunnable()).execute();
    }

    public void routeChat(View view) {
        new GrpcTask(new RouteChatRunnable()).execute();
    }
    
    private void disableButtons() {
        mGetFeatureButton.setEnabled(false);
        mListFeaturesButton.setEnabled(false);
        mRecordRouteButton.setEnabled(false);
        mRouteChatButton.setEnabled(false);
        mExitRouteGuideButton.setEnabled(false);
    }
    
    private void enableButtons() {
        mExitRouteGuideButton.setEnabled(true);
        mGetFeatureButton.setEnabled(true);
        mListFeaturesButton.setEnabled(true);
        mRecordRouteButton.setEnabled(true);
        mRouteChatButton.setEnabled(true);
    }

    private class GrpcTask extends AsyncTask<Void, Void, String> {
        private final GrpcRunnable mGrpc;

        GrpcTask(GrpcRunnable grpc) {
            this.mGrpc = grpc;
        }

        @Override
        protected void onPreExecute() {
            mResultText.setText("");
            disableButtons();
        }

        @Override
        protected String doInBackground(Void... nothing) {
            try {
                String logs = mGrpc.run(RouteGuideGrpc.newBlockingStub(mChannel),
                        RouteGuideGrpc.newStub(mChannel));
                return "Success!" + System.lineSeparator() + logs;
            } catch (Exception e) {
                StringWriter sw = new StringWriter();
                PrintWriter pw = new PrintWriter(sw);
                e.printStackTrace(pw);
                pw.flush();
                return "Failed... : " + System.lineSeparator() + sw;
            }
        }

        @Override
        protected void onPostExecute(String result) {
            mResultText.setText(result);
            enableButtons();
        }
    }

    private interface GrpcRunnable {
        /**
         * Perform a grpc and return all the logs.
         */
        String run(RouteGuideBlockingStub blockingStub, RouteGuideStub asyncStub) throws Exception;
    }

    private class GetFeatureRunnable implements GrpcRunnable {
        @Override
        public String run(RouteGuideBlockingStub blockingStub, RouteGuideStub asyncStub)
                throws Exception {
            return getFeature(409146138, -746188906, blockingStub);
        }

        /**
         * Blocking unary call example.  Calls getFeature and prints the response.
         */
        private String getFeature(int lat, int lon, RouteGuideBlockingStub blockingStub)
                throws StatusRuntimeException {
            StringBuffer logs = new StringBuffer();
            appendLogs(logs, "*** GetFeature: lat={0} lon={1}", lat, lon);

            Point request = Point.newBuilder().setLatitude(lat).setLongitude(lon).build();

            Feature feature;
            feature = blockingStub.getFeature(request);
            if (RouteGuideUtil.exists(feature)) {
                appendLogs(logs, "Found feature called \"{0}\" at {1}, {2}",
                        feature.getName(),
                        RouteGuideUtil.getLatitude(feature.getLocation()),
                        RouteGuideUtil.getLongitude(feature.getLocation()));
            } else {
                appendLogs(logs, "Found no feature at {0}, {1}",
                        RouteGuideUtil.getLatitude(feature.getLocation()),
                        RouteGuideUtil.getLongitude(feature.getLocation()));
            }
            return logs.toString();
        }    
    }

    private class ListFeaturesRunnable implements GrpcRunnable {
        @Override
        public String run(RouteGuideBlockingStub blockingStub, RouteGuideStub asyncStub)
                throws Exception {
            return listFeatures(400000000, -750000000, 420000000, -730000000, blockingStub);
        }
        
        /**
         * Blocking server-streaming example. Calls listFeatures with a rectangle of interest.
         * Prints each response feature as it arrives.
         */
        private String listFeatures(int lowLat, int lowLon, int hiLat, int hiLon,
                RouteGuideBlockingStub blockingStub) throws StatusRuntimeException {
            StringBuffer logs = new StringBuffer("Result: ");
            appendLogs(logs, "*** ListFeatures: lowLat={0} lowLon={1} hiLat={2} hiLon={3}",
                    lowLat, lowLon, hiLat, hiLon);

            Rectangle request = Rectangle.newBuilder()
                    .setLo(Point.newBuilder().setLatitude(lowLat).setLongitude(lowLon).build())
                    .setHi(Point.newBuilder().setLatitude(hiLat).setLongitude(hiLon).build())
                    .build();
            Iterator<Feature> features;
            features = blockingStub.listFeatures(request);

            while (features.hasNext()) {
                Feature feature = features.next();
                appendLogs(logs, feature.toString());
            }
            return logs.toString();
        }
    }

    private class RecordRouteRunnable implements GrpcRunnable {
        private Throwable failed;

        @Override
        public String run(RouteGuideBlockingStub blockingStub, RouteGuideStub asyncStub)
              throws Exception {
            List<Point> points = new ArrayList<Point>();
            points.add(Point.newBuilder()
                    .setLatitude(407838351).setLongitude(-746143763).build());
            points.add(Point.newBuilder()
                    .setLatitude(408122808).setLongitude(-743999179).build());
            points.add(Point.newBuilder()
                    .setLatitude(413628156).setLongitude(-749015468).build());
            return recordRoute(points, 5, asyncStub);
        }
        
        /**
         * Async client-streaming example. Sends {@code numPoints} randomly chosen points from
         * {@code features} with a variable delay in between. Prints the statistics when they are
         * sent from the server.
         */
        private String recordRoute(List<Point> points, int numPoints, RouteGuideStub asyncStub)
                throws InterruptedException, RuntimeException {
            final StringBuffer logs = new StringBuffer();
            appendLogs(logs, "*** RecordRoute");

            final CountDownLatch finishLatch = new CountDownLatch(1);
            StreamObserver<RouteSummary> responseObserver = new StreamObserver<RouteSummary>() {
                @Override
                public void onNext(RouteSummary summary) {
                    appendLogs(logs, "Finished trip with {0} points. Passed {1} features. "
                            + "Travelled {2} meters. It took {3} seconds.", summary.getPointCount(),
                            summary.getFeatureCount(), summary.getDistance(),
                            summary.getElapsedTime());
                }

                @Override
                public void onError(Throwable t) {
                    failed = t;
                    finishLatch.countDown();
                }

                @Override
                public void onCompleted() {
                    appendLogs(logs, "Finished RecordRoute");
                    finishLatch.countDown();
                }
            };

            StreamObserver<Point> requestObserver = asyncStub.recordRoute(responseObserver);
            try {
                // Send numPoints points randomly selected from the points list.
                Random rand = new Random();
                for (int i = 0; i < numPoints; ++i) {
                    int index = rand.nextInt(points.size());
                    Point point = points.get(index);
                    appendLogs(logs, "Visiting point {0}, {1}", RouteGuideUtil.getLatitude(point),
                            RouteGuideUtil.getLongitude(point));
                    requestObserver.onNext(point);
                    // Sleep for a bit before sending the next one.
                    Thread.sleep(rand.nextInt(1000) + 500);
                    if (finishLatch.getCount() == 0) {
                        // RPC completed or errored before we finished sending.
                        // Sending further requests won't error, but they will just be thrown away.
                        break;
                    }
                }
            } catch (RuntimeException e) {
                // Cancel RPC
                requestObserver.onError(e);
                throw e;
            }
            // Mark the end of requests
            requestObserver.onCompleted();

            // Receiving happens asynchronously
            if (!finishLatch.await(1, TimeUnit.MINUTES)) {
                throw new RuntimeException(
                       "Could not finish rpc within 1 minute, the server is likely down");
            }
            
            if (failed != null) {
                throw new RuntimeException(failed);
            }
            return logs.toString();
        }
    }

    private class RouteChatRunnable implements GrpcRunnable {
        private Throwable failed;

        @Override
        public String run(RouteGuideBlockingStub blockingStub, RouteGuideStub asyncStub)
                throws Exception {
            return routeChat(asyncStub);
        }
        
        /**
         * Bi-directional example, which can only be asynchronous. Send some chat messages, and
         * print any chat messages that are sent from the server.
         */
        private String routeChat(RouteGuideStub asyncStub) throws InterruptedException,
                RuntimeException {
            final StringBuffer logs = new StringBuffer();
            appendLogs(logs, "*** RouteChat");
            final CountDownLatch finishLatch = new CountDownLatch(1);
            StreamObserver<RouteNote> requestObserver =
                    asyncStub.routeChat(new StreamObserver<RouteNote>() {
                        @Override
                        public void onNext(RouteNote note) {
                            appendLogs(logs, "Got message \"{0}\" at {1}, {2}", note.getMessage(),
                                    note.getLocation().getLatitude(),
                                    note.getLocation().getLongitude());
                        }

                        @Override
                        public void onError(Throwable t) {
                            failed = t;
                            finishLatch.countDown();
                        }

                        @Override
                        public void onCompleted() {
                            appendLogs(logs,"Finished RouteChat");
                            finishLatch.countDown();
                        }
                    });

            try {
                RouteNote[] requests =
                        {newNote("First message", 0, 0), newNote("Second message", 0, 1),
                                newNote("Third message", 1, 0), newNote("Fourth message", 1, 1)};

                for (RouteNote request : requests) {
                    appendLogs(logs, "Sending message \"{0}\" at {1}, {2}", request.getMessage(),
                            request.getLocation().getLatitude(),
                            request.getLocation().getLongitude());
                    requestObserver.onNext(request);
                }
            } catch (RuntimeException e) {
                // Cancel RPC
                requestObserver.onError(e);
                throw e;
            }
            // Mark the end of requests
            requestObserver.onCompleted();

            // Receiving happens asynchronously
            if (!finishLatch.await(1, TimeUnit.MINUTES)) {
                throw new RuntimeException(
                        "Could not finish rpc within 1 minute, the server is likely down");
            }
            
            if (failed != null) {
                throw new RuntimeException(failed);
            }

            return logs.toString();
        }
    }

    private static void appendLogs(StringBuffer logs, String msg, Object... params) {
        if (params.length > 0) {
            logs.append(MessageFormat.format(msg, params));
        } else {
            logs.append(msg);
        }
        logs.append(System.lineSeparator());
    }

    private static RouteNote newNote(String message, int lat, int lon) {
        return RouteNote.newBuilder().setMessage(message)
                .setLocation(Point.newBuilder().setLatitude(lat).setLongitude(lon).build()).build();
    }
}
