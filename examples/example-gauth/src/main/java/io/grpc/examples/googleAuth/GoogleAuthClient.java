/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.examples.googleAuth;

import com.google.auth.oauth2.GoogleCredentials;
import com.google.pubsub.v1.ListTopicsRequest;
import com.google.pubsub.v1.ListTopicsResponse;
import com.google.pubsub.v1.PublisherGrpc;
import io.grpc.CallCredentials;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.StatusRuntimeException;
import io.grpc.auth.MoreCallCredentials;
import java.io.FileInputStream;
import java.util.Arrays;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Example to illustrate use of Google credentials as described in
 * @see <a href="../../../../../../GOOGLE_AUTH_EXAMPLE.md">Google Auth Example README</a>
 *
 * Also @see <a href="https://cloud.google.com/pubsub/docs/reference/rpc/">Google Cloud Pubsub via gRPC</a>
 */
public class GoogleAuthClient {
  private static final Logger logger = Logger.getLogger(GoogleAuthClient.class.getName());

  private final ManagedChannel channel;

  /**
   * stub generated from the proto file.
   */
  private final PublisherGrpc.PublisherBlockingStub blockingStub;

  /**
   * Construct our gRPC client that connects to the pubsub server at {@code host:port}.
   *
   * @param host  host to connect to - typically "pubsub.googleapis.com"
   * @param port  port to connect to - typically 443 - the TLS port
   * @param callCredentials  the Google call credentials created from a JSON file
   */
  public GoogleAuthClient(String host, int port, CallCredentials callCredentials) {
    // Google API invocation requires a secure channel. Channels are secure by default (SSL/TLS)
    this(ManagedChannelBuilder.forAddress(host, port).build(), callCredentials);
  }

  /**
   * Construct our gRPC client that connects to the pubsub server using an existing channel.
   *
   * @param channel    channel that has been built already
   * @param callCredentials  the Google call credentials created from a JSON file
   */
  GoogleAuthClient(ManagedChannel channel, CallCredentials callCredentials) {
    this.channel = channel;
    blockingStub = PublisherGrpc.newBlockingStub(channel).withCallCredentials(callCredentials);
  }

  public void shutdown() throws InterruptedException {
    channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
  }

  /**
   * Get topics (max 10) for our project ID: the topic list is logged to the logger.
   *
   * @param projectID  the GCP project ID to get the pubsub topics for. This is a string like
   *                   "projects/balmy-cirrus-225307" where "balmy-cirrus-225307" is
   *                   the project ID for the project you created.
   */
  public void getTopics(String projectID) {
    logger.log(Level.INFO, "Will try to get topics for project {0} ...", projectID);

    ListTopicsRequest request = ListTopicsRequest.newBuilder()
            .setPageSize(10)        // get max 10 topics
            .setProject(projectID)  // for our projectID
            .build();

    ListTopicsResponse response;
    try {
      response = blockingStub.listTopics(request);
    } catch (StatusRuntimeException e) {
      logger.log(Level.WARNING, "RPC failed: {0}", e.getStatus());
      return;
    }
    logger.log(Level.INFO, "Topics list:\n {0}", response.getTopicsList());
  }

  /**
   * The app requires 2 arguments as described in
   * @see <a href="../../../../../../GOOGLE_AUTH_EXAMPLE.md">Google Auth Example README</a>
   *
   * arg0 = location of the JSON file for the service account you created in the GCP console
   * arg1 = project name in the form "projects/balmy-cirrus-225307" where "balmy-cirrus-225307" is
   *        the project ID for the project you created.
   *
   */
  public static void main(String[] args) throws Exception {
    if (args.length < 2) {
      logger.severe("Usage: please pass 2 arguments:\n" +
                    "arg0 = location of the JSON file for the service account you created in the GCP console\n" +
                    "arg1 = project name in the form \"projects/xyz\" where \"xyz\" is the project ID of the project you created.\n");
      System.exit(1);
    }
    GoogleCredentials credentials = GoogleCredentials.fromStream(new FileInputStream(args[0]));

    // We need to create appropriate scope as per https://cloud.google.com/storage/docs/authentication#oauth-scopes
    credentials = credentials.createScoped(Arrays.asList("https://www.googleapis.com/auth/cloud-platform"));

    // credentials must be refreshed before the access token is available
    credentials.refreshAccessToken();
    GoogleAuthClient client =
            new GoogleAuthClient("pubsub.googleapis.com", 443, MoreCallCredentials.from(credentials));

    try {
      client.getTopics(args[1]);
    } finally {
      client.shutdown();
    }
  }
}
