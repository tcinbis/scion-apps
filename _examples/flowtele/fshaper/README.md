# FShaper Tutorial

On each machine  run the code below to fetch the repo.
```bash
mkdir -p ~/go/src && cd ~/go/src/ && git clone git@github.com:tcinbis/scio-apps.git
cd scion-apps && git checkout tcinbis/flowtele-pan
go mod download
```

Then on the machine sending the data we want to run `athena`, `fshaper` and the flowtele socket which sends data.

```bash
# TODO: Replace these values with the according IPs and ports for the machines you are using.
export REMOTE_IP="192.168.178.99"
export LOCAL_IP="192.168.178.91"
export REMOTE_PORT="51000"
export REMOTE_IA="17-ffaa:1:ec7"

# On flowtele-ethz
cd ~/go/src/scion-apps/csl331/dbus && python3.6 athena_m2.py 2
cd ~/go/src/scion-apps/_examples/flowtele/fshaper && go run . --ip $REMOTE_IP --port $REMOTE_PORT
cd ~/go/src/scion-apps/_examples/flowtele && go run . --scion --remote-ia $REMOTE_IA --ip $REMOTE_IP --quic-sender-only --num 3 --local-ip $LOCAL_IP --paths-file paths.txt --paths-index 0

# On flowtele-ohio
cd ~/go/src/scion-apps/_examples/flowtele/listener && go run . --scion --ip $REMOTE_IP --port $REMOTE_PORT --key $(pwd)/../tls.key --pem $(pwd)/../tls.pem --num 10
```
