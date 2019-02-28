Google App Engine interop tests
=====================================

This directory contains interop tests that runs in Google App Engine
as gRPC clients.

Prerequisites
==========================

- Install the Google Cloud SDK and ensure that `gcloud` is in the path
- Set up an [App Engine app](https://appengine.google.com) with your
  choice of a PROJECT_ID.
- Associate your `gcloud` environment with your app:
  ```bash
  # Log into Google Cloud
  $ gcloud auth login

  # Associate this codebase with a GAE project
  $ gcloud config set project PROJECT_ID
  ```

Running the tests in GAE
==========================

You can run the gradle task to execute the interop tests.
```bash
# cd into gae-jdk8
$ ../../gradlew runInteropTestRemote

# Or run one of these from the root gRPC Java directory:
$ ./gradlew :grpc-gae-interop-testing-jdk8:runInteropTestRemote
```

Optional:

You can also browse to `http://${PROJECT_ID}.appspot.google.com` to
see the result of the interop test.


Debugging
==========================

You can find the server side logs by logging into
`http://appengine.google.com` and scrolling down to the section titled
`Application Errors` and `Server Errors`.

Click on the `/` URI to view the log entries for each test run.

