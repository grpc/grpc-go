REM installer http://www.7-zip.org/a/7z1507-x64.exe
REM 7za is in http://www.7-zip.org/a/7z1507-extra.7z

REM Prerequisite:
REM   7za.exe in current directory or PATH

set PROTOBUF_VER=3.2.0
set CMAKE_NAME=cmake-3.3.2-win32-x86

if not exist "protobuf-%PROTOBUF_VER%\cmake\build\Release\" (
  call :installProto
)

echo Compile gRPC-Java with something like:
echo -PtargetArch=x86_32 -PvcProtobufLibs=%cd%\protobuf-%PROTOBUF_VER%\cmake\build\Release -PvcProtobufInclude=%cd%\protobuf-%PROTOBUF_VER%\cmake\build\include
goto :eof


:installProto

if not exist "%CMAKE_NAME%" (
  call :installCmake
)
set PATH=%PATH%;%cd%\%CMAKE_NAME%\bin
powershell -command "& { iwr https://github.com/google/protobuf/archive/v%PROTOBUF_VER%.zip -OutFile protobuf.zip }"
7za X protobuf.zip
del protobuf.zip
pushd protobuf-%PROTOBUF_VER%\cmake
mkdir build
cd build
cmake -Dprotobuf_BUILD_TESTS=OFF -G "Visual Studio %VisualStudioVersion:~0,2%" ..
msbuild /maxcpucount /p:Configuration=Release libprotoc.vcxproj
call extract_includes.bat
popd
goto :eof


:installCmake

powershell -command "& { iwr https://cmake.org/files/v3.3/%CMAKE_NAME%.zip -OutFile cmake.zip }"
7za X cmake.zip
del cmake.zip
goto :eof
