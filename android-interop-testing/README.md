gRPC Android test App
=======================

Implements gRPC integration tests in an Android App.

TODO(madongfly) integrate this App into the gRPC-Java build system.

In order to build this app, you need a local.properties file under this directory which specifies
the location of your android sdk:
```
sdk.dir=/somepath/somepath/sdk
```

Connect your Android device or start the emulator:
```
$ ./start-emulator.sh <AVD name> & ./wait-for-emulator.sh
```

Start test server
-----------------

Start the test server by:
```
$ ../run-test-server.sh
```


Manually test
-------------

Install the App by:
```
$ ../gradlew installDebug
```
Then manually test it with the UI.


Instrumentation tests
----------------

Instrumentation tests must be run on a connected device or emulator. Run with the
following gradle command:

```
$ ../gradlew connectedAndroidTest \
    -Pandroid.testInstrumentationRunnerArguments.server_host=10.0.2.2 \
    -Pandroid.testInstrumentationRunnerArguments.server_port=8080 \
    -Pandroid.testInstrumentationRunnerArguments.use_tls=true \
    -Pandroid.testInstrumentationRunnerArguments.server_host_override=foo.test.google.fr \
    -Pandroid.testInstrumentationRunnerArguments.use_test_ca=true \
    -Pandroid.testInstrumentationRunnerArguments.test_case=all
```

