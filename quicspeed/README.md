# QUIC Speed

<img src="https://pbs.twimg.com/media/CtLy-vuUMAAvJmw?format=png&name=900x900" alt="drawing" width="200"/>

Continuously sends and receives packets to assess the available bandwidth.


```bash
# Server
go run . -port 51000
```

```bash
# Client
go run . -remote 17-ffaa:1:f1b,127.0.0.1:51000
```
