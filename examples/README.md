# gRPC Hello World

Follow these setup to run the [quick start][] example:

 1. Update your path variable:

    ```console
    $ export PATH="$PATH:$(go env GOPATH)/bin"
    ```

 2. Get the code:

    ```console
    $ go get -u google.golang.org/grpc/examples/helloworld/greeter_client
    $ go get -u google.golang.org/grpc/examples/helloworld/greeter_server
    ```

 3. Run the server:

    ```console
    $ greeter_server &
    ```

 4. Run the client:

    ```console
    $ greeter_client
    Greeting: Hello world
    ```

For more details (including instructions for making a small change to the
example code) or if you're having trouble running this example, see [Quick
Start][].

[quick start]: https://grpc.io/docs/languages/go/quickstart
