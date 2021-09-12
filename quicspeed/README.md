# QUIC Speed

<img src="https://pbs.twimg.com/media/CtLy-vuUMAAvJmw?format=png&name=900x900" alt="drawing" width="200"/>

Continuously sends and receives packets to assess the available bandwidth.
Via SCION or classic UDP.

```bash
# Server
go run . -scion -port 51000
```

```bash
# Client
go run . -scion -remote 17-ffaa:1:f1b,127.0.0.1:51000
```

_Gopher courtesy of Olivier Poitrey._
