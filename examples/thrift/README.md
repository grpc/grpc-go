grpc-thrift is not yet published to Maven Central. To be able to run this
example, you first need to install grpc-thrift to your local Maven repository
(~/.m2).

```sh
$ pushd ../..
$ ./gradlew :grpc-thrift:install -PskipCodegen=true
$ popd
```
