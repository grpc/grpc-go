# API Changes


# Behavior Changes


# New Features

* xds: add support for aggregate clusters (#4332)

# Performance Improvements

* transport: remove decodeState to reduce allocations (#3313, #4423)
  - Special thanks: @JNProtzman

# Bug Fixes

* xds_client/rds: weighted_cluster totalWeight default to 100 (#4439)
  - Special thanks: @alpha-baby
* transport: unblock read throttling when controlbuf exits (#4447)

# Documentation

