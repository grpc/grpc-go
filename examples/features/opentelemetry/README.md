# OpenTelemetry

This example demonstrates how to configure OpenTelemetry Tracing on a gRPC client
and server, showcasing the trace data it produces for RPC interactions.

## Try it

1.  **Run the gRPC Server:**

    ```
    go run server/main.go
    ```

    * This starts the gRPC server, which is configured to send trace data via
    OTLP.

2.  **Run the gRPC Client:**

    ```
    go run client/main.go
    ```

    * This starts the gRPC client, which continuously makes RPC calls to the
    server and is configured to send trace data via OTLP.

3. **Start the OpenTelemetry Collector:**

    * **Local Collector:**
        * Install the `otelcol-contrib` binary.
        * NOTE: To see the builder manifests used for official binaries, check 
        [OpenTelemetry Collector Releases]
        (https://github.com/open-telemetry/opentelemetry-collector-releases).
        * For the OpenTelemetry Collector Core distribution specifically, see 
        [OpenTelemetry Collector Core Releases]
        (https://github.com/open-telemetry/opentelemetry-collector-releases/tree/main/distributions/otelcol).
        * The collector will use the `collector-config.yaml` file present in the same directory.
        * **Execute the collector:**
            ```bash
            /path/to/otelcol-contrib --config=collector-config.yaml
            ```
            (Replace `/path/to/otelcol-contrib` with the actual path to your `otelcol-contrib` binary.)

4.  **View Traces in the Collector Output:**

    * The OpenTelemetry Collector, using the `debug` exporter, will print trace
    data to the console.
    * Monitor the collector's console output to see the trace information.

## Explanation

* **Continuous RPC Calls:** The client continuously makes RPC calls to the 
    server, generating telemetry data.
* **OTLP Export (Tracing):** The client and server are configured to export
    trace data via OTLP.
* **OTLP Collector (Tracing):** The OpenTelemetry Collector receives the trace
    data from the client and server.
* **Debug Exporter (Tracing):** The Collector prints the trace data to the 
    console, allowing you to view the trace information directly.
* **Trace Visualization (Console):** By monitoring the collector's console 
    output, you can see the flow of RPC calls and analyze the performance of
    your gRPC services.