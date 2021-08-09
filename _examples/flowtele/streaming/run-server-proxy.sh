#!/usr/bin/env bash

go run ~/go/src/scion-apps/_examples/flowtele --scion & go run ~/go/src/scion-apps/_examples/shttp/proxy -local 0.0.0.0:8080 -remote 17-ffaa:1:ec7,[127.0.0.1]:8001 && fg
