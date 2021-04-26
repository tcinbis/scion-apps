#!/usr/bin/env sh

# Deal with go stuff...
go mod download golang.org/x/mobile

mkdir .gomobilebuild >> /dev/null 2>&1

cd .gomobilebuild

function buildiOS {
    echo "Building ios (arm64)"
    GO11MODULE=on gomobile bind -target=ios/ios_arm64 -o _SCIONDarwinIOS.framework -iosversion 13.0 ../pkg/ios && rm -rf SCIONDarwinIOS.framework && mv _SCIONDarwinIOS.framework SCIONDarwinIOS.framework && mv SCIONDarwinIOS.framework/Versions/A/_SCIONDarwinIOS SCIONDarwinIOS.framework/Versions/A/SCIONDarwinIOS.a
}

function buildSim {
    echo "Building simulator (x86_64)"
    GO11MODULE=on gomobile bind -target=ios/sim_amd64 -o _SCIONDarwinSim.framework -iosversion 13.0 ../pkg/ios && rm -rf SCIONDarwinSim.framework && mv _SCIONDarwinSim.framework SCIONDarwinSim.framework && mv SCIONDarwinSim.framework/Versions/A/_SCIONDarwinSim SCIONDarwinSim.framework/Versions/A/SCIONDarwinSim.a
}

function buildCatalyst {
    echo "Building catalyst (x86_64)"
    GO11MODULE=on gomobile bind -target=ios/catalyst_amd64 -o _SCIONDarwinCatalyst.framework -macosversion 10.15 ../pkg/ios && rm -rf SCIONDarwinCatalyst.framework && mv _SCIONDarwinCatalyst.framework SCIONDarwinCatalyst.framework && mv SCIONDarwinCatalyst.framework/Versions/A/_SCIONDarwinCatalyst SCIONDarwinCatalyst.framework/Versions/A/SCIONDarwinCatalyst.a
}

function buildMacX86 {
    echo "Building mac (x86_64)"
    GO11MODULE=on gomobile bind -target=ios/macos_amd64 -o _SCIONDarwinMac.framework -macosversion 10.15 ../pkg/ios && rm -rf SCIONDarwinMac.framework && mv _SCIONDarwinMac.framework SCIONDarwinMac.framework && mv SCIONDarwinMac.framework/Versions/A/_SCIONDarwinMac SCIONDarwinMac.framework/Versions/A/SCIONDarwinMac.a
}

function buildMacArm {
    echo "Building mac (arm64)"
    GO11MODULE=on gomobile bind -target=ios/macos_arm64 -o _SCIONDarwinMacArm.framework -macosversion 10.15 ../pkg/ios && rm -rf SCIONDarwinMacArm.framework && mv _SCIONDarwinMacArm.framework SCIONDarwinMacArm.framework && mv SCIONDarwinMacArm.framework/Versions/A/_SCIONDarwinMacArm SCIONDarwinMacArm.framework/Versions/A/SCIONDarwinMacArm.a
}

if [[ "$1" == "macarm" ]]; then
    buildMacArm
elif [[ "$1" == "macx86" ]]; then
    buildMacX86
elif [[ "$1" == "catalyst" ]]; then
    buildCatalyst
elif [[ "$1" == "ios" ]]; then
    buildiOS
elif  [[ "$1" == "sim" ]]; then
    buildSim
elif  [[ "$1" == "no-build" ]]; then
    echo "Skipping build"
elif [ "$#" -eq 0 ]; then
    echo "Building all"
    echo "Skipping catalyst"

#    buildSim
    buildMacX86
    buildMacArm
    buildiOS
else
    echo "Invalid arguments"
    exit 1
fi

cd ..

echo "Making xcframework"

lipo -create .gomobilebuild/SCIONDarwinMac.framework/Versions/A/SCIONDarwinMac.a .gomobilebuild/SCIONDarwinMacArm.framework/Versions/A/SCIONDarwinMacArm.a -output _SCIONDarwinMacFat.a

xcodebuild -create-xcframework -library .gomobilebuild/SCIONDarwinIOS.framework/Versions/A/SCIONDarwinIOS.a -headers .gomobilebuild/SCIONDarwinIOS.framework/Versions/A/Headers -library _SCIONDarwinMacFat.a -headers .gomobilebuild/SCIONDarwinMac.framework/Versions/A/Headers -output _SCIONDarwin.xcframework && rm -rf SCIONDarwin.xcframework && mv _SCIONDarwin.xcframework SCIONDarwin.xcframework

#xcodebuild -create-xcframework -library .gomobilebuild/SCIONDarwinSim.framework/Versions/A/SCIONDarwinSim.a -headers .gomobilebuild/SCIONDarwinSim.framework/Versions/A/Headers -library .gomobilebuild/SCIONDarwinIOS.framework/Versions/A/SCIONDarwinIOS.a -headers .gomobilebuild/SCIONDarwinIOS.framework/Versions/A/Headers -library .gomobilebuild/SCIONDarwinCatalyst.framework/Versions/A/SCIONDarwinCatalyst.a -headers .gomobilebuild/SCIONDarwinCatalyst.framework/Versions/A/Headers -library _SCIONDarwinMacFat.a -headers .gomobilebuild/SCIONDarwinMac.framework/Versions/A/Headers -output _SCIONDarwin.xcframework && rm -rf SCIONDarwin.xcframework && mv _SCIONDarwin.xcframework SCIONDarwin.xcframework

rm _SCIONDarwinMacFat.a

echo Done
