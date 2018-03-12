#!/bin/bash

mkdir libs
gsutil cp gs://chromium-cronet/android/67.0.3368.0/Release/cronet/cronet_api.jar libs/
gsutil cp gs://chromium-cronet/android/67.0.3368.0/Release/cronet/cronet_impl_common_java.jar \
    libs/
