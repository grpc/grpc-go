@rem ##########################################################################
@rem
@rem Runs tests and then builds artifacts to %WORKSPACE%\artifacts\
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

mkdir grpc-java-helper32
cd grpc-java-helper32
call "%VS120COMNTOOLS%\vsvars32.bat" || exit /b 1
call "%WORKSPACE%\buildscripts\make_dependencies.bat" || exit /b 1

cd "%WORKSPACE%"

SET TARGET_ARCH=x86_32
SET FAIL_ON_WARNINGS=true
SET VC_PROTOBUF_LIBS=%ESCWORKSPACE%\\grpc-java-helper32\\protobuf-%PROTOBUF_VER%\\cmake\\build\\Release
SET VC_PROTOBUF_INCLUDE=%ESCWORKSPACE%\\grpc-java-helper32\\protobuf-%PROTOBUF_VER%\\cmake\\build\\include
SET GRADLE_FLAGS=-PtargetArch=%TARGET_ARCH% -PfailOnWarnings=%FAIL_ON_WARNINGS% -PvcProtobufLibs=%VC_PROTOBUF_LIBS% -PvcProtobufInclude=%VC_PROTOBUF_INCLUDE%

cmd.exe /C "%WORKSPACE%\gradlew.bat %GRADLE_FLAGS% build"
set GRADLEEXIT=%ERRORLEVEL%

@rem Rename test results .xml files to format parsable by Kokoro
@echo off
for /r %%F in (TEST-*.xml) do (
  mkdir "%%~dpnF"
  move "%%F" "%%~dpnF\sponge_log.xml" >NUL
)
@echo on

IF NOT %GRADLEEXIT% == 0 (
  exit /b %GRADLEEXIT%
)

@rem make sure no daemons have any files open
cmd.exe /C "%WORKSPACE%\gradlew.bat --stop"

cmd.exe /C "%WORKSPACE%\gradlew.bat  %GRADLE_FLAGS% -Dorg.gradle.parallel=false -PrepositoryDir=%WORKSPACE%\artifacts clean grpc-compiler:build grpc-compiler:uploadArchives" || exit /b 1
