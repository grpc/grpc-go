#!/bin/bash

mkdir libs
gsutil cp gs://chromium-cronet/android/64.0.3254.0/Release/cronet/cronet_api.jar libs/
gsutil cp gs://chromium-cronet/android/64.0.3254.0/Release/cronet/cronet_impl_common_java.jar \
    libs/
