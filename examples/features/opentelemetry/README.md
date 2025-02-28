# OpenTelemetry

This example demonstrates how to configure OpenTelemetry Tracing on a gRPC client
and server, showcasing the telemetry data it produces for RPC interactions.

## Try it

1.  **Run the gRPC Server:**

    ```
    go run server/main.go
    ```

    * This starts the gRPC server, which is configured to send trace data via
    OTLP and expose Prometheus metrics.

2.  **Run the gRPC Client:**

    ```
    go run client/main.go
    ```

    * This starts the gRPC client, which continuously makes RPC calls to the
    server and is configured to send trace data via OTLP and expose
    Prometheus metrics.

3.  **View Prometheus Metrics:**

    * **Server Metrics:**

        ```
        curl localhost:9464/metrics
        ```

        * This command retrieves the Prometheus metrics exposed by the gRPC
        server.

    * **Client Metrics:**

        ```
        curl localhost:9465/metrics
        ```

        * This command retrieves the Prometheus metrics exposed by the gRPC
        client.

## View Traces (Optional)

To visualize traces, you'll need to set up an OTLP Collector and a trace
backend like Jaeger. The client and server code are already configured to
send trace data via OTLP.

1.  **Start Jaeger (or another trace backend):**

    * **Local Jaeger (No Docker):**

        ```
        ./jaeger-all-in-one --collector.otlp.enabled
        ```

2.  **Start the OpenTelemetry Collector:**

    * **Local Collector:**
        * Install the `otelcol-contrib` binary.
        * NOTE: To see the builder manifests used for official binaries, check
            [OpenTelemetry Collector Releases](https://github.com/open-telemetry/
            opentelemetry-collector-releases).
        * For the OpenTelemetry Collector Core distribution specifically, see
            [OpenTelemetry Collector Core Releases](https://github.com/open-telemetry/
            opentelemetry-collector-releases/tree/main/distributions/otelcol).
        * Create a `collector-config.yaml` file with the following content:

            ```yaml
            receivers:
              otlp:
                protocols:
                  grpc:
                  http:

            processors:
              batch:

            exporters:
              jaeger:
                endpoint: "localhost:14250"

            service:
              pipelines:
                traces:
                  receivers: [otlp]
                  processors: [batch]
                  exporters: [jaeger]
            ```

        * Run the collector: `./otelcol-contrib --config collector-config.yaml`

3.  **View Traces in Jaeger UI:**

    * Open `http://localhost:16686` in your browser.
    * Search for traces. You should see traces with names like `UnaryEcho`,
    representing the gRPC calls.

## Explanation

* **Continuous RPC Calls:** The client continuously makes RPC calls to the
    server, generating telemetry data.
* **Prometheus Exporter:** Both the client and server are configured with a
    Prometheus exporter, which listens and provides metrics.
    * The server's Prometheus endpoint defaults to `:9464`.
    * The client's Prometheus endpoint defaults to `:9465`.
* **OpenTelemetry Configuration:** OpenTelemetry is configured on both the
    client and server to capture metrics and traces.
* **Metrics Export:** The OpenTelemetry configuration exports metrics to the
    Prometheus exporter.
* **Viewing Metrics:** By curling the exposed Prometheus ports, you can view
    the metrics recorded by the client and server. These metrics provide
    insights into the performance and behavior of the gRPC calls.
* **OTLP Export (Tracing):** The client and server are configured to export
    trace data via OTLP.
* **OTLP Collector (Tracing):** The OpenTelemetry Collector receives the trace
    data from the client and server.
* **Jaeger Backend (Tracing):** The Collector exports the trace data to Jaeger,
    which visualizes the traces.
* **Trace Visualization (Tracing):** By using the Jaeger UI, you can see the
    flow of RPC calls and analyze the performance of your gRPC services.
    