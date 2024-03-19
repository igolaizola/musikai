#!/bin/bash

# This won't work on macOS with arm chip!

# Install xcode from app store first

# Instal dependencies
brew install cmake
brew install boost
brew install python3
brew install python3-pip 
brew install pipenv
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer

# Install phaselimiter
xcodebuild \
    -project phaselimiter.xcodeproj \
    -configuration Release

rm -rf CMakeFiles CMakeCache.txt deps/gflags/CMakeFiles \
  && cmake -GXcode -DCMAKE_BUILD_TYPE=Release .

xcodebuild \
    -project phaselimiter.xcodeproj \
    -configuration Release

sudo mkdir -p /etc/phaselimiter
sudo cp -R ./* /etc/phaselimiter/
sudo cp ./.python-version /etc/phaselimiter/

(
cd /etc/phaselimiter/ || exit
sudo chmod +x bin/Release/*
sudo cp bin/Release/* /usr/local/bin
sudo chmod +x script/audio_detector
sudo cp script/audio_detector /usr/local/bin/
sudo chmod 777 ./
pyenv exec pipenv install
)
