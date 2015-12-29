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


Commandline test
----------------

Run the test with arguments:
```
$ adb shell am instrument -w -e server_host <hostname or ip address> -e server_port <port> -e server_host_override foo.test.google.fr -e use_tls true -e use_test_ca true -e test_case all io.grpc.android.integrationtest/.TesterInstrumentation
```

If the test passed successfully, it will output:
```
INSTRUMENTATION_RESULT: grpc test result=Succeed!!!
INSTRUMENTATION_CODE: 0
```
otherwise, output something like:
```
INSTRUMENTATION_RESULT: grpc test result=Failed... : <exception stacktrace if applicable>
INSTRUMENTATION_CODE: 1
```
