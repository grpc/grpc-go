Benchmarks
==========

This directory contains the standard benchmarks used to assess the performance of GRPC. Since these
benchmarks run on localhost over loopback the performance of the underlying network is considerably
different to real networks and their behavior. To address this issue we recommend the use of
a network emulator to make loopback behave more like a real network. To this end the benchmark
code looks for a loopback interface with 'benchmark' in its name and attempts to use the address
bound to that interface when creating the client and server. If it cannot find such an interface it
will print a warning and continue with the default localhost address.

To attempt to standardize benchmark behavior across machines we attempt to emulate a 10gbit
ethernet interface with a packet delay of 0.1ms.


Linux
=====

On Linux we can use [netem](https://www.linuxfoundation.org/collaborate/workgroups/networking/netem)  to shape the traffic appropriately.

```sh
# Remove all traffic shaping from loopback
sudo tc qdisc del dev lo root
# Add a priority traffic class to the root of loopback
sudo tc qdisc add dev lo root handle 1: prio
# Add a qdisc under the new class with the appropriate shaping
sudo tc qdisc add dev lo parent 1:1 handle 2: netem delay 0.1ms rate 10gbit
# Add a filter which selects the new qdisc class for traffic to 127.127.127.127
sudo tc filter add dev lo parent 1:0 protocol ip prio 1 u32 match ip dst 127.127.127.127 flowid 2:1
# Create an interface alias call 'lo:benchmark' that maps 127.127.127.127 to loopback
sudo ip addr add dev lo 127.127.127.127/32 label lo:benchmark
```

to remove this configuration

```sh
sudo tc qdisc del dev lo root
sudo ip addr del dev lo 127.127.127.127/32 label lo:benchmark
```

Other Platforms
===============

Contributions welcome!

