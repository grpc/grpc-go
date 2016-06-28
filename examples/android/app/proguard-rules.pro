# Add project specific ProGuard rules here.
# By default, the flags in this file are appended to flags specified
# in $ANDROID_HOME/tools/proguard/proguard-android.txt
# You can edit the include path and order by changing the proguardFiles
# directive in build.gradle.
#
# For more details, see
#   http://developer.android.com/guide/developing/tools/proguard.html

# Add any project specific keep options here:

-dontwarn sun.misc.Unsafe
-dontwarn com.google.common.**
-dontwarn okio.**

# Providers use their name to load files from META-INF
-keepnames class io.grpc.ServerProvider
-keepnames class io.grpc.ManagedChannelProvider
-keepnames class io.grpc.NameResolverProvider

# The Provider implementations must be kept and retain their names, since the
# names are referenced from META-INF
-keep class io.grpc.internal.DnsNameResolverProvider
-keep class io.grpc.okhttp.OkHttpChannelProvider
