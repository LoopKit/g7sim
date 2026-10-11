#!/bin/sh
# Builds ImpossiBLE's simulator library as a dylib to inject into an app at
# launch, so the app needs no change to talk to g7sim from the iOS Simulator.
#
#   simulator/build-impossible.sh [out-dir]     (default: simulator/build)
set -eu

# Library 3.1.0, the protocol impossible.go speaks.
repo=https://github.com/mickeyl/ImpossiBLE
commit=58fe38d

here=$(cd "$(dirname "$0")" && pwd)
out=${1:-$here/build}
src=$out/ImpossiBLE
mkdir -p "$out"
if [ ! -d "$src/.git" ]; then
	git clone --quiet "$repo" "$src"
fi
git -C "$src" fetch --quiet origin
git -C "$src" checkout --quiet --force "$commit"
# Fixes a state callback delivered on the main queue instead of the
# central's own, which apps that assert their queue (G7SensorKit) trap on.
git -C "$src" apply "$here/impossible-delegate-queue.patch"

xcrun -sdk iphonesimulator clang -dynamiclib -fobjc-arc -fmodules \
	-target arm64-apple-ios15.0-simulator \
	-I "$src/Sources/ImpossiBLE/include" -I "$src/Sources/ImpossiBLE" \
	-framework Foundation -framework CoreBluetooth \
	-install_name @rpath/libImpossiBLE.dylib \
	"$src"/Sources/ImpossiBLE/*.m -o "$out/libImpossiBLE.dylib"
echo "$out/libImpossiBLE.dylib"
