#!/bin/bash

# check if file exists
if [ ! -f "$1" ]; then
  echo "File not found!"
  exit 1
fi

# remove extension
name=${1%.*}

# run phase limiter
phase_limiter --input "$1" --output "$name.master.wav" --ffmpeg ffmpeg \
 --mastering true --mastering_mode mastering5 \
 --sound_quality2_cache /etc/phaselimiter/resource/sound_quality2_cache \
 --mastering_matching_level 1.0000000 --mastering_ms_matching_level 1.0000000 \
 --mastering5_mastering_level 1.0000000 --erb_eval_func_weighting true \
 --reference -9.0000000

# convert to mp3
ffmpeg -i "$name.master.wav" -codec:a libmp3lame -b:a 320k -ac 2 "$name.master.mp3"

# remove wav file
rm "$name.master.wav"
