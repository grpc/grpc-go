grpc Benchmarks
==============================================

## QPS Benchmark

The "Queries Per Second Benchmark" allows you to get a quick overview of the throughput and latency characteristics of grpc.

To build the benchmark type

```
$ ./gradlew :grpc-benchmarks:installDist
```

from the grpc-java directory.

You can now find the client and the server executables in `benchmarks/build/install/grpc-benchmarks/bin`.

The `C++` counterpart can be found at https://github.com/grpc/grpc/tree/master/test/cpp/qps

## Visualizing the Latency Distribution

The QPS client comes with the option `--save_histogram=FILE`, if set it serializes the histogram to `FILE` which can then be used with a plotter to visualize the latency distribution. The histogram is stored in the file format of [HdrHistogram](https://hdrhistogram.org/). That way it can be plotted very easily using a browser based tool like https://hdrhistogram.github.io/HdrHistogram/plotFiles.html. Simply upload the generated file and it will generate a beautiful graph for you. It also allows you to plot two or more histograms on the same surface in order two easily compare latency distributions.

## JVM Options

When running a benchmark it's often useful to adjust some JVM options to improve performance and to gain some insights into what's happening. Passing JVM options to the QPS server and client is as easy as setting the `JAVA_OPTS` environment variables. Below are some options that I find very useful:
 - `-Xms` gives a lower bound on the heap to allocate and `-Xmx` gives an upper bound. If your program uses more than what's specified in `-Xmx` the JVM will exit with an `OutOfMemoryError`. When setting those always set `Xms` and `Xmx` to the same value. The reason for this is that the young and old generation are sized according to the total available heap space. So if the total heap gets resized, they will also have to be resized and this will then trigger a full GC.
 - `-verbose:gc` prints some basic information about garbage collection. It will log to stdout whenever a GC happend and will tell you about the kind of GC, pause time and memory compaction.
 - `-XX:+PrintGCDetails` prints out very detailed GC and heap usage information before the program terminates.
 - `-XX:-HeapDumpOnOutOfMemoryError` and `-XX:HeapDumpPath=path` when you are pushing the JVM hard it sometimes happens that it will crash due to the lack of available heap space. This option will allow you to dive into the details of why it happened. The heap dump can be viewed with e.g. the [Eclipse Memory Analyzer](https://eclipse.org/mat/).
 - `-XX:+PrintCompilation` will give you a detailed overview of what gets compiled, when it gets compiled, by which HotSpot compiler it gets compiled and such. It's a lot of output. I usually just redirect it to file and look at it with `less` and `grep`.
 - `-XX:+PrintInlining` will give you a detailed overview of what gets inlined and why some methods didn't get inlined. The output is very verbose and like `-XX:+PrintCompilation` and useful to look at after some major changes or when a drop in performance occurs.
 - It sometimes happens that a benchmark just doesn't make any progress, that is no bytes are transferred over the network, there is hardly any CPU utilization and low memory usage but the benchmark is still running. In that case it's useful to get a thread dump and see what's going on. HotSpot ships with a tool called `jps` and `jstack`. `jps` tells you the process id of all running JVMs on the machine, which you can then pass to `jstack` and it will print a thread dump of this JVM.
 - Taking a heap dump of a running JVM is similarly straightforward. First get the process id with `jps` and then use `jmap` to take the heap dump. You will almost always want to run it with `-dump:live` in order to only dump live objects. If possible, try to size the heap of your JVM (`-Xmx`) as small as possible in order to also keep the heap dump small. Large heap dumps are very painful and slow to analyze.

## Profiling

Newer JVMs come with a built-in profiler called `Java Flight Recorder`. It's an excellent profiler and it can be used to start a recording directly on the command line,  from within `Java Mission Control` or
with jcmd.

A good introduction on how it works and how to use it are http://hirt.se/blog/?p=364 and http://hirt.se/blog/?p=370.
