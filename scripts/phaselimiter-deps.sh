#!/bin/bash

echo "Updating package list..."
sudo apt-get update

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

echo "All specified dependencies have been installed."
