#!/usr/bin/env bash

NARG=$1
N_CLIENTS=${NARG:-1}
echo "Starting $N_CLIENTS clients"
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

BUILD(){
  go build -o client
}

RUN() {
  ./client -s 17-ffaa:1:ec7,127.0.0.1:8001 -f .mp4
}

BUILD
for ((i = 0; i < $N_CLIENTS; i++)); do
  RUN&
  sleep 1
done

wait