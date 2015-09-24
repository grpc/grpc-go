REM installer http://www.7-zip.org/a/7z1507-x64.exe
REM 7za is in http://www.7-zip.org/a/7z1507-extra.7z

REM Prerequisite: 7za.exe in current directory or PATH

set PROTOBUF_VER=3.0.0-beta-1
set CMAKE_NAME=cmake-3.3.2-win32-x86

if not exist "protobuf-%PROTOBUF_VER%\cmake\Release\" (
  call :installProto
)
set PROTOCDIR=%cd%\protobuf-%PROTOBUF_VER%\cmake\Release\
goto :eof


:installProto

if not exist "%CMAKE_NAME%" (
  call :installCmake
)
set PATH=%PATH%;%cd%\%CMAKE_NAME%\bin
powershell -command "& { iwr https://github.com/google/protobuf/archive/v%PROTOBUF_VER%.zip -OutFile protobuf.zip }"
7za X protobuf.zip
del protobuf.zip
pushd protobuf-3.0.0-beta-1\cmake
mkdir build
cd build
cmake -DBUILD_TESTING=OFF ..
msbuild /maxcpucount /p:Configuration=Release libprotoc.vcxproj
call extract_includes.bat
popd
goto :eof


:installCmake

powershell -command "& { iwr https://cmake.org/files/v3.3/%CMAKE_NAME%.zip -OutFile cmake.zip }"
7za X cmake.zip
del cmake.zip
goto :eof

REM Compile gRPC-Java with something like:
REM -PtargetArch=x86_32 -PvcProtobufLibs=%cd%\protobuf-3.0.0-beta-1\cmake\build\Release -PvcProtobufInclude=%cd%\protobuf-3.0.0-beta-1\cmake\build\include
