#!/bin/bash
set -ex


echo "Updating package list..."
sudo apt-get update

# Install python and pip
echo "Installing python and pip..."
sudo apt-get install python3 -y
sudo apt-get install python3-pip -y

# Install pipenv
echo "Installing pipenv..."
sudo apt install python3-pip -y
pip3 install pipenv

# Install ffmpeg
echo "Installing ffmpeg..."
sudo apt-get install ffmpeg -y

# Install Intel TBB
echo "Installing Intel TBB..."
sudo apt-get install libtbb-dev -y
sudo apt-get install libtbb2 -y

# Install LAPACK and BLAS
echo "Installing LAPACK and BLAS..."
sudo apt-get install liblapack-dev libblas-dev -y

# Install Armadillo
echo "Installing Armadillo..."
sudo apt-get install libarmadillo-dev -y

# Install libsndfile
echo "Installing libsndfile..."
sudo apt-get install libsndfile1-dev -y

# Install XZ Utils
echo "Installing XZ Utils..."
sudo apt-get install xz-utils -y

echo "All specified dependencies have been installed."

# Instal phaselimiter

cd "$(mktemp -d)"
curl -OL https://github.com/ai-mastering/phaselimiter/releases/download/v0.2.0/release.tar.xz
tar -Jxvf release.tar.xz
#git clone -b master --depth 1 --single-branch https://github.com/ai-mastering/phaselimiter.git phaselimiter

sudo mkdir -p /etc/phaselimiter
sudo cp -R phaselimiter/* /etc/phaselimiter/
sudo cp phaselimiter/.python-version /etc/phaselimiter/

cd /etc/phaselimiter/
sudo chmod +x bin/*
sudo cp bin/* /usr/local/bin
sudo chmod +x script/audio_detector
sudo cp script/audio_detector /usr/local/bin/
