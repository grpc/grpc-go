grpc Examples
==============================================

In order to run the examples simply execute one of the gradle tasks `mathserver`, `mathclient`,
`stockserver` or `stockclient`.

For example, say you want to play around with the math examples. First you want to start
the server and then have the client connect to it and do some fun calculations.

Assuming you are in the grpc-java root folder you would first start the math server by running

```
$ ./gradlew :grpc-examples:mathserver
```

and in a different terminal window then run the client by typing

```
$ ./gradlew :grpc-examples:mathclient
```

That's it!
