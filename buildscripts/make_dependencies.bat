set PROTOBUF_VER=3.5.1
set CMAKE_NAME=cmake-3.3.2-win32-x86

if not exist "protobuf-%PROTOBUF_VER%\cmake\build\Release\" (
  call :installProto || exit /b 1
)

echo Compile gRPC-Java with something like:
echo -PtargetArch=x86_32 -PvcProtobufLibs=%cd%\protobuf-%PROTOBUF_VER%\cmake\build\Release -PvcProtobufInclude=%cd%\protobuf-%PROTOBUF_VER%\cmake\build\include
goto :eof


:installProto

where /q cmake
if not ERRORLEVEL 1 goto :hasCmake
if not exist "%CMAKE_NAME%" (
  call :installCmake || exit /b 1
)
set PATH=%PATH%;%cd%\%CMAKE_NAME%\bin
:hasCmake
@rem GitHub requires TLSv1.2, and for whatever reason our powershell doesn't have it enabled
powershell -command "& { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 ; iwr https://github.com/google/protobuf/archive/v%PROTOBUF_VER%.zip -OutFile protobuf.zip }" || exit /b 1
powershell -command "& { Add-Type -AssemblyName System.IO.Compression.FileSystem; [System.IO.Compression.ZipFile]::ExtractToDirectory('protobuf.zip', '.') }" || exit /b 1
del protobuf.zip
pushd protobuf-%PROTOBUF_VER%\cmake
mkdir build
cd build
cmake -Dprotobuf_BUILD_TESTS=OFF -G "Visual Studio %VisualStudioVersion:~0,2%" .. || exit /b 1
msbuild /maxcpucount /p:Configuration=Release libprotoc.vcxproj || exit /b 1
call extract_includes.bat || exit /b 1
popd
goto :eof


:installCmake

powershell -command "& { iwr https://cmake.org/files/v3.3/%CMAKE_NAME%.zip -OutFile cmake.zip }" || exit /b 1
powershell -command "& { Add-Type -AssemblyName System.IO.Compression.FileSystem; [System.IO.Compression.ZipFile]::ExtractToDirectory('cmake.zip', '.') }" || exit /b 1
del cmake.zip
goto :eof
