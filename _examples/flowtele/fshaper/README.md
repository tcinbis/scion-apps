# FShaper Tutorial

On your control machine (not the RTT machines)
```bash
git clone git@github.com:tcinbis/scion.git
cd scion && git checkout tcinbis/flowtele
make all

./sync_repo.sh
```

```bash
# On flowtele-ethz
cd ~/go/src/scion/csl331/dbus && python3.6 athena_m2.py 2
cd ~/go/src/scion/ && ./bazel-bin/go/flowtele/fshaper/fshaper_/fshaper --scion --ip 3.12.159.15 --port 51000
cd ~/go/src/scion/ && ./bazel-bin/go/flowtele/flowtele_socket_/flowtele_socket --remote-ia 16-ffaa:0:1004 --ip 3.12.159.15 --quic-sender-only --paths-file paths.txt --paths-index 0 --scion --num 3 --local-ip 129.132.121.187
```

```bash
# On flowtele-ohio
cd ~/go/src/scion/ && ./bazel-bin/go/flowtele/listener/flowtele_listener_/flowtele_listener --ip 3.12.159.15 --scion --port 51000 --key go/flowtele/tls.key --pem go/flowtele/tls.pem --num 10
```
