@rem ##########################################################################
@rem
@rem Builds artifacts for x86_64 into %WORKSPACE%\artifacts\
@rem
@rem ##########################################################################

type c:\VERSION

@rem Enter repo root
cd /d %~dp0\..\..

set WORKSPACE=T:\src\github\grpc-java
set ESCWORKSPACE=%WORKSPACE:\=\\%

@rem Clear JAVA_HOME to prevent a different Java version from being used
set JAVA_HOME=
set PATH=C:\Program Files\java\jdk1.8.0_152\bin;%PATH%

mkdir grpc-java-helper64
cd grpc-java-helper64
call "%VS120COMNTOOLS%\..\..\VC\bin\amd64\vcvars64.bat" || exit /b 1
call "%WORKSPACE%\buildscripts\make_dependencies.bat" || exit /b 1

cd "%WORKSPACE%"

SET TARGET_ARCH=x86_64
SET FAIL_ON_WARNINGS=true
SET VC_PROTOBUF_LIBS=%ESCWORKSPACE%\\grpc-java-helper64\\protobuf-%PROTOBUF_VER%\\cmake\\build\\Release
SET VC_PROTOBUF_INCLUDE=%ESCWORKSPACE%\\grpc-java-helper64\\protobuf-%PROTOBUF_VER%\\cmake\\build\\include
SET GRADLE_FLAGS=-PtargetArch=%TARGET_ARCH% -PfailOnWarnings=%FAIL_ON_WARNINGS% -PvcProtobufLibs=%VC_PROTOBUF_LIBS% -PvcProtobufInclude=%VC_PROTOBUF_INCLUDE%

@rem make sure no daemons have any files open
cmd.exe /C "%WORKSPACE%\gradlew.bat --stop"

cmd.exe /C "%WORKSPACE%\gradlew.bat  %GRADLE_FLAGS% -Dorg.gradle.parallel=false -PrepositoryDir=%WORKSPACE%\artifacts clean grpc-compiler:build grpc-compiler:uploadArchives" || exit /b 1
