# Add project specific ProGuard rules here.
# By default, the flags in this file are appended to flags specified
# in $ANDROID_HOME/tools/proguard/proguard-android.txt
# You can edit the include path and order by changing the proguardFiles
# directive in build.gradle.
#
# For more details, see
#   http://developer.android.com/guide/developing/tools/proguard.html

# Add any project specific keep options here:

-dontwarn com.google.common.**
# Ignores: can't find referenced class javax.lang.model.element.Modifier
-dontwarn com.google.errorprone.annotations.**
-dontwarn javax.naming.**
-dontwarn okio.**
-dontwarn sun.misc.Unsafe
